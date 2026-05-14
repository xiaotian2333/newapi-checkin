package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"newapi-checkin/internal/store/driver"
)

// 驱动名称
const driverName = "postgres"

// 初始化时自动注册驱动
func init() {
	driver.Register(&Driver{})
}

// Driver PostgreSQL 数据库驱动
type Driver struct{}

// Name 返回驱动名称
func (d *Driver) Name() string {
	return driverName
}

// Open 打开 PostgreSQL 数据库连接
func (d *Driver) Open(ctx context.Context, cfg driver.Config) (driver.Store, error) {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(10)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(5)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
	} else {
		db.SetConnMaxLifetime(30 * time.Minute)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接主库失败: %w", err)
	}

	s := &Store{db: db}

	// 当 LogBaseURL 配置时，打开独立的日志库连接
	if cfg.LogBaseURL != "" {
		logDriver := cfg.LogDriverName
		if logDriver == "" {
			logDriver = "pgx"
		}

		logDB, err := sql.Open(logDriver, cfg.LogBaseURL)
		if err != nil {
			db.Close()
			return nil, err
		}
		logDB.SetMaxOpenConns(5)
		logDB.SetMaxIdleConns(2)
		logDB.SetConnMaxLifetime(30 * time.Minute)

		if err := logDB.PingContext(ctx); err != nil {
			logDB.Close()
			db.Close()
			return nil, fmt.Errorf("连接日志库失败: %w", err)
		}
		s.logDB = logDB
	}

	return s, nil
}

// Store PostgreSQL 存储实现
type Store struct {
	db    *sql.DB
	logDB *sql.DB // nil 表示 logs 表使用主库
}

// ValidateSchema 验证数据库 Schema
func (s *Store) ValidateSchema(ctx context.Context) error {
	mainTables := map[string][]string{
		"users":    {"id", "linux_do_id", "quota", "username"},
		"checkins": {"user_id", "checkin_date", "quota_awarded", "created_at"},
	}
	for table, columns := range mainTables {
		if err := s.validateTableColumns(ctx, s.db, table, columns); err != nil {
			return err
		}
	}

	// logs 表使用独立日志库（若配置），否则使用主库
	logDB := s.logDB
	if logDB == nil {
		logDB = s.db
	}
	logColumns := []string{
		"user_id", "created_at", "type", "content", "username",
		"token_name", "model_name", "quota", "prompt_tokens", "completion_tokens",
		"use_time", "is_stream", "channel_id", "channel_name", "token_id",
		"group", "ip", "other", "request_id",
	}
	return s.validateTableColumns(ctx, logDB, "logs", logColumns)
}

// Close 关闭数据库连接
func (s *Store) Close() error {
	if s.logDB != nil {
		_ = s.logDB.Close()
	}
	return s.db.Close()
}

