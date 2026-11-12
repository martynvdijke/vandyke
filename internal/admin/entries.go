package admin

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/upload"
	"vandyke.thor.edu/vandyke/internal/webhook"
)

type pageData struct {
	Admin  store.Admin
	Umami  config.Umami
	CSRF   string
	Ledger ledgerData
	Flash  string
}

type ledgerData struct {
	Entries   []store.Entry
	Years     []int
	Q         string
	Sort      string
	Order     string
	Person    string
	Page      int
	Pages     int
	PerPage   int
	From      int
	To        int
	Total     int
	PhotoOnly bool
	Year      int
}

type editData struct {
	Admin       store.Admin
	Umami       config.Umami
	CSRF        string
	Entry       store.Entry
	Tags        []tagOption
	FormWord    string
	FormMeaning string
	FormPerson  string
	FormDate    string
	FormRemove  bool
	Error       string
}

type trashData struct {
	Admin   store.Admin
	Umami   config.Umami
	CSRF    string
	Entries []store.Entry
	Flash   string
}

func (a *Admin) handleIndex(rw http.ResponseWriter, r *http.Request) {
	ledger, err := a.buildLedger(r)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	flash, _ := a.flashMessage(r)
	state := stateFrom(r)
	a.render(rw, http.StatusOK, "admin-index", pageData{
		Admin: state.Admin, Umami: a.settings.Current(), CSRF: state.Session.CSRFToken,
		Ledger: ledger, Flash: flash,
	})
}

func (a *Admin) handleEntries(rw http.ResponseWriter, r *http.Request) {
	ledger, err := a.buildLedger(r)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	state := stateFrom(r)
	a.render(rw, http.StatusOK, "admin-entries", pageData{
		Admin: state.Admin, Umami: a.settings.Current(), CSRF: state.Session.CSRFToken,
		Ledger: ledger,
	})
}

func (a *Admin) buildLedger(r *http.Request) (ledgerData, error) {
	params := store.NormalizeListParams(parseAdminListParams(r))
	page, err := a.store.ListEntries(r.Context(), params)
	if err != nil {
		return ledgerData{}, err
	}
	years, err := a.store.EntryYears(r.Context())
	if err != nil {
		return ledgerData{}, err
	}
	d := ledgerData{
		Entries:   page.Entries,
		Years:     years,
		Q:         params.Query,
		Sort:      params.Sort,
		Order:     params.Order,
		Person:    params.Person,
		Page:      page.Page,
		Pages:     page.Pages,
		PerPage:   page.PerPage,
		Total:     page.Total,
		PhotoOnly: params.HasPhoto != nil && *params.HasPhoto,
		Year:      params.Year,
	}
	if page.Total > 0 && len(page.Entries) > 0 {
		d.From = (page.Page-1)*page.PerPage + 1
		d.To = d.From + len(page.Entries) - 1
	}
	return d, nil
}

func parseAdminListParams(r *http.Request) store.ListParams {
	_ = r.ParseForm()
	return store.ListParams{
		Query:    r.FormValue("q"),
		Sort:     r.FormValue("sort"),
		Order:    r.FormValue("order"),
		Person:   r.FormValue("person"),
		Page:     adminFormInt(r, "page", 1),
		PerPage:  adminFormInt(r, "per_page", store.DefaultPerPage),
		HasPhoto: adminFormOptionalBool(r, "photo"),
		Year:     adminFormInt(r, "year", 0),
	}
}

func adminFormOptionalBool(r *http.Request, key string) *bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue(key))) {
	case "1", "true", "yes":
		value := true
		return &value
	case "0", "false", "no":
		value := false
		return &value
	}
	return nil
}

func adminFormInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.FormValue(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func (d ledgerData) values() url.Values {
	v := url.Values{}
	if d.Q != "" {
		v.Set("q", d.Q)
	}
	if d.Person != "" {
		v.Set("person", d.Person)
	}
	v.Set("sort", d.Sort)
	v.Set("order", d.Order)
	v.Set("per_page", strconv.Itoa(d.PerPage))
	if d.PhotoOnly {
		v.Set("photo", "1")
	}
	if d.Year > 0 {
		v.Set("year", strconv.Itoa(d.Year))
	}
	return v
}

// SortHref builds the full-page URL for a column header.
func (d ledgerData) SortHref(column string) string {
	v := d.sortValues(column)
	return "/admin?" + v.Encode()
}

// SortHX builds the HTMX partial URL for a column header.
func (d ledgerData) SortHX(column string) string {
	v := d.sortValues(column)
	return "/admin/entries?" + v.Encode()
}

func (d ledgerData) sortValues(column string) url.Values {
	order := "asc"
	switch {
	case d.Sort == column && d.Order == "asc":
		order = "desc"
	case d.Sort != column && (column == "created_at" || column == "id"):
		order = "desc"
	}
	v := d.values()
	v.Set("sort", column)
	v.Set("order", order)
	return v
}

// SortMark returns the arrow for the active sort column.
func (d ledgerData) SortMark(column string) string {
	if d.Sort != column {
		return ""
	}
	if d.Order == "asc" {
		return "↑"
	}
	return "↓"
}

// PageHref builds the full-page pagination URL.
func (d ledgerData) PageHref(n int) string {
	v := d.values()
	v.Set("page", strconv.Itoa(n))
	return "/admin?" + v.Encode()
}

// PageHX builds the HTMX pagination URL.
func (d ledgerData) PageHX(n int) string {
	v := d.values()
	v.Set("page", strconv.Itoa(n))
	return "/admin/entries?" + v.Encode()
}

func (a *Admin) handleOriginalPhoto(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid entry id", http.StatusBadRequest)
		return
	}
	// Admins may inspect the photo of a trashed entry before restoring or
	// purging it, so this route accepts deleted rows.
	if _, err := a.store.GetEntryIncludingDeleted(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(rw, r)
			return
		}
		a.internalError(rw, err)
		return
	}
	photo, err := a.store.GetEntryPhotoSize(r.Context(), id, "original")
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}
	rw.Header().Set("Content-Type", photo.ContentType)
	rw.Header().Set("Content-Length", strconv.Itoa(len(photo.Data)))
	rw.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("entry-%04d%s", id, photoExtension(photo.ContentType))))
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = rw.Write(photo.Data)
}

func photoExtension(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func (a *Admin) handleEdit(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid entry id", http.StatusBadRequest)
		return
	}
	entry, err := a.store.GetEntryIncludingDeleted(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}
	state := stateFrom(r)
	current, err := a.store.EntryTags(r.Context(), id)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	selected := make([]int64, 0, len(current))
	for _, tag := range current {
		selected = append(selected, tag.ID)
	}
	tags, err := a.tagOptions(r.Context(), selected)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.render(rw, http.StatusOK, "admin-edit", editData{
		Admin:       state.Admin,
		Umami:       a.settings.Current(),
		CSRF:        state.Session.CSRFToken,
		Entry:       entry,
		Tags:        tags,
		FormWord:    entry.Word,
		FormMeaning: entry.Meaning,
		FormPerson:  entry.Person,
		FormDate:    entry.CreatedAt.Format("2006-01-02"),
	})
}

