package handler

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"newapi-checkin/internal/auth"
	"newapi-checkin/internal/config"
	"newapi-checkin/internal/store"
)

type Options struct {
	Config config.Config
	Store  *store.Store
	Auth   *auth.Service
}

type App struct {
	config config.Config
	store  *store.Store
	auth   *auth.Service
	tpl    *template.Template
}

type PageData struct {
	LoggedIn       bool
	Username       string
	LinuxDoID      string
	Quota          int64
	QuotaThreshold int64
	CanCheckin     bool
	Message        string
	Error          string
	LastCheckin    *store.CheckinResult
}

func New(opts Options) (*App, error) {
	tpl, err := parseTemplate()
	if err != nil {
		return nil, err
	}

	return &App{
		config: opts.Config,
		store:  opts.Store,
		auth:   opts.Auth,
		tpl:    tpl,
	}, nil
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.handleHome)
	mux.HandleFunc("GET /login", a.handleLogin)
	mux.HandleFunc("GET /auth/callback", a.handleCallback)
	mux.HandleFunc("POST /checkin", a.handleCheckin)
	mux.HandleFunc("POST /logout", a.handleLogout)
	return a.recoverMiddleware(mux)
}

func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	session, err := a.auth.ReadSession(r)
	if err != nil {
		a.renderPage(w, http.StatusOK, PageData{
			QuotaThreshold: a.config.QuotaThreshold,
			Message:        "请先使用 Linux Do 登录。",
		})
		return
	}

	user, err := a.store.GetUserByLinuxDoID(r.Context(), session.LinuxDoID)
	if err != nil {
		a.renderError(w, err)
		return
	}

	message := "当前余额低于阈值，可以签到。"
	canCheckin := user.Quota < a.config.QuotaThreshold
	if !canCheckin {
		message = fmt.Sprintf("余额大于等于 %s，暂无法签到。", formatQuotaYuan(a.config.QuotaThreshold))
	}

	a.renderPage(w, http.StatusOK, PageData{
		LoggedIn:       true,
		Username:       session.Username,
		LinuxDoID:      session.LinuxDoID,
		Quota:          user.Quota,
		QuotaThreshold: a.config.QuotaThreshold,
		CanCheckin:     canCheckin,
		Message:        message,
	})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	loginURL, err := a.auth.BeginLogin(w, r)
	if err != nil {
		a.renderPage(w, http.StatusInternalServerError, PageData{
			Error:          err.Error(),
			QuotaThreshold: a.config.QuotaThreshold,
		})
		return
	}
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func (a *App) handleCallback(w http.ResponseWriter, r *http.Request) {
	_, err := a.auth.HandleCallback(r.Context(), w, r)
	if err != nil {
		a.renderPage(w, http.StatusBadRequest, PageData{
			Error:          err.Error(),
			QuotaThreshold: a.config.QuotaThreshold,
		})
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *App) handleCheckin(w http.ResponseWriter, r *http.Request) {
	session, err := a.auth.ReadSession(r)
	if err != nil {
		a.renderPage(w, http.StatusUnauthorized, PageData{
			Error:          "请先登录后再签到。",
			QuotaThreshold: a.config.QuotaThreshold,
		})
		return
	}

	result, err := a.store.Checkin(
		r.Context(),
		session.LinuxDoID,
		a.config.QuotaThreshold,
		a.config.QuotaIncrement,
		time.Now(),
	)
	if err != nil {
		a.renderError(w, err)
		return
	}

	a.renderPage(w, http.StatusOK, PageData{
		LoggedIn:       true,
		Username:       session.Username,
		LinuxDoID:      session.LinuxDoID,
		Quota:          result.QuotaAfter,
		QuotaThreshold: a.config.QuotaThreshold,
		CanCheckin:     false,
		Message:        fmt.Sprintf("签到成功，额度已增加 %s。", formatQuotaYuan(result.QuotaAwarded)),
		LastCheckin:    &result,
	})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.auth.ClearSession(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *App) renderError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := err.Error()
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrDuplicateUsers):
		status = http.StatusConflict
	case errors.Is(err, store.ErrAlreadyCheckedIn), errors.Is(err, store.ErrQuotaNotEligible):
		status = http.StatusBadRequest
	case errors.Is(err, auth.ErrMissingSession), errors.Is(err, auth.ErrExpiredSession), errors.Is(err, auth.ErrInvalidSession):
		status = http.StatusUnauthorized
	}

	a.renderPage(w, status, PageData{
		Error:          message,
		QuotaThreshold: a.config.QuotaThreshold,
	})
}

func (a *App) renderPage(w http.ResponseWriter, status int, data PageData) {
	var buf bytes.Buffer
	if err := a.tpl.Execute(&buf, data); err != nil {
		http.Error(w, "页面渲染失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (a *App) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.renderPage(w, http.StatusInternalServerError, PageData{
					Error:          fmt.Sprintf("服务内部错误: %v", rec),
					QuotaThreshold: a.config.QuotaThreshold,
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func parseTemplate() (*template.Template, error) {
	return template.New("home.html").Funcs(template.FuncMap{
		"formatQuotaYuan": formatQuotaYuan,
		"formatQuotaRaw":  formatQuotaRaw,
	}).ParseFS(templateFS, "templates/home.html")
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

func formatQuotaRaw(value int64) string {
	return strconv.FormatInt(value, 10)
}
