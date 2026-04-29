package config

import (
	"errors"
	"fmt"
	"net/url"
	"newapi-checkin/internal/reward"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr        = ":8080"
	defaultQuotaThreshold    = int64(10000000)
	defaultQuotaIncrementMin = int64(10000000)
	defaultQuotaIncrementMax = int64(10000000)
	defaultPoWEnabled        = true
	defaultPoWDifficulty     = 18
	defaultPoWTTLSeconds     = 300
	defaultCaptchaEnabled    = false
	defaultLeaderboardLimit    = 10
	maxPoWDifficulty           = 256
)

type Config struct {
	DatabaseURL               string
	ListenAddr                string
	QuotaThreshold            int64
	QuotaIncrementMin         int64
	QuotaIncrementMax         int64
	JWTSecret                 []byte
	JWTCookieName             string
	OAuthStateCookie          string
	OAuthStateTTL             time.Duration
	SessionTTL                time.Duration
	CookieSecure              bool
	AuthorizeURL              string
	TokenURL                  string
	UserInfoURL               string
	ClientID                  string
	ClientSecret              string
	RedirectURI               string
	CheckinPoWEnabled       bool
	CheckinPoWDifficulty    int
	CheckinPoWTTL           time.Duration
	CheckinCaptchaEnabled  bool
	CheckinCaptchaType     string
	CheckinTurnstileSiteKey  string
	CheckinTurnstileSecretKey string
	CheckinCaptchaSiteKey    string
	CheckinCaptchaSecretKey  string
	LeaderboardLimit        int
	TrustProxyHeaders        bool
}

