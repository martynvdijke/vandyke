package admin

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"vandyke.thor.edu/vandyke/internal/auth"
	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

type loginPage struct {
	Umami    config.Umami
	Error    string
	Next     string
	Username string
	OIDC     bool
}

type setupPage struct {
	Umami         config.Umami
	Error         string
	TokenRequired bool
	Token         string
	Username      string
}

func (a *Admin) handleLoginForm(rw http.ResponseWriter, r *http.Request) {
	count, err := a.store.AdminCount(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if count == 0 {
		http.Redirect(rw, r, "/admin/setup", http.StatusSeeOther)
		return
	}
	if _, ok := a.session(r); ok {
		http.Redirect(rw, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	a.render(rw, http.StatusOK, "login", loginPage{
		Umami: a.settings.Current(),
		Next:  r.URL.Query().Get("next"),
		OIDC:  a.oidc != nil,
	})
}

func (a *Admin) handleLogin(rw http.ResponseWriter, r *http.Request) {
	if !a.parseBody(rw, r, maxAdminFormBytes) {
		return
	}
	count, err := a.store.AdminCount(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if count == 0 {
		http.Redirect(rw, r, "/admin/setup", http.StatusSeeOther)
		return
	}
	next := safeNext(r.FormValue("next"))
	username := strings.TrimSpace(r.FormValue("username"))
	if !a.loginLimiter.Allow("login:" + clientIP(r, a.cfg.TrustProxy)) {
		rw.Header().Set("Retry-After", "60")
		a.render(rw, http.StatusTooManyRequests, "login", loginPage{
			Umami: a.settings.Current(), Next: next, Username: username, OIDC: a.oidc != nil,
			Error: "Too many sign-in attempts. Try again in a minute.",
		})
		return
	}
	admin, err := a.store.GetAdminByUsername(r.Context(), username)
	if err != nil || !admin.HasPassword() || !auth.VerifyPassword(admin.PasswordHash, r.FormValue("password")) {
		a.render(rw, http.StatusUnauthorized, "login", loginPage{
			Umami: a.settings.Current(), Next: next, Username: username, OIDC: a.oidc != nil,
			Error: "Invalid username or password.",
		})
		return
	}
	if err := a.startSession(rw, r, admin); err != nil {
		a.internalError(rw, err)
		return
	}
	http.Redirect(rw, r, next, http.StatusSeeOther)
}

func (a *Admin) handleLogout(rw http.ResponseWriter, r *http.Request) {
	a.clearSession(rw, r)
	http.Redirect(rw, r, "/admin/login", http.StatusSeeOther)
}

func (a *Admin) handleSetupForm(rw http.ResponseWriter, r *http.Request) {
	count, err := a.store.AdminCount(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if count > 0 {
		http.Redirect(rw, r, "/admin/login", http.StatusSeeOther)
		return
	}
	a.render(rw, http.StatusOK, "setup", setupPage{
		Umami:         a.settings.Current(),
		TokenRequired: a.cfg.AdminSetupToken != "",
	})
}

func (a *Admin) handleSetup(rw http.ResponseWriter, r *http.Request) {
	if !a.parseBody(rw, r, maxAdminFormBytes) {
		return
	}
	count, err := a.store.AdminCount(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if count > 0 {
		http.Redirect(rw, r, "/admin/login", http.StatusSeeOther)
		return
	}
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")
	token := r.FormValue("token")

	fail := func(status int, message string) {
		a.render(rw, status, "setup", setupPage{
			Umami:         a.settings.Current(),
			Error:         message,
			TokenRequired: a.cfg.AdminSetupToken != "",
			Token:         token,
			Username:      username,
		})
	}

	if !a.loginLimiter.Allow("setup:" + clientIP(r, a.cfg.TrustProxy)) {
		fail(http.StatusTooManyRequests, "Too many attempts. Try again in a minute.")
		return
	}
	if a.cfg.AdminSetupToken != "" && !auth.TokensEqual(token, a.cfg.AdminSetupToken) {
		fail(http.StatusForbidden, "The setup token is incorrect.")
		return
	}
	if !usernamePattern.MatchString(username) {
		fail(http.StatusUnprocessableEntity, "Username must be 3–64 characters: letters, numbers, dots, dashes or underscores.")
		return
	}
	if len(password) < 10 || len(password) > 200 {
		fail(http.StatusUnprocessableEntity, "Password must be between 10 and 200 characters.")
		return
	}
	if password != confirm {
		fail(http.StatusUnprocessableEntity, "The passwords do not match.")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	admin, err := a.store.CreateFirstAdmin(r.Context(), username, hash)
	if errors.Is(err, store.ErrAdminExists) {
		http.Redirect(rw, r, "/admin/login", http.StatusSeeOther)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if err := a.startSession(rw, r, admin); err != nil {
		a.internalError(rw, err)
		return
	}
	http.Redirect(rw, r, "/admin", http.StatusSeeOther)
}

func (a *Admin) handleOIDCStart(rw http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		http.NotFound(rw, r)
		return
	}
	count, err := a.store.AdminCount(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if count == 0 {
		http.Redirect(rw, r, "/admin/setup", http.StatusSeeOther)
		return
	}
	state, err := auth.RandomToken(32)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	nonce, err := auth.RandomToken(32)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	verifier, _, err := auth.NewPKCE()
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.setOIDCCookie(rw, r, oidcStateCookie, state)
	a.setOIDCCookie(rw, r, oidcNonceCookie, nonce)
	a.setOIDCCookie(rw, r, oidcPKCECookie, verifier)
	http.Redirect(rw, r, a.oidc.AuthCodeURL(a.oidcRedirectURL(r), state, nonce, verifier), http.StatusSeeOther)
}

func (a *Admin) handleOIDCCallback(rw http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		http.NotFound(rw, r)
		return
	}
	defer a.clearOIDCCookies(rw, r)
	fail := func(message string) {
		a.render(rw, http.StatusBadRequest, "login", loginPage{
			Umami: a.settings.Current(), OIDC: true, Error: message,
		})
	}
	if providerErr := r.URL.Query().Get("error"); providerErr != "" {
		fail("The identity provider rejected the sign-in: " + providerErr)
		return
	}
	stateCookie, err := r.Cookie(oidcStateCookie)
	if err != nil || stateCookie.Value == "" || !auth.TokensEqual(stateCookie.Value, r.URL.Query().Get("state")) {
		fail("The sign-in session expired or was invalid. Please try again.")
		return
	}
	nonceCookie, err := r.Cookie(oidcNonceCookie)
	if err != nil || nonceCookie.Value == "" {
		fail("The sign-in session expired. Please try again.")
		return
	}
	pkceCookie, err := r.Cookie(oidcPKCECookie)
	if err != nil || pkceCookie.Value == "" {
		fail("The sign-in session expired. Please try again.")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		fail("The identity provider did not return an authorization code.")
		return
	}
	identity, err := a.oidc.Exchange(r.Context(), code, a.oidcRedirectURL(r), pkceCookie.Value, nonceCookie.Value)
	if err != nil {
		a.logger.Warn("oidc exchange failed", "error", err)
		fail("Could not verify the identity provider response. Please try again.")
		return
	}
	displayName := identity.Name
	if displayName == "" {
		displayName = identity.Username
	}
	admin, err := a.store.UpsertOIDCAdmin(r.Context(), identity.Issuer, identity.Subject, identity.Email, displayName)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if err := a.startSession(rw, r, admin); err != nil {
		a.internalError(rw, err)
		return
	}
	http.Redirect(rw, r, "/admin", http.StatusSeeOther)
}

func (a *Admin) setOIDCCookie(rw http.ResponseWriter, r *http.Request, name, value string) {
	http.SetCookie(rw, &http.Cookie{
		Name: name, Value: value, Path: "/admin", HttpOnly: true,
		Secure: a.secureCookie(r), SameSite: http.SameSiteLaxMode, MaxAge: oidcCookieMaxAge,
	})
}

func (a *Admin) clearOIDCCookies(rw http.ResponseWriter, r *http.Request) {
	for _, name := range []string{oidcStateCookie, oidcNonceCookie, oidcPKCECookie} {
		http.SetCookie(rw, &http.Cookie{
			Name: name, Value: "", Path: "/admin", HttpOnly: true,
			Secure: a.secureCookie(r), SameSite: http.SameSiteLaxMode, MaxAge: -1,
		})
	}
}

func safeNext(raw string) string {
	if raw == "" {
		return "/admin"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "/admin"
	}
	if !strings.HasPrefix(parsed.Path, "/admin") || strings.HasPrefix(parsed.Path, "//") {
		return "/admin"
	}
	return parsed.RequestURI()
}
