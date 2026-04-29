package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	webassets "newapi-checkin/assets"
	"newapi-checkin/internal/auth"
	"newapi-checkin/internal/config"
	"newapi-checkin/internal/reward"
	"newapi-checkin/internal/store"
	"newapi-checkin/internal/captcha"
)

const flashCookieName = "linuxdo_checkin_flash"

var ErrInvalidCheckinRequest = errors.New("签到请求格式非法")
var ErrInvalidCheckinTaskRequest = errors.New("验证码请求格式非法")
var ErrTooManyRequests = errors.New("请求过于频繁，请稍后再试")

type Options struct {
	Config      config.Config
	Store       userStore
	Auth        authService
	CaptchaVerifier   captchaVerifier
	Leaderboard *LeaderboardCache
}

type App struct {
	config       config.Config
	store        userStore
	auth         authService
	captchaVerifier    captchaVerifier
	leaderboard  *LeaderboardCache
	assetHandler http.Handler
	indexHTML    []byte
	checkinTaskLimiter *rateLimiter
}

type AppState struct {
	LoggedIn        bool                           `json:"logged_in"`
	Username        string                         `json:"username,omitempty"`
	LinuxDoID       string                         `json:"linux_do_id,omitempty"`
	Quota           int64                          `json:"quota,omitempty"`
	QuotaThreshold  int64                          `json:"quota_threshold"`
	CanCheckin      bool                           `json:"can_checkin"`
	Message         string                         `json:"message,omitempty"`
	Error           string                         `json:"error,omitempty"`
	LeaderboardDate string                         `json:"leaderboard_date"`
	Leaderboard     []store.CheckinLeaderboardItem `json:"leaderboard"`
	LastCheckin     *store.CheckinResult           `json:"last_checkin,omitempty"`
	PoW             *PoWClientState                `json:"pow,omitempty"`
	Captcha         *CaptchaClientState            `json:"captcha,omitempty"`
}

