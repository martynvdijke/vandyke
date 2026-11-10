package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/auth"
	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

// minPasswordLen is the minimum for a changed or reset password. First-run
// setup keeps its own historical limit.
const minPasswordLen = 12

type adminsData struct {
	Admin    store.Admin
	Umami    config.Umami
	CSRF     string
	Admins   []store.Admin
	Error    string
	FormRole string
}

// handleAdmins lists every admin account with its role and state.
func (a *Admin) handleAdmins(rw http.ResponseWriter, r *http.Request) {
	admins, err := a.store.ListAdmins(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	flash, _ := a.flashMessage(r)
	state := stateFrom(r)
	a.render(rw, http.StatusOK, "admin-admins", adminsData{
		Admin:    state.Admin,
		Umami:    a.settings.Current(),
		CSRF:     state.Session.CSRFToken,
		Admins:   admins,
		FormRole: store.RoleEditor,
		Error:    flash,
	})
}

// renderAdminsError re-renders the accounts page with a message and status.
func (a *Admin) renderAdminsError(rw http.ResponseWriter, r *http.Request, status int, message string) {
	admins, err := a.store.ListAdmins(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	state := stateFrom(r)
	a.render(rw, status, "admin-admins", adminsData{
		Admin:  state.Admin,
		Umami:  a.settings.Current(),
		CSRF:   state.Session.CSRFToken,
		Admins: admins,
		Error:  message,
	})
}

func (a *Admin) handleAdminCreate(rw http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	password := r.FormValue("password")
	role := strings.TrimSpace(r.FormValue("role"))
	if role == "" {
		role = store.RoleEditor
	}

	switch {
	case !usernamePattern.MatchString(username):
		a.renderAdminsError(rw, r, http.StatusUnprocessableEntity,
			"Username must be 3–64 characters: letters, numbers, dots, dashes or underscores.")
		return
	case len(password) < minPasswordLen || len(password) > 200:
		a.renderAdminsError(rw, r, http.StatusUnprocessableEntity,
			fmt.Sprintf("Password must be between %d and 200 characters.", minPasswordLen))
		return
	case !store.ValidRole(role):
		a.renderAdminsError(rw, r, http.StatusUnprocessableEntity, "Choose the owner or editor role.")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	created, err := a.store.CreateAdmin(r.Context(), username, displayName, hash, role)
	if errors.Is(err, store.ErrAdminExists) {
		a.renderAdminsError(rw, r, http.StatusConflict, "That username is already taken.")
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.auditAs(r, "admin.create", strconv.FormatInt(created.ID, 10), "role="+created.Role)
	http.Redirect(rw, r, "/admin/admins?flash=admin-created", http.StatusSeeOther)
}

func (a *Admin) handleAdminRole(rw http.ResponseWriter, r *http.Request) {
	id, ok := a.adminTargetID(rw, r)
	if !ok {
		return
	}
	role := strings.TrimSpace(r.FormValue("role"))
	if !store.ValidRole(role) {
		a.renderAdminsError(rw, r, http.StatusUnprocessableEntity, "Choose the owner or editor role.")
		return
	}
	state := stateFrom(r)
	if id == state.Admin.ID && role != store.RoleOwner {
		a.renderAdminsError(rw, r, http.StatusConflict, "You cannot demote your own account.")
		return
	}
	before, err := a.store.GetAdmin(r.Context(), id)
	if err != nil {
		a.adminStoreError(rw, r, err)
		return
	}
	updated, err := a.store.SetAdminRole(r.Context(), id, role)
	if err != nil {
		a.adminStoreError(rw, r, err)
		return
	}
	a.auditAs(r, "admin.role", strconv.FormatInt(id, 10), before.Role+"->"+updated.Role)
	http.Redirect(rw, r, "/admin/admins?flash=role-updated", http.StatusSeeOther)
}

func (a *Admin) handleAdminDisable(rw http.ResponseWriter, r *http.Request) {
	id, ok := a.adminTargetID(rw, r)
	if !ok {
		return
	}
	if id == stateFrom(r).Admin.ID {
		a.renderAdminsError(rw, r, http.StatusConflict, "You cannot disable your own account.")
		return
	}
	if err := a.store.DisableAdmin(r.Context(), id, time.Now().UTC()); err != nil {
		a.adminStoreError(rw, r, err)
		return
	}
	a.auditAs(r, "admin.disable", strconv.FormatInt(id, 10), "")
	http.Redirect(rw, r, "/admin/admins?flash=admin-disabled", http.StatusSeeOther)
}

func (a *Admin) handleAdminEnable(rw http.ResponseWriter, r *http.Request) {
	id, ok := a.adminTargetID(rw, r)
	if !ok {
		return
	}
	if err := a.store.EnableAdmin(r.Context(), id); err != nil {
		a.adminStoreError(rw, r, err)
		return
	}
	a.auditAs(r, "admin.enable", strconv.FormatInt(id, 10), "")
	http.Redirect(rw, r, "/admin/admins?flash=admin-enabled", http.StatusSeeOther)
}

func (a *Admin) handleAdminDelete(rw http.ResponseWriter, r *http.Request) {
	id, ok := a.adminTargetID(rw, r)
	if !ok {
		return
	}
	if id == stateFrom(r).Admin.ID {
		a.renderAdminsError(rw, r, http.StatusConflict, "You cannot delete your own account.")
		return
	}
	if err := a.store.DeleteAdmin(r.Context(), id); err != nil {
		a.adminStoreError(rw, r, err)
		return
	}
	a.auditAs(r, "admin.delete", strconv.FormatInt(id, 10), "")
	http.Redirect(rw, r, "/admin/admins?flash=admin-deleted", http.StatusSeeOther)
}

func (a *Admin) handleAdminPasswordReset(rw http.ResponseWriter, r *http.Request) {
	id, ok := a.adminTargetID(rw, r)
	if !ok {
		return
	}
	password := r.FormValue("password")
	if len(password) < minPasswordLen || len(password) > 200 {
		a.renderAdminsError(rw, r, http.StatusUnprocessableEntity,
			fmt.Sprintf("Password must be between %d and 200 characters.", minPasswordLen))
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if err := a.store.SetAdminPassword(r.Context(), id, hash, ""); err != nil {
		a.adminStoreError(rw, r, err)
		return
	}
	// No password value, hash or secret is written to the audit row.
	a.auditAs(r, "admin.password_reset", strconv.FormatInt(id, 10), "all sessions revoked")
	http.Redirect(rw, r, "/admin/admins?flash=password-reset", http.StatusSeeOther)
}

func (a *Admin) adminTargetID(rw http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid admin id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// adminStoreError maps the store's typed errors onto the documented statuses.
func (a *Admin) adminStoreError(rw http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrAdminNotFound):
		http.Error(rw, "admin not found", http.StatusNotFound)
	case errors.Is(err, store.ErrLastOwner):
		a.renderAdminsError(rw, r, http.StatusConflict,
			"Cannot remove the last enabled owner. Promote or enable another owner first.")
	default:
		a.internalError(rw, err)
	}
}
