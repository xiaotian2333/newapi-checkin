package handler

import (
	"context"
	"net/http"
	"time"

	"newapi-checkin/internal/auth"
	"newapi-checkin/internal/store"
)

type userStore interface {
	GetUserByLinuxDoID(ctx context.Context, linuxDoID string) (store.User, error)
	Checkin(ctx context.Context, linuxDoID, username string, threshold, quotaAwarded int64, now time.Time) (store.CheckinResult, error)
	GetDailyLeaderboard(ctx context.Context, checkinDate string, limit int) ([]store.CheckinLeaderboardItem, error)
}

type authService interface {
	ReadSession(r *http.Request) (auth.SessionClaims, error)
	BeginLogin(w http.ResponseWriter, r *http.Request) (string, error)
	HandleCallback(ctx context.Context, w http.ResponseWriter, r *http.Request) (auth.LinuxDoUser, error)
	ClearSession(w http.ResponseWriter)
}

type captchaVerifier interface {
	Verify(ctx context.Context, token, remoteIP, expectedAction string) error
}
