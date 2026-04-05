package handler

import (
	"context"
	"net"
	"net/http"
	"strings"

	"newapi-checkin/internal/turnstile"
)

const captchaActionCheckin = "checkin"

func (a *App) verifyCaptchaForCheckinTask(ctx context.Context, r *http.Request) error {
	if !a.config.CheckinTurnstileEnabled {
		return nil
	}
	if a.turnstile == nil {
		return turnstile.ErrServiceUnavailable
	}

	req, err := decodeCheckinTaskRequest(r)
	if err != nil {
		return err
	}

	return a.turnstile.Verify(ctx, strings.TrimSpace(req.CaptchaToken), extractClientIP(r), captchaActionCheckin)
}

func extractClientIP(r *http.Request) string {
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

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
