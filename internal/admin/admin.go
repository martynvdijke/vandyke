// Package admin serves the authenticated ledger administration interface.
package admin

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/auth"
	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/settings"
	"vandyke.thor.edu/vandyke/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

const (
	sessionCookie     = "vandyke_admin"
	sessionTTL        = 7 * 24 * time.Hour
	oidcStateCookie   = "vandyke_oidc_state"
	oidcNonceCookie   = "vandyke_oidc_nonce"
	oidcPKCECookie    = "vandyke_oidc_pkce"
	oidcCookieMaxAge  = 600
	maxAdminFormBytes = 64 << 10
	maxAdminPhotoBody = store.MaxPhotoBytes + (1 << 20)
)

// Admin serves the HTML admin interface.
type Admin struct {
	store        *store.Store
	settings     *settings.Store
	cfg          config.Config
	logger       *slog.Logger
	tmpl         *template.Template
	oidc         *auth.OIDC
	loginLimiter *ratelimit.Limiter
}

type ctxKey struct{}

type requestState struct {
	Session store.Session
	Admin   store.Admin
}

// New prepares templates, the optional OIDC client and login limiter.
func New(ctx context.Context, st *store.Store, stg *settings.Store, cfg config.Config, logger *slog.Logger) (*Admin, error) {
	funcs := template.FuncMap{
		"formatDate": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Format("02 Jan 2006")
		},
		"formatDateTime": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Format("02 Jan 2006 15:04")
		},
		"entryNo": func(id int64) string { return fmt.Sprintf("#%04d", id) },
		"inc":     func(i int) int { return i + 1 },
		"dec":     func(i int) int { return i - 1 },
	}
	tmpl, err := template.New("admin").Funcs(funcs).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse admin templates: %w", err)
	}

	a := &Admin{
		store:        st,
		settings:     stg,
		cfg:          cfg,
		logger:       logger,
		tmpl:         tmpl,
		loginLimiter: ratelimit.NewPerMinute(cfg.AdminLoginRatePerMinute, cfg.AdminLoginRateBurst),
	}
	if cfg.OIDC.Enabled() {
		a.oidc, err = auth.NewOIDC(ctx, auth.OIDCConfig{
			Issuer:       cfg.OIDC.Issuer,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURL:  cfg.OIDC.RedirectURL,
		})
		if err != nil {
			return nil, err
		}
	}
	if err := st.DeleteExpiredSessions(ctx); err != nil {
		logger.Warn("clean expired sessions", "error", err)
	}
	return a, nil
}

// Register wires admin routes into mux.
func (a *Admin) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin", a.requireAuth(a.handleIndex))
	mux.HandleFunc("GET /admin/entries", a.requireAuth(a.handleEntries))
	mux.HandleFunc("GET /admin/entries/{id}/photo/original", a.requireAuth(a.handleOriginalPhoto))
	mux.HandleFunc("GET /admin/entries/{id}/edit", a.requireAuth(a.handleEdit))
	mux.HandleFunc("POST /admin/entries/{id}", a.withCSRF(maxAdminPhotoBody, a.handleUpdate))
	mux.HandleFunc("POST /admin/entries/delete", a.withCSRF(maxAdminFormBytes, a.handleDelete))
	mux.HandleFunc("GET /admin/settings", a.requireAuth(a.handleSettings))
	mux.HandleFunc("POST /admin/settings", a.withCSRF(maxAdminFormBytes, a.handleSaveSettings))

	mux.HandleFunc("GET /admin/login", a.handleLoginForm)
	mux.HandleFunc("POST /admin/login", a.handleLogin)
	mux.HandleFunc("POST /admin/logout", a.withCSRF(maxAdminFormBytes, a.handleLogout))
	mux.HandleFunc("GET /admin/setup", a.handleSetupForm)
	mux.HandleFunc("POST /admin/setup", a.handleSetup)
	mux.HandleFunc("GET /admin/oidc/start", a.handleOIDCStart)
	mux.HandleFunc("GET /admin/oidc/callback", a.handleOIDCCallback)
}

func (a *Admin) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Cache-Control", "no-store")
		info, ok := a.session(r)
		if !ok {
			count, err := a.store.AdminCount(r.Context())
			if err != nil {
				a.internalError(rw, err)
				return
			}
			if count == 0 {
				http.Redirect(rw, r, "/admin/setup", http.StatusSeeOther)
				return
			}
			nextURL := r.URL.RequestURI()
			http.Redirect(rw, r, "/admin/login?next="+url.QueryEscape(nextURL), http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, requestState{Session: info.Session, Admin: info.Admin})
		next(rw, r.WithContext(ctx))
	}
}