type PoWClientState struct {
	Enabled    bool   `json:"enabled"`
	Payload    string `json:"payload,omitempty"`
	Signature  string `json:"signature,omitempty"`
	Difficulty int    `json:"difficulty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

type CaptchaClientState struct {
	Enabled bool   `json:"enabled"`
	Type    string `json:"type,omitempty"`
	SiteKey string `json:"site_key,omitempty"`
}

type checkinRequest struct {
	Payload   string `json:"pow_payload"`
	Signature string `json:"pow_signature"`
	Counter   string `json:"pow_counter"`
	Hash      string `json:"pow_hash"`
}

type checkinTaskRequest struct {
	CaptchaToken string `json:"captcha_token"`
}

type flashState struct {
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func New(opts Options) (*App, error) {
	if opts.Auth == nil {
		opts.Auth = auth.NewService(opts.Config)
	}
	if opts.CaptchaVerifier == nil && opts.Config.CheckinCaptchaEnabled {
		secretKey := opts.Config.CheckinCaptchaSecretKey
		if opts.Config.CheckinCaptchaType == "cloudflare" {
			secretKey = opts.Config.CheckinTurnstileSecretKey
		}
		svc, err := captcha.NewService(opts.Config.CheckinCaptchaType, secretKey)
		if err != nil {
			return nil, err
		}
		opts.CaptchaVerifier = svc
	}
	if opts.Leaderboard == nil {
		opts.Leaderboard = NewLeaderboardCache(opts.Store, time.Now, opts.Config.LeaderboardLimit)
	}

	assetHandler, err := newAssetHandler()
	if err != nil {
		return nil, err
	}
	indexHTML, err := webassets.Files.ReadFile("index.html")
	if err != nil {
		return nil, fmt.Errorf("读取首页资源失败: %w", err)
	}

	return &App{
		config:       opts.Config,
		store:        opts.Store,
		auth:         opts.Auth,
		captchaVerifier:    opts.CaptchaVerifier,
		leaderboard:  opts.Leaderboard,
		assetHandler: assetHandler,
		indexHTML:    indexHTML,
		checkinTaskLimiter: newRateLimiter(),
	}, nil
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /assets/", a.assetHandler)
	mux.HandleFunc("GET /", a.handleIndex)
	mux.HandleFunc("GET /api/info", a.handleInfo)
	mux.HandleFunc("GET /login", a.handleLogin)
	mux.HandleFunc("GET /auth/callback", a.handleCallback)
	mux.HandleFunc("POST /api/checkin/task", a.handleCheckinTask)
	mux.HandleFunc("POST /api/checkin", a.handleCheckin)
	mux.HandleFunc("POST /api/logout", a.handleLogout)
	return a.recoverMiddleware(mux)
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(a.indexHTML)
}

func (a *App) handleInfo(w http.ResponseWriter, r *http.Request) {
	flash := a.consumeFlash(w, r)

	session, err := a.auth.ReadSession(r)
	if err != nil {
		state := a.anonymousState()
		state.Message = "请先使用 Linux Do 登录"
		state.Message, state.Error = mergeFlash(state.Message, "", flash)
		writeJSON(w, http.StatusOK, state)
		return
	}

	state, err := a.loadAppState(r.Context(), session, nil)
	if err != nil {
		a.writeStateError(w, r.Context(), session, err, nil)
		return
	}

	state.Message, state.Error = mergeFlash(state.Message, state.Error, flash)
	writeJSON(w, http.StatusOK, state)
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	loginURL, err := a.auth.BeginLogin(w, r)
	if err != nil {
		a.setFlash(w, flashState{Error: err.Error()})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func (a *App) handleCallback(w http.ResponseWriter, r *http.Request) {
	_, err := a.auth.HandleCallback(r.Context(), w, r)
	if err != nil {
		a.setFlash(w, flashState{Error: err.Error()})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	a.setFlash(w, flashState{Message: "登录成功"})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *App) handleCheckin(w http.ResponseWriter, r *http.Request) {
	session, err := a.auth.ReadSession(r)
	if err != nil {
		state := a.anonymousState()
		state.Error = "请先登录后再签到"
		writeJSON(w, http.StatusUnauthorized, state)
		return
	}

	req, err := decodeCheckinRequest(r)
	if err != nil {
		a.writeStateError(w, r.Context(), session, err, nil)
		return
	}

	if err := a.verifyPoW(
		session.LinuxDoID,
		strings.TrimSpace(req.Payload),
		strings.TrimSpace(req.Signature),
		strings.TrimSpace(req.Counter),
		strings.TrimSpace(req.Hash),
		time.Now(),
	); err != nil {
		a.writeStateError(w, r.Context(), session, err, nil)
		return
	}

	currentState, err := a.loadAppState(r.Context(), session, nil)
	if err != nil {
		a.writeStateError(w, r.Context(), session, err, nil)
		return
	}
	if !currentState.CanCheckin {
		currentState.Error = store.ErrQuotaNotEligible.Error()
		currentState.Message = ""
		currentState.PoW = nil
		writeJSON(w, http.StatusBadRequest, currentState)
		return
	}

	quotaAwarded, err := reward.RandomQuotaIncrement(a.config.QuotaIncrementMin, a.config.QuotaIncrementMax)
	if err != nil {
		a.writeStateError(w, r.Context(), session, err, nil)
		return
	}

	result, err := a.store.Checkin(
		r.Context(),
		session.LinuxDoID,
		session.Username,
		a.config.QuotaThreshold,
		quotaAwarded,
		time.Now(),
	)
	if err != nil {
		a.writeStateError(w, r.Context(), session, err, nil)
		return
	}

	log.Printf("用户%d签到成功", result.UserID)
	if err := a.refreshLeaderboard(r.Context(), result.CheckinDate); err != nil {
		log.Printf("刷新签到排行榜失败: %v", err)
	}
	leaderboardDate, leaderboard := a.snapshotLeaderboard()

	state := AppState{
		LoggedIn:        true,
		Username:        session.Username,
		LinuxDoID:       session.LinuxDoID,
		Quota:           result.QuotaAfter,
		QuotaThreshold:  a.config.QuotaThreshold,
		CanCheckin:      false,
		Message:         fmt.Sprintf("签到成功，额度已增加 %s", formatQuotaYuan(result.QuotaAwarded)),
		LeaderboardDate: leaderboardDate,
		Leaderboard:     leaderboard,
		LastCheckin:     &result,
	}
	writeJSON(w, http.StatusOK, state)
}

func (a *App) handleCheckinTask(w http.ResponseWriter, r *http.Request) {
	session, err := a.auth.ReadSession(r)
	if err != nil {
		state := a.anonymousState()
		state.Error = "请先登录后再签到"
		writeJSON(w, http.StatusUnauthorized, state)
		return
	}

	if a.checkinTaskLimiter != nil && !a.checkinTaskLimiter.allow(session.LinuxDoID, time.Minute) {
		writeJSON(w, http.StatusTooManyRequests, AppState{
			QuotaThreshold: a.config.QuotaThreshold,
			Error:          ErrTooManyRequests.Error(),
		})
		return
	}

	state, err := a.loadAppState(r.Context(), session, nil)
	if err != nil {
		a.writeStateError(w, r.Context(), session, err, nil)
		return
	}
	if !state.CanCheckin {
		state.Error = store.ErrQuotaNotEligible.Error()
		state.Message = ""
		state.PoW = nil
		writeJSON(w, http.StatusBadRequest, state)
		return
	}
	if err := a.verifyCaptchaForCheckinTask(r.Context(), r); err != nil {
		a.writeStateError(w, r.Context(), session, err, nil)
		return
	}
	if !a.config.CheckinPoWEnabled {
		writeJSON(w, http.StatusOK, PoWClientState{})
		return
	}

	challenge, err := a.issuePoWChallenge(session.LinuxDoID, time.Now())
	if err != nil {
		state.Error = err.Error()
		state.Message = ""
		writeJSON(w, http.StatusInternalServerError, state)
		return
	}

	writeJSON(w, http.StatusOK, PoWClientState{
		Enabled:    true,
		Payload:    challenge.Payload,
		Signature:  challenge.Signature,
		Difficulty: challenge.Difficulty,
		ExpiresAt:  challenge.ExpiresAt,
	})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.auth.ClearSession(w)

	state := a.anonymousState()
	state.Message = "已退出登录"
	writeJSON(w, http.StatusOK, state)
}

func (a *App) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeJSON(w, http.StatusInternalServerError, AppState{
					QuotaThreshold: a.config.QuotaThreshold,
					Error:          fmt.Sprintf("服务内部错误: %v", rec),
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (a *App) loadAppState(ctx context.Context, session auth.SessionClaims, lastCheckin *store.CheckinResult) (AppState, error) {
	if a.store == nil {
		return AppState{}, errors.New("服务未初始化用户存储")
	}

	user, err := a.store.GetUserByLinuxDoID(ctx, session.LinuxDoID)
	if err != nil {
		return AppState{}, err
	}

	leaderboardDate, leaderboard := a.snapshotLeaderboard()
	state := AppState{
		LoggedIn:        true,
		Username:        session.Username,
		LinuxDoID:       session.LinuxDoID,
		Quota:           user.Quota,
		QuotaThreshold:  a.config.QuotaThreshold,
		CanCheckin:      user.Quota < a.config.QuotaThreshold,
		LeaderboardDate: leaderboardDate,
		Leaderboard:     leaderboard,
		LastCheckin:     lastCheckin,
	}
	if state.CanCheckin {
		state.Message = "当前余额低于阈值，可以签到"
		if a.config.CheckinPoWEnabled {
			state.PoW = &PoWClientState{
				Enabled:    true,
				Difficulty: a.config.CheckinPoWDifficulty,
			}
		}
		if a.config.CheckinCaptchaEnabled {
			siteKey := a.config.CheckinTurnstileSiteKey
			if a.config.CheckinCaptchaType == "hcaptcha" {
				siteKey = a.config.CheckinCaptchaSiteKey
			}
			state.Captcha = &CaptchaClientState{
				Enabled: true,
				Type:    a.config.CheckinCaptchaType,
				SiteKey: siteKey,
			}
		}
		return state, nil
	}

	state.Message = fmt.Sprintf("余额大于等于 %s，暂无法签到", formatQuotaYuan(a.config.QuotaThreshold))
	return state, nil
}

func (a *App) writeStateError(w http.ResponseWriter, ctx context.Context, session auth.SessionClaims, err error, lastCheckin *store.CheckinResult) {
	status := statusForError(err)
	state, loadErr := a.loadAppState(ctx, session, lastCheckin)
	if loadErr != nil {
		leaderboardDate, leaderboard := a.snapshotLeaderboard()
		writeJSON(w, statusForError(loadErr), AppState{
			LoggedIn:        true,
			Username:        session.Username,
			LinuxDoID:       session.LinuxDoID,
			QuotaThreshold:  a.config.QuotaThreshold,
			LeaderboardDate: leaderboardDate,
			Leaderboard:     leaderboard,
			Error:           loadErr.Error(),
		})
		return
	}

	state.Error = err.Error()
	state.Message = ""
	if errors.Is(err, store.ErrAlreadyCheckedIn) || errors.Is(err, store.ErrQuotaNotEligible) {
		state.CanCheckin = false
		state.PoW = nil
		state.Captcha = nil
	}
	writeJSON(w, status, state)
}

func (a *App) anonymousState() AppState {
	leaderboardDate, leaderboard := a.snapshotLeaderboard()
	return AppState{
		QuotaThreshold:  a.config.QuotaThreshold,
		LeaderboardDate: leaderboardDate,
		Leaderboard:     leaderboard,
	}
}

func (a *App) refreshLeaderboard(ctx context.Context, checkinDate string) error {
	if a.leaderboard == nil {
		return nil
	}
	return a.leaderboard.RefreshDate(ctx, checkinDate)
}

func (a *App) snapshotLeaderboard() (string, []store.CheckinLeaderboardItem) {
	if a.leaderboard == nil {
		return "", []store.CheckinLeaderboardItem{}
	}
	return a.leaderboard.Snapshot()
}

func newAssetHandler() (http.Handler, error) {
	assetFS, err := fs.Sub(webassets.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("初始化静态资源失败: %w", err)
	}
	return http.StripPrefix("/assets/", http.FileServer(http.FS(assetFS))), nil
}

func decodeCheckinRequest(r *http.Request) (checkinRequest, error) {
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "application/json") {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			return checkinRequest{}, ErrInvalidCheckinRequest
		}

		var req checkinRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return checkinRequest{}, ErrInvalidCheckinRequest
		}
		return req, nil
	}

	if err := r.ParseForm(); err != nil {
		return checkinRequest{}, ErrInvalidCheckinRequest
	}
	return checkinRequest{
		Payload:   r.FormValue("pow_payload"),
		Signature: r.FormValue("pow_signature"),
		Counter:   r.FormValue("pow_counter"),
		Hash:      r.FormValue("pow_hash"),
	}, nil
}

func decodeCheckinTaskRequest(r *http.Request) (checkinTaskRequest, error) {
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "application/json") {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			return checkinTaskRequest{}, ErrInvalidCheckinTaskRequest
		}

		var req checkinTaskRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return checkinTaskRequest{}, ErrInvalidCheckinTaskRequest
		}
		return req, nil
	}

	if err := r.ParseForm(); err != nil {
		return checkinTaskRequest{}, ErrInvalidCheckinTaskRequest
	}
	return checkinTaskRequest{
		CaptchaToken: r.FormValue("captcha_token"),
	}, nil
}

func (a *App) setFlash(w http.ResponseWriter, flash flashState) {
	payload, err := json.Marshal(flash)
	if err != nil {
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(payload),
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60,
	})
}

func (a *App) consumeFlash(w http.ResponseWriter, r *http.Request) flashState {
	cookie, err := r.Cookie(flashCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return flashState{}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	decoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return flashState{}
	}

	var flash flashState
	if err := json.Unmarshal(decoded, &flash); err != nil {
		return flashState{}
	}
	return flash
}

func mergeFlash(message, errText string, flash flashState) (string, string) {
	if flash.Message != "" {
		message = flash.Message
	}
	if flash.Error != "" {
		errText = flash.Error
	}
	return message, errText
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "JSON 编码失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		return http.StatusNotFound
	case errors.Is(err, store.ErrDuplicateUsers):
		return http.StatusConflict
	case errors.Is(err, captcha.ErrServiceUnavailable):
		return http.StatusBadGateway
	case errors.Is(err, store.ErrAlreadyCheckedIn), errors.Is(err, store.ErrQuotaNotEligible), errors.Is(err, ErrInvalidPoW):
		return http.StatusBadRequest
	case errors.Is(err, ErrInvalidCheckinRequest), errors.Is(err, ErrInvalidCheckinTaskRequest), errors.Is(err, captcha.ErrInvalidToken):
		return http.StatusBadRequest
	case errors.Is(err, auth.ErrMissingSession), errors.Is(err, auth.ErrExpiredSession), errors.Is(err, auth.ErrInvalidSession):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

func formatQuotaYuan(value int64) string {
	const rate = int64(500000)

	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}

	yuan := value / rate
	fraction := (value % rate) * 100 / rate
	result := sign + "￥" + strconv.FormatInt(yuan, 10)
	if fraction == 0 {
		return result
	}

	decimal := strings.TrimRight(fmt.Sprintf("%02d", fraction), "0")
	return result + "." + decimal
}