// GetUserByLinuxDoID 根据 LinuxDo ID 获取用户
func (s *Store) GetUserByLinuxDoID(ctx context.Context, linuxDoID string) (driver.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, CAST(linux_do_id AS text), quota
		FROM users
		WHERE CAST(linux_do_id AS text) = $1
		ORDER BY id
		LIMIT 2
	`, linuxDoID)
	if err != nil {
		return driver.User{}, fmt.Errorf("查询用户失败: %w", err)
	}
	defer rows.Close()

	var users []driver.User
	for rows.Next() {
		var user driver.User
		if err := rows.Scan(&user.ID, &user.LinuxDoID, &user.Quota); err != nil {
			return driver.User{}, fmt.Errorf("读取用户失败: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return driver.User{}, fmt.Errorf("遍历用户结果失败: %w", err)
	}
	if len(users) == 0 {
		return driver.User{}, driver.ErrUserNotFound
	}
	if len(users) > 1 {
		return driver.User{}, driver.ErrDuplicateUsers
	}
	return users[0], nil
}

// Checkin 执行签到操作
func (s *Store) Checkin(ctx context.Context, linuxDoID, username string, threshold, quotaAwarded int64, now time.Time) (driver.CheckinResult, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return driver.CheckinResult{}, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, quota
		FROM users
		WHERE CAST(linux_do_id AS text) = $1
		ORDER BY id
		FOR UPDATE
	`, linuxDoID)
	if err != nil {
		return driver.CheckinResult{}, fmt.Errorf("锁定用户失败: %w", err)
	}
	defer rows.Close()

	var ids []int64
	var quota int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id, &quota); err != nil {
			return driver.CheckinResult{}, fmt.Errorf("读取待签到用户失败: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return driver.CheckinResult{}, fmt.Errorf("遍历待签到用户失败: %w", err)
	}
	if len(ids) == 0 {
		return driver.CheckinResult{}, driver.ErrUserNotFound
	}
	if len(ids) > 1 {
		return driver.CheckinResult{}, driver.ErrDuplicateUsers
	}

	userID := ids[0]
	if quota >= threshold {
		return driver.CheckinResult{}, driver.ErrQuotaNotEligible
	}

	checkinDate := now.Format("2006-01-02")
	var existed int
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM checkins
		WHERE user_id = $1 AND checkin_date = $2
		LIMIT 1
	`, userID, checkinDate).Scan(&existed)
	if err == nil {
		return driver.CheckinResult{}, driver.ErrAlreadyCheckedIn
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return driver.CheckinResult{}, fmt.Errorf("检查签到记录失败: %w", err)
	}

	createdAt := now.Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO checkins (user_id, checkin_date, quota_awarded, created_at)
		VALUES ($1, $2, $3, $4)
	`, userID, checkinDate, quotaAwarded, createdAt); err != nil {
		return driver.CheckinResult{}, fmt.Errorf("写入签到记录失败: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET quota = quota + $1
		WHERE id = $2
	`, quotaAwarded, userID); err != nil {
		return driver.CheckinResult{}, fmt.Errorf("更新用户额度失败: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return driver.CheckinResult{}, fmt.Errorf("提交事务失败: %w", err)
	}

	// 事务提交成功后，异步写入日志到独立日志库
	go s.insertCheckinLogAsyncInternal(userID, username, quotaAwarded, createdAt)

	return driver.CheckinResult{
		UserID:       userID,
		CheckinDate:  checkinDate,
		QuotaAwarded: quotaAwarded,
		QuotaBefore:  quota,
		QuotaAfter:   quota + quotaAwarded,
	}, nil
}

// GetDailyLeaderboard 获取每日签到排行榜
func (s *Store) GetDailyLeaderboard(ctx context.Context, checkinDate string, limit int) ([]driver.CheckinLeaderboardItem, error) {
	if limit <= 0 {
		return []driver.CheckinLeaderboardItem{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT c.user_id, c.checkin_date, c.quota_awarded, c.created_at, COALESCE(u.username, '')
		FROM checkins c
		LEFT JOIN users u ON c.user_id = u.id
		WHERE c.checkin_date = $1
		ORDER BY c.quota_awarded DESC, c.created_at ASC, c.user_id ASC
		LIMIT $2
	`, checkinDate, limit)
	if err != nil {
		return nil, fmt.Errorf("查询签到排行榜失败: %w", err)
	}
	defer rows.Close()

	items := make([]driver.CheckinLeaderboardItem, 0, limit)
	for rows.Next() {
		var item driver.CheckinLeaderboardItem
		if err := rows.Scan(&item.UserID, &item.CheckinDate, &item.QuotaAwarded, &item.CreatedAt, &item.Username); err != nil {
			return nil, fmt.Errorf("读取签到排行榜失败: %w", err)
		}
		item.Rank = len(items) + 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历签到排行榜失败: %w", err)
	}
	return items, nil
}

// InsertCheckinLogAsync 异步写入签到日志
func (s *Store) InsertCheckinLogAsync(userID int64, username string, increment, createdAt int64) {
	s.insertCheckinLogAsyncInternal(userID, username, increment, createdAt)
}

// insertCheckinLogAsyncInternal 异步写入签到日志到独立日志库，不阻塞主流程
// 日志写入失败静默处理，只记录错误日志，不影响主业务
func (s *Store) insertCheckinLogAsyncInternal(userID int64, username string, increment, createdAt int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db := s.logDB
	if db == nil {
		db = s.db
	}

	content := driver.BuildCheckinLogContent(increment)
	_, err := db.ExecContext(ctx, `
		INSERT INTO logs (
			user_id, created_at, type, content, username,
			token_name, model_name, quota, prompt_tokens, completion_tokens,
			use_time, is_stream, channel_id, channel_name, token_id,
			"group", ip, other, request_id
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15,
			$16, $17, $18, $19
		)
	`, userID, createdAt, 4, content, username,
		"", "", int64(0), int64(0), int64(0),
		int64(0), false, int64(0), "", int64(0),
		"", "", "", "",
	)
	if err != nil {
		slog.Warn("写入签到日志失败", "error", err, "user_id", userID)
	}
}

// validateTableColumns 验证表字段
func (s *Store) validateTableColumns(ctx context.Context, db *sql.DB, table string, required []string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1
	`, table)
	if err != nil {
		return fmt.Errorf("检查表 %s 结构失败: %w", table, err)
	}
	defer rows.Close()

	existing := make(map[string]struct{}, len(required))
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return fmt.Errorf("读取表 %s 字段失败: %w", table, err)
		}
		existing[column] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历表 %s 字段失败: %w", table, err)
	}
	if len(existing) == 0 {
		return fmt.Errorf("表 %s 不存在或不在当前 schema 中", table)
	}

	for _, column := range required {
		if _, ok := existing[column]; !ok {
			return fmt.Errorf("表 %s 缺少字段 %s", table, column)
		}
	}
	return nil
}
