package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const quotaYuanRate = int64(500000)

var (
	ErrUserNotFound     = errors.New("用户不存在")
	ErrDuplicateUsers   = errors.New("匹配到多条用户记录")
	ErrAlreadyCheckedIn = errors.New("今日已签到")
	ErrQuotaNotEligible = errors.New("当前余额已达到签到阈值")
)

type Store struct {
	db *sql.DB
}

type User struct {
	ID        int64
	LinuxDoID string
	Quota     int64
}

type HomeData struct {
	User     User
	CanCheck bool
}

type CheckinResult struct {
	UserID       int64  `json:"user_id"`
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int64  `json:"quota_awarded"`
	QuotaBefore  int64  `json:"quota_before"`
	QuotaAfter   int64  `json:"quota_after"`
}

type CheckinLeaderboardItem struct {
	Rank         int    `json:"rank"`
	UserID       int64  `json:"user_id"`
	Username     string `json:"username"`
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int64  `json:"quota_awarded"`
	CreatedAt    int64  `json:"created_at"`
}

func (s *Store) ValidateSchema(ctx context.Context) error {
	required := map[string][]string{
		"users":    {"id", "linux_do_id", "quota", "username"},
		"checkins": {"user_id", "checkin_date", "quota_awarded", "created_at"},
		"logs": {
			"user_id", "created_at", "type", "content", "username",
			"token_name", "model_name", "quota", "prompt_tokens", "completion_tokens",
			"use_time", "is_stream", "channel_id", "channel_name", "token_id",
			"group", "ip", "other", "request_id",
		},
	}

	for table, columns := range required {
		if err := s.validateTableColumns(ctx, table, columns); err != nil {
			return err
		}
	}
	return nil
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) validateTableColumns(ctx context.Context, table string, required []string) error {
	rows, err := s.db.QueryContext(ctx, `
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

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) GetUserByLinuxDoID(ctx context.Context, linuxDoID string) (User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, CAST(linux_do_id AS text), quota
		FROM users
		WHERE CAST(linux_do_id AS text) = $1
		ORDER BY id
		LIMIT 2
	`, linuxDoID)
	if err != nil {
		return User{}, fmt.Errorf("查询用户失败: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.LinuxDoID, &user.Quota); err != nil {
			return User{}, fmt.Errorf("读取用户失败: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return User{}, fmt.Errorf("遍历用户结果失败: %w", err)
	}
	if len(users) == 0 {
		return User{}, ErrUserNotFound
	}
	if len(users) > 1 {
		return User{}, ErrDuplicateUsers
	}
	return users[0], nil
}

func (s *Store) Checkin(ctx context.Context, linuxDoID, username string, threshold, quotaAwarded int64, now time.Time) (CheckinResult, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return CheckinResult{}, fmt.Errorf("开启事务失败: %w", err)
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
		return CheckinResult{}, fmt.Errorf("锁定用户失败: %w", err)
	}
	defer rows.Close()

	var ids []int64
	var quota int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id, &quota); err != nil {
			return CheckinResult{}, fmt.Errorf("读取待签到用户失败: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return CheckinResult{}, fmt.Errorf("遍历待签到用户失败: %w", err)
	}
	if len(ids) == 0 {
		return CheckinResult{}, ErrUserNotFound
	}
	if len(ids) > 1 {
		return CheckinResult{}, ErrDuplicateUsers
	}

	userID := ids[0]
	if quota >= threshold {
		return CheckinResult{}, ErrQuotaNotEligible
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
		return CheckinResult{}, ErrAlreadyCheckedIn
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CheckinResult{}, fmt.Errorf("检查签到记录失败: %w", err)
	}

	createdAt := now.Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO checkins (user_id, checkin_date, quota_awarded, created_at)
		VALUES ($1, $2, $3, $4)
	`, userID, checkinDate, quotaAwarded, createdAt); err != nil {
		return CheckinResult{}, fmt.Errorf("写入签到记录失败: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET quota = quota + $1
		WHERE id = $2
	`, quotaAwarded, userID); err != nil {
		return CheckinResult{}, fmt.Errorf("更新用户额度失败: %w", err)
	}

	if err := insertCheckinLog(ctx, tx, userID, username, quotaAwarded, createdAt); err != nil {
		return CheckinResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return CheckinResult{}, fmt.Errorf("提交事务失败: %w", err)
	}

	return CheckinResult{
		UserID:       userID,
		CheckinDate:  checkinDate,
		QuotaAwarded: quotaAwarded,
		QuotaBefore:  quota,
		QuotaAfter:   quota + quotaAwarded,
	}, nil
}

func (s *Store) GetDailyLeaderboard(ctx context.Context, checkinDate string, limit int) ([]CheckinLeaderboardItem, error) {
	if limit <= 0 {
		return []CheckinLeaderboardItem{}, nil
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

	items := make([]CheckinLeaderboardItem, 0, limit)
	for rows.Next() {
		var item CheckinLeaderboardItem
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

func insertCheckinLog(ctx context.Context, tx *sql.Tx, userID int64, username string, increment, createdAt int64) error {
	content := buildCheckinLogContent(increment)
	if _, err := tx.ExecContext(ctx, `
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
	`,
		userID, createdAt, 4, content, username,
		"", "", int64(0), int64(0), int64(0),
		int64(0), false, int64(0), "", int64(0),
		"", "", "", "",
	); err != nil {
		return fmt.Errorf("写入签到日志失败: %w", err)
	}
	return nil
}

func buildCheckinLogContent(increment int64) string {
	return fmt.Sprintf("用户签到，获得额度 %s 额度", formatQuotaYuanFixed(increment))
}

func formatQuotaYuanFixed(value int64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}

	yuan := value / quotaYuanRate
	fraction := value % quotaYuanRate
	decimal := fraction * 1000000 / quotaYuanRate
	return fmt.Sprintf("%s¥%d.%06d", sign, yuan, decimal)
}
