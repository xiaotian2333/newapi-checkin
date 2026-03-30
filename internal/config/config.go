package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr     = ":8080"
	defaultQuotaThreshold = int64(10000000)
	defaultQuotaIncrement = int64(10000000)
)

type Config struct {
	DatabaseURL         string
	ListenAddr          string
	QuotaThreshold      int64
	QuotaIncrement      int64
	JWTSecret           []byte
	JWTCookieName       string
	OAuthStateCookie    string
	OAuthStateTTL       time.Duration
	SessionTTL          time.Duration
	CookieSecure        bool
	AuthorizeURL        string
	TokenURL            string
	UserInfoURL         string
	ClientID            string
	ClientSecret        string
	RedirectURI         string
}

func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, err
	}

	quotaThreshold, err := int64OrDefault("QUOTA_THRESHOLD", defaultQuotaThreshold)
	if err != nil {
		return Config{}, err
	}
	quotaIncrement, err := int64OrDefault("QUOTA_INCREMENT", defaultQuotaIncrement)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		ListenAddr:       valueOrDefault("LISTEN_ADDR", defaultListenAddr),
		QuotaThreshold:   quotaThreshold,
		QuotaIncrement:   quotaIncrement,
		JWTSecret:        []byte(strings.TrimSpace(os.Getenv("JWT_SECRET"))),
		JWTCookieName:    "linuxdo_checkin_session",
		OAuthStateCookie: "linuxdo_oauth_state",
		OAuthStateTTL:    10 * time.Minute,
		SessionTTL:       24 * time.Hour,
		AuthorizeURL:     "https://connect.linux.do/oauth2/authorize",
		TokenURL:         "https://connect.linux.do/oauth2/token",
		UserInfoURL:      "https://connect.linux.do/api/user",
		ClientID:         strings.TrimSpace(os.Getenv("LINUXDO_CLIENT_ID")),
		ClientSecret:     strings.TrimSpace(os.Getenv("LINUXDO_CLIENT_SECRET")),
		RedirectURI:      strings.TrimSpace(os.Getenv("LINUXDO_REDIRECT_URI")),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("缺少环境变量 DATABASE_URL")
	}
	if len(cfg.JWTSecret) == 0 {
		return Config{}, errors.New("缺少环境变量 JWT_SECRET")
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURI == "" {
		return Config{}, errors.New("缺少 Linux Do OAuth2 配置")
	}
	if cfg.QuotaThreshold < 0 {
		return Config{}, errors.New("QUOTA_THRESHOLD 不能小于 0")
	}
	if cfg.QuotaIncrement <= 0 {
		return Config{}, errors.New("QUOTA_INCREMENT 必须大于 0")
	}

	redirectURL, err := url.Parse(cfg.RedirectURI)
	if err != nil {
		return Config{}, fmt.Errorf("LINUXDO_REDIRECT_URI 非法: %w", err)
	}
	if redirectURL.Scheme == "" || redirectURL.Host == "" {
		return Config{}, errors.New("LINUXDO_REDIRECT_URI 必须是完整的回调地址")
	}
	cfg.CookieSecure = strings.EqualFold(redirectURL.Scheme, "https")
	return cfg, nil
}

func valueOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func int64OrDefault(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s 不是合法整数: %w", key, err)
	}
	return parsed, nil
}
