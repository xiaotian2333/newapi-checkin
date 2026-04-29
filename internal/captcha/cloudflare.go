package captcha

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const cloudflareVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
const maxTokenLength = 2048

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type siteVerifyResponse struct {
	Success    bool     `json:"success"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
}

type cloudflareProvider struct {
	secretKey string
	endpoint  string
	client    httpClient
}

func init() {
	Register("cloudflare", func(secretKey string) (Provider, error) {
		return &cloudflareProvider{
			secretKey: strings.TrimSpace(secretKey),
			endpoint:  cloudflareVerifyURL,
			client:    &http.Client{Timeout: 10 * time.Second},
		}, nil
	})
}

func (p *cloudflareProvider) Verify(ctx context.Context, token, remoteIP, expectedAction string) error {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > maxTokenLength {
		return ErrInvalidToken
	}

	form := url.Values{}
	form.Set("secret", p.secretKey)
	form.Set("response", token)
	if remoteIP = strings.TrimSpace(remoteIP); remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		log.Printf("创建验证码校验请求失败: %v", err)
		return ErrServiceUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		log.Printf("请求验证码校验接口失败: %v", err)
		return ErrServiceUnavailable
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		log.Printf("读取验证码校验响应失败: %v", err)
		return ErrServiceUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("验证码校验接口返回异常状态 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		return ErrServiceUnavailable
	}

	var result siteVerifyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("解析验证码校验响应失败: %v", err)
		return ErrServiceUnavailable
	}
	if !result.Success {
		log.Printf("验证码校验失败: error_codes=%v", result.ErrorCodes)
		return ErrInvalidToken
	}
	if expectedAction != "" && strings.TrimSpace(result.Action) != expectedAction {
		log.Printf("验证码 action 不匹配: got=%q want=%q", result.Action, expectedAction)
		return ErrInvalidToken
	}
	return nil
}