package store

import (
	"context"
	"fmt"
	"time"

	"newapi-checkin/internal/store/driver"
)

// 重新导出 driver 包中的类型，保持向后兼容
type (
	User                   = driver.User
	CheckinResult          = driver.CheckinResult
	CheckinLeaderboardItem = driver.CheckinLeaderboardItem
)

// 重新导出 driver 包中的错误，保持向后兼容
var (
	ErrUserNotFound     = driver.ErrUserNotFound
	ErrDuplicateUsers   = driver.ErrDuplicateUsers
	ErrAlreadyCheckedIn = driver.ErrAlreadyCheckedIn
	ErrQuotaNotEligible = driver.ErrQuotaNotEligible
)

// QuotaYuanRate 额度与人民币的换算比例
const QuotaYuanRate = driver.QuotaYuanRate

// New 创建数据库存储，自动根据连接 URL 检测驱动类型
// 支持主库和日志库使用不同的数据库类型
func New(ctx context.Context, databaseURL, logBaseURL string) (driver.Store, error) {
	// 检测主库驱动
	mainDriver, err := driver.DetectDriverFromURL(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("检测主库驱动失败: %w", err)
	}

	// 检测日志库驱动（日志库 URL 非空且不同于主库时）
	var logDriver string
	if logBaseURL != "" && logBaseURL != databaseURL {
		logDriver, err = driver.DetectDriverFromURL(logBaseURL)
		if err != nil {
			return nil, fmt.Errorf("检测日志库驱动失败: %w", err)
		}
	}

	cfg := driver.Config{
		DatabaseURL:     databaseURL,
		LogBaseURL:      logBaseURL,
		LogDriverName:   logDriver,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 1800,
	}

	s, err := driver.Open(ctx, mainDriver, cfg)
	if err != nil {
		return nil, fmt.Errorf("打开主库失败: %w", err)
	}

	return s, nil
}

// NewWithDriver 使用指定驱动创建数据库存储
func NewWithDriver(ctx context.Context, driverName, databaseURL, logBaseURL string) (driver.Store, error) {
	// 检测日志库驱动（日志库 URL 非空且不同于主库时）
	var logDriver string
	if logBaseURL != "" && logBaseURL != databaseURL {
		var err error
		logDriver, err = driver.DetectDriverFromURL(logBaseURL)
		if err != nil {
			return nil, fmt.Errorf("检测日志库驱动失败: %w", err)
		}
	}

	cfg := driver.Config{
		DatabaseURL:     databaseURL,
		LogBaseURL:      logBaseURL,
		LogDriverName:   logDriver,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 1800,
	}

	return driver.Open(ctx, driverName, cfg)
}

// ValidateSchemaContext 验证数据库 Schema（带超时）
func ValidateSchemaContext(ctx context.Context, s driver.Store) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.ValidateSchema(ctx)
}
