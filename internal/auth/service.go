package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"newapi-checkin/internal/config"
)

var (
	ErrStateMismatch   = errors.New("OAuth state 校验失败")
	ErrMissingCode     = errors.New("回调中缺少 code")
	ErrMissingToken    = errors.New("响应中缺少 access_token")
	ErrOAuthRejected   = errors.New("用户未完成授权")
	ErrMissingSession  = errors.New("未找到登录态")
	ErrInvalidSession  = errors.New("登录态无效")
	ErrExpiredSession  = errors.New("登录态已过期")
)

type Service struct {
	config config.Config
	client *http.Client
}

type SessionClaims struct {
	LinuxDoID string `json:"linux_do_id"`
	Username  string `json:"username"`
	ExpiresAt int64  `json:"exp"`
}

type LinuxDoUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func NewService(cfg config.Config) *Service {
	return &Service{
		config: cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) BeginLogin(w http.ResponseWriter, r *http.Request) (string, error) {
	state, err := randomString(32)
	if err != nil {
		return "", fmt.Errorf("生成 state 失败: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     s.config.OAuthStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.config.OAuthStateTTL.Seconds()),
	})

	query := url.Values{}
	query.Set("client_id", s.config.ClientID)
	query.Set("redirect_uri", s.config.RedirectURI)
	query.Set("response_type", "code")
	query.Set("scope", "user")
	query.Set("state", state)

	return s.config.AuthorizeURL + "?" + query.Encode(), nil
}

func (s *Service) HandleCallback(ctx context.Context, w http.ResponseWriter, r *http.Request) (LinuxDoUser, error) {
	defer s.clearOAuthState(w)

	if oauthErr := strings.TrimSpace(r.URL.Query().Get("error")); oauthErr != "" {
		description := strings.TrimSpace(r.URL.Query().Get("error_description"))
		if description == "" {
			description = oauthErr
		}
		return LinuxDoUser{}, fmt.Errorf("%w: %s", ErrOAuthRejected, description)
	}

	if err := s.verifyState(r); err != nil {
		return LinuxDoUser{}, err
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		return LinuxDoUser{}, ErrMissingCode
	}

	token, err := s.exchangeCode(ctx, code)
	if err != nil {
		return LinuxDoUser{}, err
	}

	user, err := s.fetchUser(ctx, token)
	if err != nil {
		return LinuxDoUser{}, err
	}

	if err := s.WriteSession(w, SessionClaims{
		LinuxDoID: fmt.Sprintf("%d", user.ID),
		Username:  user.Username,
		ExpiresAt: time.Now().Add(s.config.SessionTTL).Unix(),
	}); err != nil {
		return LinuxDoUser{}, err
	}

	return user, nil
}

func (s *Service) verifyState(r *http.Request) error {
	expected, err := r.Cookie(s.config.OAuthStateCookie)
	if err != nil {
		return ErrStateMismatch
	}
	actual := strings.TrimSpace(r.URL.Query().Get("state"))
	if actual == "" || actual != expected.Value {
		return ErrStateMismatch
	}
	return nil
}

func (s *Service) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", s.config.ClientID)
	form.Set("client_secret", s.config.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", s.config.RedirectURI)
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("创建 token 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 token 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("读取 token 响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token 接口返回异常状态 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("解析 token 响应失败: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", ErrMissingToken
	}
	return tokenResp.AccessToken, nil
}

func (s *Service) fetchUser(ctx context.Context, accessToken string) (LinuxDoUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.UserInfoURL, nil)
	if err != nil {
		return LinuxDoUser{}, fmt.Errorf("创建用户信息请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return LinuxDoUser{}, fmt.Errorf("请求用户信息失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return LinuxDoUser{}, fmt.Errorf("读取用户信息失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return LinuxDoUser{}, fmt.Errorf("用户信息接口返回异常状态 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var user LinuxDoUser
	if err := json.Unmarshal(body, &user); err != nil {
		return LinuxDoUser{}, fmt.Errorf("解析用户信息失败: %w", err)
	}
	if user.ID == 0 {
		return LinuxDoUser{}, errors.New("用户信息中缺少 id")
	}
	return user, nil
}

func (s *Service) WriteSession(w http.ResponseWriter, claims SessionClaims) error {
	token, err := s.signClaims(claims)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     s.config.JWTCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.config.SessionTTL.Seconds()),
	})
	return nil
}

func (s *Service) ReadSession(r *http.Request) (SessionClaims, error) {
	cookie, err := r.Cookie(s.config.JWTCookieName)
	if err != nil {
		return SessionClaims{}, ErrMissingSession
	}

	claims, err := s.verifyClaims(cookie.Value)
	if err != nil {
		return SessionClaims{}, err
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		return SessionClaims{}, ErrExpiredSession
	}
	return claims, nil
}

func (s *Service) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.config.JWTCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	s.clearOAuthState(w)
}

func (s *Service) clearOAuthState(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.config.OAuthStateCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *Service) signClaims(claims SessionClaims) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("编码 JWT 头失败: %w", err)
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("编码 JWT 负载失败: %w", err)
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString(headerBytes)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := encodedHeader + "." + encodedPayload

	mac := hmac.New(sha256.New, s.config.JWTSecret)
	mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + signature, nil
}

func (s *Service) verifyClaims(token string) (SessionClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return SessionClaims{}, ErrInvalidSession
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, s.config.JWTSecret)
	mac.Write([]byte(signingInput))
	expected := mac.Sum(nil)

	actual, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(actual, expected) {
		return SessionClaims{}, ErrInvalidSession
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return SessionClaims{}, ErrInvalidSession
	}

	var claims SessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return SessionClaims{}, ErrInvalidSession
	}
	if claims.LinuxDoID == "" {
		return SessionClaims{}, ErrInvalidSession
	}
	return claims, nil
}

func randomString(length int) (string, error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