func (a *Admin) withCSRF(maxBytes int64, next http.HandlerFunc) http.HandlerFunc {
	return a.requireAuth(func(rw http.ResponseWriter, r *http.Request) {
		if !a.parseBody(rw, r, maxBytes) {
			return
		}
		if r.MultipartForm != nil {
			defer func() { _ = r.MultipartForm.RemoveAll() }()
		}
		state := stateFrom(r)
		if !auth.TokensEqual(r.FormValue("csrf"), state.Session.CSRFToken) {
			http.Error(rw, "invalid or missing CSRF token", http.StatusForbidden)
			return
		}
		next(rw, r)
	})
}

func (a *Admin) parseBody(rw http.ResponseWriter, r *http.Request, maxBytes int64) bool {
	r.Body = http.MaxBytesReader(rw, r.Body, maxBytes)
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err = r.ParseMultipartForm(64 << 10)
	} else {
		err = r.ParseForm()
	}
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		http.Error(rw, "request is too large", http.StatusRequestEntityTooLarge)
	} else {
		http.Error(rw, "could not read the form", http.StatusBadRequest)
	}
	return false
}

func (a *Admin) session(r *http.Request) (store.SessionInfo, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return store.SessionInfo{}, false
	}
	info, err := a.store.GetSession(r.Context(), auth.HashToken(cookie.Value))
	if err != nil {
		if !errors.Is(err, store.ErrSessionNotFound) {
			a.logger.Error("load admin session", "error", err)
		}
		return store.SessionInfo{}, false
	}
	return info, true
}

func (a *Admin) startSession(rw http.ResponseWriter, r *http.Request, admin store.Admin) error {
	token, err := auth.RandomToken(32)
	if err != nil {
		return err
	}
	csrf, err := auth.RandomToken(32)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	session := store.Session{
		TokenHash: auth.HashToken(token),
		AdminID:   admin.ID,
		CSRFToken: csrf,
		CreatedAt: now,
		ExpiresAt: now.Add(sessionTTL),
	}
	if err := a.store.CreateSession(r.Context(), session); err != nil {
		return err
	}
	if err := a.store.TouchAdmin(r.Context(), admin.ID, now); err != nil {
		a.logger.Warn("touch admin", "error", err)
	}
	http.SetCookie(rw, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   a.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return nil
}

func (a *Admin) clearSession(rw http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
		if err := a.store.DeleteSession(r.Context(), auth.HashToken(cookie.Value)); err != nil {
			a.logger.Warn("delete session", "error", err)
		}
	}
	http.SetCookie(rw, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		Secure:   a.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (a *Admin) secureCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return a.cfg.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (a *Admin) baseURL(r *http.Request) string {
	scheme := "http"
	if a.secureCookie(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (a *Admin) oidcRedirectURL(r *http.Request) string {
	if a.cfg.OIDC.RedirectURL != "" {
		return a.cfg.OIDC.RedirectURL
	}
	return a.baseURL(r) + "/admin/oidc/callback"
}

func stateFrom(r *http.Request) requestState {
	state, _ := r.Context().Value(ctxKey{}).(requestState)
	return state
}

func (a *Admin) render(rw http.ResponseWriter, status int, name string, data any) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(status)
	if err := a.tmpl.ExecuteTemplate(rw, name, data); err != nil {
		a.logger.Error("render admin template", "template", name, "error", err)
	}
}

func (a *Admin) internalError(rw http.ResponseWriter, err error) {
	a.logger.Error("admin internal error", "error", err)
	http.Error(rw, "internal server error", http.StatusInternalServerError)
}

func (a *Admin) flashMessage(r *http.Request) (string, string) {
	q := r.URL.Query()
	switch {
	case q.Get("updated") == "1":
		return "Entry updated.", ""
	case q.Get("saved") == "1":
		return "Settings saved.", ""
	}
	if n := q.Get("deleted"); n != "" {
		if count, err := strconv.Atoi(n); err == nil && count > 0 {
			return fmt.Sprintf("%d %s deleted.", count, plural(count, "entry", "entries")), ""
		}
	}
	return "", ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func clientIP(r *http.Request, trustProxy bool) string { return httpx.ClientIP(r, trustProxy) }
