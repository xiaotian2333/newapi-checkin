package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	verifyURL      = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	maxTokenLength = 2048
)

var (
	ErrInvalidToken       = errors.New("验证码校验失败，请重试")
	ErrServiceUnavailable = errors.New("验证码服务暂时不可用，请稍后重试")
)

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Service struct {
	secretKey string
	endpoint  string
	client    httpClient
}

type siteVerifyResponse struct {
	Success    bool     `json:"success"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
}

func NewService(secretKey string) *Service {
	return &Service{
		secretKey: strings.TrimSpace(secretKey),
		endpoint:  verifyURL,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) Verify(ctx context.Context, token, remoteIP, expectedAction string) error {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > maxTokenLength {
		return ErrInvalidToken
	}

	form := url.Values{}
	form.Set("secret", s.secretKey)
	form.Set("response", token)
	if remoteIP = strings.TrimSpace(remoteIP); remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		log.Printf("创建 Turnstile 校验请求失败: %v", err)
		return ErrServiceUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("请求 Turnstile 校验接口失败: %v", err)
		return ErrServiceUnavailable
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		log.Printf("读取 Turnstile 校验响应失败: %v", err)
		return ErrServiceUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("Turnstile 校验接口返回异常状态 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		return ErrServiceUnavailable
	}

	var result siteVerifyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("解析 Turnstile 校验响应失败: %v", err)
		return ErrServiceUnavailable
	}
	if !result.Success {
		log.Printf("Turnstile 校验失败: error_codes=%v", result.ErrorCodes)
		return ErrInvalidToken
	}
	if expectedAction != "" && strings.TrimSpace(result.Action) != expectedAction {
		log.Printf("Turnstile action 不匹配: got=%q want=%q", result.Action, expectedAction)
		return ErrInvalidToken
	}
	return nil
}
