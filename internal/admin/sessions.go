package admin

import (
	"net/http"
	"strconv"
	"strings"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

type sessionsData struct {
	Admin    store.Admin
	Umami    config.Umami
	CSRF     string
	Sessions []store.SessionView
	Error    string
	Flash    string
}

func (a *Admin) handleSessions(rw http.ResponseWriter, r *http.Request) {
	state := stateFrom(r)
	sessions, err := a.store.ListSessionsForAdmin(r.Context(), state.Admin.ID, state.Session.TokenHash)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	flash, _ := a.flashMessage(r)
	a.render(rw, http.StatusOK, "admin-sessions", sessionsData{
		Admin:    state.Admin,
		Umami:    a.settings.Current(),
		CSRF:     state.Session.CSRFToken,
		Sessions: sessions,
		Flash:    flash,
	})
}

func (a *Admin) handleSessionRevoke(rw http.ResponseWriter, r *http.Request) {
	state := stateFrom(r)
	ref := strings.TrimSpace(r.FormValue("ref"))
	if ref == "" {
		a.renderSessionsError(rw, r, http.StatusBadRequest, "No session was selected.")
		return
	}
	// Scope to the acting admin; an owner may revoke any reference.
	scope := state.Admin.ID
	if state.Admin.IsOwner() {
		scope = 0
	}
	n, err := a.store.DeleteSessionByRef(r.Context(), ref, scope)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if n == 0 {
		a.renderSessionsError(rw, r, http.StatusForbidden, "That session does not belong to you.")
		return
	}
	a.auditAs(r, "session.revoke", ref, "count="+strconv.FormatInt(n, 10))
	http.Redirect(rw, r, "/admin/sessions?flash=session-revoked", http.StatusSeeOther)
}

func (a *Admin) handleRevokeAllSessions(rw http.ResponseWriter, r *http.Request) {
	state := stateFrom(r)
	n, err := a.store.DeleteOtherSessionsForAdmin(r.Context(), state.Admin.ID, state.Session.TokenHash)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.auditAs(r, "session.revoke_all", strconv.FormatInt(state.Admin.ID, 10), "count="+strconv.FormatInt(n, 10))
	http.Redirect(rw, r, "/admin/sessions?flash=sessions-revoked", http.StatusSeeOther)
}

func (a *Admin) renderSessionsError(rw http.ResponseWriter, r *http.Request, status int, message string) {
	state := stateFrom(r)
	sessions, err := a.store.ListSessionsForAdmin(r.Context(), state.Admin.ID, state.Session.TokenHash)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.render(rw, status, "admin-sessions", sessionsData{
		Admin:    state.Admin,
		Umami:    a.settings.Current(),
		CSRF:     state.Session.CSRFToken,
		Sessions: sessions,
		Error:    message,
	})
}