func (a *Admin) handleUpdate(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid entry id", http.StatusBadRequest)
		return
	}
	entry, err := a.store.GetEntryIncludingDeleted(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}

	_ = r.ParseForm()
	word := strings.TrimSpace(r.FormValue("word"))
	meaning := strings.TrimSpace(r.FormValue("meaning"))
	person := entry.Person
	if _, ok := r.PostForm["person"]; ok {
		person = strings.TrimSpace(r.PostFormValue("person"))
	}
	dateRaw := strings.TrimSpace(r.FormValue("date"))
	removePhoto := r.FormValue("remove_photo") == "1"

	createdAt := entry.CreatedAt
	if dateRaw != "" && dateRaw != entry.CreatedAt.Format("2006-01-02") {
		parsed, parseErr := time.Parse("2006-01-02", dateRaw)
		if parseErr != nil {
			a.renderEditError(rw, r, entry, word, meaning, person, dateRaw, removePhoto,
				"Use a valid date in YYYY-MM-DD format.")
			return
		}
		createdAt = time.Date(parsed.Year(), parsed.Month(), parsed.Day(),
			entry.CreatedAt.Hour(), entry.CreatedAt.Minute(), entry.CreatedAt.Second(),
			entry.CreatedAt.Nanosecond(), time.UTC)
	}

	photo, err := upload.Photo(r, "photo")
	if err != nil {
		a.renderEditError(rw, r, entry, word, meaning, person, dateRaw, removePhoto, capitalize(err.Error()))
		return
	}
	previous, updated, err := a.store.UpdateEntry(r.Context(), store.EntryUpdate{
		ID: id, Word: word, Meaning: meaning, Person: person, CreatedAt: createdAt,
		Photo: photo, RemovePhoto: removePhoto,
	})
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.renderEditError(rw, r, entry, word, meaning, person, dateRaw, removePhoto, capitalize(err.Error()))
		return
	}
	// The submitted checkboxes are the complete set for this entry; an empty
	// submission clears every tag.
	if err := a.store.SetEntryTags(r.Context(), id, parseTagIDs(r.Form["tags"])); err != nil {
		a.internalError(rw, err)
		return
	}
	resulting, err := a.store.EntryTags(r.Context(), id)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	slugs := make([]string, 0, len(resulting))
	for _, tag := range resulting {
		slugs = append(slugs, tag.Slug)
	}
	sort.Strings(slugs)
	changes, changed := entryChanges(previous, updated)
	if len(changes) > 0 {
		a.hooks.Emit(webhook.Change{
			Event:   webhook.EventEntryUpdated,
			Entry:   updated,
			Changes: changes,
			BaseURL: httpx.BaseURL(r, a.cfg.PublicBaseURL, a.cfg.TrustProxy),
		})
	}
	detail := "no field changed"
	if len(changed) > 0 {
		detail = "changed: " + strings.Join(changed, ",")
	}
	a.auditAs(r, "entry.update", strconv.FormatInt(id, 10), detail)
	a.auditAs(r, "entry.tags", strconv.FormatInt(id, 10), "tags="+strings.Join(slugs, ","))
	http.Redirect(rw, r, fmt.Sprintf("/admin?updated=1#entry-%d", id), http.StatusSeeOther)
}

func (a *Admin) renderEditError(rw http.ResponseWriter, r *http.Request, entry store.Entry,
	word, meaning, person, date string, remove bool, message string) {
	state := stateFrom(r)
	tags, err := a.tagOptions(r.Context(), parseTagIDs(r.Form["tags"]))
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.render(rw, http.StatusUnprocessableEntity, "admin-edit", editData{
		Admin:       state.Admin,
		Umami:       a.settings.Current(),
		CSRF:        state.Session.CSRFToken,
		Entry:       entry,
		Tags:        tags,
		FormWord:    word,
		FormMeaning: meaning,
		FormPerson:  person,
		FormDate:    date,
		FormRemove:  remove,
		Error:       message,
	})
}

func (a *Admin) handleDelete(rw http.ResponseWriter, r *http.Request) {
	ids, ok := parseBulkIDs(rw, r)
	if !ok {
		return
	}
	removed, err := a.store.SoftDeleteEntries(r.Context(), ids)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.auditAs(r, "entry.delete", joinIDs(ids), fmt.Sprintf("count=%d", len(removed)))
	a.hooks.EmitBatch(webhook.ChangesFor(webhook.EventEntryDeleted, removed,
		httpx.BaseURL(r, a.cfg.PublicBaseURL, a.cfg.TrustProxy)))
	http.Redirect(rw, r, fmt.Sprintf("/admin?deleted=%d", len(removed)), http.StatusSeeOther)
}

