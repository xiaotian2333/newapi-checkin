package handler

import (
	"context"
	"net"
	"net/http"
	"strings"

	"newapi-checkin/internal/captcha"
)

const captchaActionCheckin = "checkin"

func (a *App) verifyCaptchaForCheckinTask(ctx context.Context, r *http.Request) error {
	if !a.config.CheckinCaptchaEnabled {
		return nil
	}
	if a.captchaVerifier == nil {
		return captcha.ErrServiceUnavailable
	}

	req, err := decodeCheckinTaskRequest(r)
	if err != nil {
		return err
	}

	return a.captchaVerifier.Verify(ctx, strings.TrimSpace(req.CaptchaToken), a.extractClientIP(r), captchaActionCheckin)
}

func (a *App) extractClientIP(r *http.Request) string {
	if a.config.TrustProxyHeaders {
		if value := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); value != "" {
			return value
		}

		if value := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); value != "" {
			parts := strings.Split(value, ",")
			if len(parts) > 0 {
				first := strings.TrimSpace(parts[0])
				if first != "" {
					return first
				}
			}
		}
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