func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, err
	}

	quotaThreshold, err := int64OrDefault("QUOTA_THRESHOLD", defaultQuotaThreshold)
	if err != nil {
		return Config{}, err
	}
	quotaIncrementMin, err := int64OrDefault("QUOTA_INCREMENT_MIN", defaultQuotaIncrementMin)
	if err != nil {
		return Config{}, err
	}
	quotaIncrementMax, err := int64OrDefault("QUOTA_INCREMENT_MAX", defaultQuotaIncrementMax)
	if err != nil {
		return Config{}, err
	}
	powEnabled, err := boolOrDefault("CHECKIN_POW_ENABLED", defaultPoWEnabled)
	if err != nil {
		return Config{}, err
	}
	powDifficulty, err := intOrDefault("CHECKIN_POW_DIFFICULTY", defaultPoWDifficulty)
	if err != nil {
		return Config{}, err
	}
	powTTLSeconds, err := intOrDefault("CHECKIN_POW_TTL_SECONDS", defaultPoWTTLSeconds)
	if err != nil {
		return Config{}, err
	}
	captchaEnabled, err := boolOrDefault("CHECKIN_TURNSTILE_ENABLED", defaultCaptchaEnabled)
	if err != nil {
		return Config{}, err
	}
	captchaType := valueOrDefault("CHECKIN_TURNSTILE_TYPE", "cloudflare")
	leaderboardLimit, err := intOrDefault("LEADERBOARD_LIMIT", defaultLeaderboardLimit)
	if err != nil {
		return Config{}, err
	}
	trustProxyHeaders, err := boolOrDefault("TRUST_PROXY_HEADERS", false)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DatabaseURL:               strings.TrimSpace(os.Getenv("DATABASE_URL")),
		ListenAddr:                valueOrDefault("LISTEN_ADDR", defaultListenAddr),
		QuotaThreshold:            quotaThreshold,
		QuotaIncrementMin:         quotaIncrementMin,
		QuotaIncrementMax:         quotaIncrementMax,
		JWTSecret:                 []byte(strings.TrimSpace(os.Getenv("JWT_SECRET"))),
		JWTCookieName:             "linuxdo_checkin_session",
		OAuthStateCookie:          "linuxdo_oauth_state",
		OAuthStateTTL:             10 * time.Minute,
		SessionTTL:                24 * time.Hour,
		AuthorizeURL:              "https://connect.linux.do/oauth2/authorize",
		TokenURL:                  "https://connect.linux.do/oauth2/token",
		UserInfoURL:               "https://connect.linux.do/api/user",
		ClientID:                  strings.TrimSpace(os.Getenv("LINUXDO_CLIENT_ID")),
		ClientSecret:              strings.TrimSpace(os.Getenv("LINUXDO_CLIENT_SECRET")),
		RedirectURI:               strings.TrimSpace(os.Getenv("LINUXDO_REDIRECT_URI")),
		CheckinPoWEnabled:       powEnabled,
		CheckinPoWDifficulty:    powDifficulty,
		CheckinPoWTTL:           time.Duration(powTTLSeconds) * time.Second,
		CheckinCaptchaEnabled:  captchaEnabled,
		CheckinCaptchaType:     captchaType,
		CheckinTurnstileSiteKey:  strings.TrimSpace(os.Getenv("CHECKIN_TURNSTILE_SITE_KEY")),
		CheckinTurnstileSecretKey: strings.TrimSpace(os.Getenv("CHECKIN_TURNSTILE_SECRET_KEY")),
		CheckinCaptchaSiteKey:    strings.TrimSpace(os.Getenv("CHECKIN_CAPTCHA_SITE_KEY")),
		CheckinCaptchaSecretKey:   strings.TrimSpace(os.Getenv("CHECKIN_CAPTCHA_SECRET_KEY")),
		LeaderboardLimit:        leaderboardLimit,
		TrustProxyHeaders:       trustProxyHeaders,
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
	if _, _, err := reward.NormalizeQuotaIncrementRange(cfg.QuotaIncrementMin, cfg.QuotaIncrementMax); err != nil {
		return Config{}, err
	}
	if cfg.CheckinPoWDifficulty < 0 {
		return Config{}, errors.New("CHECKIN_POW_DIFFICULTY 不能小于 0")
	}
	if cfg.CheckinPoWDifficulty > maxPoWDifficulty {
		return Config{}, fmt.Errorf("CHECKIN_POW_DIFFICULTY 不能大于 %d", maxPoWDifficulty)
	}
	if cfg.CheckinPoWTTL <= 0 {
		return Config{}, errors.New("CHECKIN_POW_TTL_SECONDS 必须大于 0")
	}
	if cfg.LeaderboardLimit <= 0 {
		return Config{}, errors.New("LEADERBOARD_LIMIT 必须大于 0")
	}
	if cfg.CheckinCaptchaEnabled {
		if !cfg.CheckinPoWEnabled {
			return Config{}, errors.New("启用 CHECKIN_TURNSTILE_ENABLED 时必须同时启用 CHECKIN_POW_ENABLED")
		}
		if cfg.CheckinCaptchaType != "cloudflare" && cfg.CheckinCaptchaType != "hcaptcha" {
			return Config{}, errors.New("CHECKIN_TURNSTILE_TYPE 必须是 cloudflare 或 hcaptcha")
		}
		if cfg.CheckinCaptchaType == "cloudflare" {
			if cfg.CheckinTurnstileSiteKey == "" {
				return Config{}, errors.New("缺少环境变量 CHECKIN_TURNSTILE_SITE_KEY")
			}
			if cfg.CheckinTurnstileSecretKey == "" {
				return Config{}, errors.New("缺少环境变量 CHECKIN_TURNSTILE_SECRET_KEY")
			}
		} else {
			if cfg.CheckinCaptchaSiteKey == "" {
				return Config{}, errors.New("缺少环境变量 CHECKIN_CAPTCHA_SITE_KEY")
			}
			if cfg.CheckinCaptchaSecretKey == "" {
				return Config{}, errors.New("缺少环境变量 CHECKIN_CAPTCHA_SECRET_KEY")
			}
		}
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

func intOrDefault(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s 不是合法整数: %w", key, err)
	}
	return parsed, nil
}

func boolOrDefault(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s 不是合法布尔值: %w", key, err)
	}
	return parsed, nil
}
