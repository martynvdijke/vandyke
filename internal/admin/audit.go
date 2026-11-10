package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

// auditPageData backs the read-only audit viewer.
type auditPageData struct {
	Admin   store.Admin
	Umami   config.Umami
	CSRF    string
	Entries []store.AuditEntry
	Total   int
	Page    int
	Pages   int
	PerPage int

	FilterAction string
	FilterActor  string
	FilterFrom   string
	FilterTo     string
}

// recordAudit writes one audit row and logs, but never surfaces, a failure so
// the audited action still commits.
func (a *Admin) recordAudit(r *http.Request, actorID *int64, actorName, action, subject, detail string) {
	rec := store.AuditRecord{
		ActorID:   actorID,
		ActorName: actorName,
		Action:    action,
		Subject:   subject,
		Detail:    detail,
		IP:        clientIP(r, a.cfg.TrustProxy),
		CreatedAt: time.Now().UTC(),
	}
	if err := a.store.RecordAudit(r.Context(), rec); err != nil {
		a.logger.Error("record audit", "action", action, "error", err)
	}
}

// auditAs records an action for the authenticated admin in the request.
func (a *Admin) auditAs(r *http.Request, action, subject, detail string) {
	state := stateFrom(r)
	id := state.Admin.ID
	a.recordAudit(r, &id, adminActorName(state.Admin), action, subject, detail)
}

// adminActorName snapshots a human-identifiable name for the audit row.
func adminActorName(admin store.Admin) string {
	switch {
	case admin.Username != "":
		return admin.Username
	case admin.DisplayName != "":
		return admin.DisplayName
	default:
		return admin.Email
	}
}

// handleAudit renders the paginated, filterable audit trail. There is no route
// that edits or deletes a row.
func (a *Admin) handleAudit(rw http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := 1
	if n, err := strconv.Atoi(strings.TrimSpace(q.Get("page"))); err == nil && n > 0 {
		page = n
	}
	filter := store.AuditFilter{
		Action:  strings.TrimSpace(q.Get("action")),
		Actor:   strings.TrimSpace(q.Get("actor")),
		From:    strings.TrimSpace(q.Get("from")),
		To:      strings.TrimSpace(q.Get("to")),
		Page:    page,
		PerPage: 50,
	}
	pageData, err := a.store.ListAudit(r.Context(), filter)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	state := stateFrom(r)
	a.render(rw, http.StatusOK, "admin-audit", auditPageData{
		Admin:        state.Admin,
		Umami:        a.settings.Current(),
		CSRF:         state.Session.CSRFToken,
		Entries:      pageData.Entries,
		Total:        pageData.Total,
		Page:         pageData.Page,
		Pages:        pageData.Pages,
		PerPage:      pageData.PerPage,
		FilterAction: filter.Action,
		FilterActor:  filter.Actor,
		FilterFrom:   filter.From,
		FilterTo:     filter.To,
	})
}
