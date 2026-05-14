package driver

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 数据库操作错误
var (
	ErrUserNotFound     = errors.New("用户不存在")
	ErrDuplicateUsers   = errors.New("匹配到多条用户记录")
	ErrAlreadyCheckedIn = errors.New("今日已签到")
	ErrQuotaNotEligible = errors.New("当前余额已达到签到阈值")
)

// Config 数据库配置
type Config struct {
	// 主库连接 URL
	DatabaseURL string
	// 日志库连接 URL（可选，为空则使用主库）
	LogBaseURL string
	// 日志库驱动名称（可选，为空则与主库相同）
	LogDriverName string
	// 最大打开连接数
	MaxOpenConns int
	// 最大空闲连接数
	MaxIdleConns int
	// 连接最大生命周期（秒）
	ConnMaxLifetime int
}

// Driver 驱动接口，所有数据库驱动必须实现此接口
type Driver interface {
	// Name 返回驱动名称，如 "postgres"、"mysql"、"sqlite"
	Name() string

	// Open 打开数据库连接
	Open(ctx context.Context, cfg Config) (Store, error)
}

// Store 数据库存储接口，由各驱动实现
type Store interface {
	// ValidateSchema 验证数据库 Schema
	ValidateSchema(ctx context.Context) error
	// Close 关闭数据库连接
	Close() error
	// GetUserByLinuxDoID 根据 LinuxDo ID 获取用户
	GetUserByLinuxDoID(ctx context.Context, linuxDoID string) (User, error)
	// Checkin 执行签到操作
	Checkin(ctx context.Context, linuxDoID, username string, threshold, quotaAwarded int64, now time.Time) (CheckinResult, error)
	// GetDailyLeaderboard 获取每日签到排行榜
	GetDailyLeaderboard(ctx context.Context, checkinDate string, limit int) ([]CheckinLeaderboardItem, error)
	// InsertCheckinLogAsync 异步写入签到日志
	InsertCheckinLogAsync(userID int64, username string, increment, createdAt int64)
}

// User 用户数据结构
type User struct {
	ID        int64
	LinuxDoID string
	Quota     int64
}

// CheckinResult 签到结果数据结构
type CheckinResult struct {
	UserID       int64  `json:"user_id"`
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int64  `json:"quota_awarded"`
	QuotaBefore  int64  `json:"quota_before"`
	QuotaAfter   int64  `json:"quota_after"`
}

// CheckinLeaderboardItem 排行榜条目数据结构
type CheckinLeaderboardItem struct {
	Rank         int    `json:"rank"`
	UserID       int64  `json:"user_id"`
	Username     string `json:"username"`
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int64  `json:"quota_awarded"`
	CreatedAt    int64  `json:"created_at"`
}

// QuotaYuanRate 额度与人民币的换算比例
const QuotaYuanRate = int64(500000)

// 全局驱动注册表
var (
	drivers     = make(map[string]Driver)
	driversMu   sync.RWMutex
)

// 默认驱动名称（受 mutex 保护）
var (
	defaultDriverName = "postgres"
	mu                sync.RWMutex
)

// SetDefault 设置默认驱动
func SetDefault(name string) {
	mu.Lock()
	defer mu.Unlock()
	defaultDriverName = name
}

// GetDefault 获取默认驱动名称
func GetDefault() string {
	mu.RLock()
	defer mu.RUnlock()
	return defaultDriverName
}

// Open 使用指定驱动打开数据库连接
func Open(ctx context.Context, driverName string, cfg Config) (Store, error) {
	driversMu.RLock()
	d, ok := drivers[driverName]
	driversMu.RUnlock()
	if !ok {
		return nil, &UnsupportedDriverError{Driver: driverName}
	}
	return d.Open(ctx, cfg)
}

// OpenDefault 使用默认驱动打开数据库连接
func OpenDefault(ctx context.Context, cfg Config) (Store, error) {
	mu.RLock()
	defer mu.RUnlock()
	return Open(ctx, defaultDriverName, cfg)
}

// DetectDriverFromURL 从连接 URL 自动检测驱动类型
// 支持的协议前缀：
//   - postgres://, postgresql:// → postgres
//   - mysql://, mysql+tcp:// → mysql
//   - file: → sqlite (可选)
func DetectDriverFromURL(databaseURL string) (string, error) {
	if databaseURL == "" {
		return "", errors.New("连接 URL 为空")
	}

	// 解析 URL
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", err
	}

	scheme := strings.ToLower(u.Scheme)

	// 根据 scheme 映射到驱动名称
	switch scheme {
	case "postgres", "postgresql":
		return "postgres", nil
	case "mysql", "mysql+tcp":
		return "mysql", nil
	case "file", "sqlite", "sqlite3":
		return "sqlite", nil
	case "amqp", "grpc", "http", "https":
		// 其他协议暂不支持
		return "", errors.New("不支持的数据库协议: " + scheme)
	default:
		// 未知 scheme，返回错误而非静默使用默认驱动
		return "", fmt.Errorf("不支持的数据库协议: %s", scheme)
	}
}

// OpenFromURL 根据 URL 自动检测并打开数据库连接
func OpenFromURL(ctx context.Context, databaseURL string) (Store, error) {
	driverName, err := DetectDriverFromURL(databaseURL)
	if err != nil {
		return nil, err
	}

	cfg := Config{
		DatabaseURL:     databaseURL,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 1800,
	}

	return Open(ctx, driverName, cfg)
}

// UnsupportedDriverError 不支持的驱动错误
type UnsupportedDriverError struct {
	Driver string
}

func (e *UnsupportedDriverError) Error() string {
	return "不支持的数据库驱动: " + e.Driver
}

// BuildCheckinLogContent 构建签到日志内容
func BuildCheckinLogContent(increment int64) string {
	return fmt.Sprintf("用户签到，获得额度 %s 额度", FormatQuotaYuanFixed(increment))
}

// FormatQuotaYuanFixed 格式化额度为人民币显示
func FormatQuotaYuanFixed(value int64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}

	yuan := value / QuotaYuanRate
	fraction := value % QuotaYuanRate
	decimal := fraction * 1000000 / QuotaYuanRate
	return fmt.Sprintf("%s¥%d.%06d", sign, yuan, decimal)
}

// Register 注册数据库驱动
func Register(d Driver) {
	driversMu.Lock()
	defer driversMu.Unlock()
	drivers[d.Name()] = d
}

// Get 获取已注册的驱动
func Get(name string) (Driver, bool) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	d, ok := drivers[name]
	return d, ok
}
