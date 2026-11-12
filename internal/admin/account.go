package admin

import (
	"fmt"
	"net/http"

	"vandyke.thor.edu/vandyke/internal/auth"
	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

type accountData struct {
	Admin                  store.Admin
	Umami                  config.Umami
	CSRF                   string
	Error                  string
	Flash                  string
	HasLocal               bool
	Prefs                  store.AdminNotificationPrefs
	NotificationsAvailable bool
}

func (a *Admin) accountPage(r *http.Request, errMsg string) accountData {
	state := stateFrom(r)
	flash, _ := a.flashMessage(r)
	prefs, err := a.store.GetAdminNotificationPrefs(r.Context(), state.Admin.ID)
	if err != nil {
		a.logger.Error("load admin notification prefs", "admin_id", state.Admin.ID, "error", err)
		prefs = store.AdminNotificationPrefs{
			AdminID:      state.Admin.ID,
			Realtime:     true,
			WebPush:      true,
			EntryCreated: true,
		}
	}
	return accountData{
		Admin:                  state.Admin,
		Umami:                  a.settings.Current(),
		CSRF:                   state.Session.CSRFToken,
		Error:                  errMsg,
		Flash:                  flash,
		HasLocal:               state.Admin.HasPassword(),
		Prefs:                  prefs,
		NotificationsAvailable: a.settings.CurrentNotifications().Enabled,
	}
}

func (a *Admin) handleAccount(rw http.ResponseWriter, r *http.Request) {
	a.render(rw, http.StatusOK, "admin-account", a.accountPage(r, ""))
}

func (a *Admin) handleAccountPassword(rw http.ResponseWriter, r *http.Request) {
	state := stateFrom(r)
	current := r.FormValue("current_password")
	next := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	fail := func(status int, message string) {
		data := a.accountPage(r, message)
		a.render(rw, status, "admin-account", data)
	}

	if !state.Admin.HasPassword() {
		fail(http.StatusBadRequest, "This account signs in through the identity provider.")
		return
	}
	if !auth.VerifyPassword(state.Admin.PasswordHash, current) {
		fail(http.StatusUnprocessableEntity, "The current password is incorrect.")
		return
	}
	if len(next) < minPasswordLen || len(next) > 200 {
		fail(http.StatusUnprocessableEntity,
			fmt.Sprintf("Password must be between %d and 200 characters.", minPasswordLen))
		return
	}
	if next != confirm {
		fail(http.StatusUnprocessableEntity, "The new passwords do not match.")
		return
	}
	hash, err := auth.HashPassword(next)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	// Keep the session making the change, revoke every sibling session.
	if err := a.store.SetAdminPassword(r.Context(), state.Admin.ID, hash, state.Session.TokenHash); err != nil {
		a.internalError(rw, err)
		return
	}
	http.Redirect(rw, r, "/admin/account?flash=password-changed", http.StatusSeeOther)
}

// handleAccountNotifications stores the signed-in administrator's own
// notification toggles. The browser push opt-in is separate from the OS
// permission prompt; this only controls whether this server sends to it.
func (a *Admin) handleAccountNotifications(rw http.ResponseWriter, r *http.Request) {
	state := stateFrom(r)
	prefs := store.AdminNotificationPrefs{
		AdminID:      state.Admin.ID,
		Realtime:     r.FormValue("realtime_enabled") != "",
		WebPush:      r.FormValue("web_push_enabled") != "",
		EntryCreated: r.FormValue("entry_created") != "",
	}
	if err := a.store.SaveAdminNotificationPrefs(r.Context(), prefs); err != nil {
		a.internalError(rw, err)
		return
	}
	http.Redirect(rw, r, "/admin/account?flash=notifications-saved", http.StatusSeeOther)
}
