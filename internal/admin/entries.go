package admin

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/upload"
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
	FormWord    string
	FormMeaning string
	FormDate    string
	FormRemove  bool
	Error       string
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
	entry, err := a.store.GetEntry(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}
	state := stateFrom(r)
	a.render(rw, http.StatusOK, "admin-edit", editData{
		Admin:       state.Admin,
		Umami:       a.settings.Current(),
		CSRF:        state.Session.CSRFToken,
		Entry:       entry,
		FormWord:    entry.Word,
		FormMeaning: entry.Meaning,
		FormDate:    entry.CreatedAt.Format("2006-01-02"),
	})
}

func (a *Admin) handleUpdate(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid entry id", http.StatusBadRequest)
		return
	}
	entry, err := a.store.GetEntry(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}

	word := strings.TrimSpace(r.FormValue("word"))
	meaning := strings.TrimSpace(r.FormValue("meaning"))
	dateRaw := strings.TrimSpace(r.FormValue("date"))
	removePhoto := r.FormValue("remove_photo") == "1"

	createdAt := entry.CreatedAt
	if dateRaw != "" && dateRaw != entry.CreatedAt.Format("2006-01-02") {
		parsed, parseErr := time.Parse("2006-01-02", dateRaw)
		if parseErr != nil {
			a.renderEditError(rw, r, entry, word, meaning, dateRaw, removePhoto,
				"Use a valid date in YYYY-MM-DD format.")
			return
		}
		createdAt = time.Date(parsed.Year(), parsed.Month(), parsed.Day(),
			entry.CreatedAt.Hour(), entry.CreatedAt.Minute(), entry.CreatedAt.Second(),
			entry.CreatedAt.Nanosecond(), time.UTC)
	}

	photo, err := upload.Photo(r, "photo")
	if err != nil {
		a.renderEditError(rw, r, entry, word, meaning, dateRaw, removePhoto, capitalize(err.Error()))
		return
	}
	updated, err := a.store.UpdateEntry(r.Context(), store.EntryUpdate{
		ID: id, Word: word, Meaning: meaning, CreatedAt: createdAt,
		Photo: photo, RemovePhoto: removePhoto,
	})
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.renderEditError(rw, r, entry, word, meaning, dateRaw, removePhoto, capitalize(err.Error()))
		return
	}
	_ = updated
	http.Redirect(rw, r, fmt.Sprintf("/admin?updated=1#entry-%d", id), http.StatusSeeOther)
}

func (a *Admin) renderEditError(rw http.ResponseWriter, r *http.Request, entry store.Entry,
	word, meaning, date string, remove bool, message string) {
	state := stateFrom(r)
	a.render(rw, http.StatusUnprocessableEntity, "admin-edit", editData{
		Admin:       state.Admin,
		Umami:       a.settings.Current(),
		CSRF:        state.Session.CSRFToken,
		Entry:       entry,
		FormWord:    word,
		FormMeaning: meaning,
		FormDate:    date,
		FormRemove:  remove,
		Error:       message,
	})
}

func (a *Admin) handleDelete(rw http.ResponseWriter, r *http.Request) {
	rawIDs := r.Form["ids"]
	if len(rawIDs) == 0 {
		http.Error(rw, "no entries selected", http.StatusBadRequest)
		return
	}
	if len(rawIDs) > 100 {
		http.Error(rw, "too many entries selected", http.StatusBadRequest)
		return
	}
	ids := make([]int64, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := parseID(raw)
		if err != nil {
			http.Error(rw, "invalid entry id", http.StatusBadRequest)
			return
		}
		ids = append(ids, id)
	}
	n, err := a.store.DeleteEntries(r.Context(), ids)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	http.Redirect(rw, r, fmt.Sprintf("/admin?deleted=%d", n), http.StatusSeeOther)
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