// parseBulkIDs reads and validates the `ids` form field. It mirrors the
// historical delete handler: an empty selection is a 400, more than 100 ids is
// a 400, and any malformed id rejects the whole request.
func parseBulkIDs(rw http.ResponseWriter, r *http.Request) ([]int64, bool) {
	rawIDs := r.Form["ids"]
	if len(rawIDs) == 0 {
		http.Error(rw, "no entries selected", http.StatusBadRequest)
		return nil, false
	}
	if len(rawIDs) > 100 {
		http.Error(rw, "too many entries selected", http.StatusBadRequest)
		return nil, false
	}
	ids := make([]int64, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := parseID(raw)
		if err != nil {
			http.Error(rw, "invalid entry id", http.StatusBadRequest)
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func (a *Admin) handleTrash(rw http.ResponseWriter, r *http.Request) {
	entries, err := a.store.ListTrashedEntries(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	flash, _ := a.flashMessage(r)
	state := stateFrom(r)
	a.render(rw, http.StatusOK, "admin-trash", trashData{
		Admin: state.Admin, Umami: a.settings.Current(), CSRF: state.Session.CSRFToken,
		Entries: entries, Flash: flash,
	})
}

func (a *Admin) handleTrashRestore(rw http.ResponseWriter, r *http.Request) {
	ids, ok := parseBulkIDs(rw, r)
	if !ok {
		return
	}
	restored, err := a.store.RestoreEntries(r.Context(), ids)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.auditAs(r, "entry.restore", joinIDs(ids), fmt.Sprintf("count=%d", len(restored)))
	a.hooks.EmitBatch(webhook.ChangesFor(webhook.EventEntryRestored, restored,
		httpx.BaseURL(r, a.cfg.PublicBaseURL, a.cfg.TrustProxy)))
	http.Redirect(rw, r, fmt.Sprintf("/admin/trash?restored=%d", len(restored)), http.StatusSeeOther)
}

func (a *Admin) handleTrashPurge(rw http.ResponseWriter, r *http.Request) {
	ids, ok := parseBulkIDs(rw, r)
	if !ok {
		return
	}
	purged, err := a.store.PurgeEntries(r.Context(), ids)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.auditAs(r, "entry.purge", joinIDs(ids), fmt.Sprintf("count=%d", len(purged)))
	a.hooks.EmitBatch(webhook.ChangesFor(webhook.EventEntryPurged, purged,
		httpx.BaseURL(r, a.cfg.PublicBaseURL, a.cfg.TrustProxy)))
	http.Redirect(rw, r, fmt.Sprintf("/admin/trash?purged=%d", len(purged)), http.StatusSeeOther)
}

// entryChanges builds the bounded webhook diff and the audit field list from a
// previous and updated entry snapshot. A no-op edit returns empty maps.
func entryChanges(previous, updated store.Entry) (map[string]webhook.FieldChange, []string) {
	changes := make(map[string]webhook.FieldChange)
	if previous.Word != updated.Word {
		changes["word"] = webhook.FieldChange{From: previous.Word, To: updated.Word}
	}
	if previous.Meaning != updated.Meaning {
		changes["meaning"] = webhook.FieldChange{From: previous.Meaning, To: updated.Meaning}
	}
	if previous.Person != updated.Person {
		changes["person"] = webhook.FieldChange{From: previous.Person, To: updated.Person}
	}
	if !previous.CreatedAt.Equal(updated.CreatedAt) {
		changes["created_at"] = webhook.FieldChange{
			From: previous.CreatedAt.UTC().Format(time.RFC3339),
			To:   updated.CreatedAt.UTC().Format(time.RFC3339),
		}
	}
	if previous.PhotoSHA256 != updated.PhotoSHA256 {
		changes["photo"] = webhook.FieldChange{
			From: photoHash(previous.PhotoSHA256),
			To:   photoHash(updated.PhotoSHA256),
		}
	}
	changed := make([]string, 0, len(changes))
	for _, field := range []string{"word", "meaning", "person", "created_at", "photo"} {
		if _, ok := changes[field]; ok {
			changed = append(changed, field)
		}
	}
	return changes, changed
}

// photoHash truncates a photo digest to the 12 hex characters the payload
// contract uses, or null when there is no photo.
func photoHash(sum string) any {
	if len(sum) < 12 {
		return nil
	}
	return sum[:12]
}

// joinIDs renders entry ids for an audit subject.
func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}
