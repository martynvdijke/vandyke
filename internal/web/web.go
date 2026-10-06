// Package web renders the server-side HTML ledger with HTMX enhancements.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Web serves the HTML UI.
type Web struct {
	store      *store.Store
	limiter    *ratelimit.Limiter
	trustProxy bool
	umami      config.Umami
	logger     *slog.Logger
	tmpl       *template.Template
	assets     http.Handler
}

// New parses templates and prepares the static asset handler.
func New(st *store.Store, limiter *ratelimit.Limiter, umami config.Umami, trustProxy bool, logger *slog.Logger) (*Web, error) {
	tmpl, err := template.New("vandyke").Funcs(template.FuncMap{
		"formatDate": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Format("02 Jan 2006")
		},
		"formatDateP": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return "—"
			}
			return t.Format("02 Jan 2006")
		},
		"entryNo": func(id int64) string { return fmt.Sprintf("#%04d", id) },
		"inc":     func(i int) int { return i + 1 },
		"dec":     func(i int) int { return i - 1 },
	}).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("static assets: %w", err)
	}

	return &Web{
		store:      st,
		limiter:    limiter,
		trustProxy: trustProxy,
		umami:      umami,
		logger:     logger,
		tmpl:       tmpl,
		assets:     http.FileServer(http.FS(sub)),
	}, nil
}

// Register wires the UI routes into mux.
func (w *Web) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", w.handleIndex)
	mux.HandleFunc("GET /entries", w.handleLedger)
	mux.HandleFunc("POST /entries", w.handleCreate)
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheable(w.assets)))
}

type pageData struct {
	Umami       config.Umami
	Stats       store.Stats
	SpanYears   int
	Ledger      ledgerData
	Added       bool
	ShowAdd     bool
	ShowInfo    bool
	FormError   string
	FormWord    string
	FormMeaning string
	Year        int
}

type ledgerData struct {
	Entries   []store.Entry
	Query     ledgerQuery
	Total     int
	Pages     int
	From      int
	To        int
	PageLinks []pageLink
}

type ledgerQuery struct {
	Q       string
	Sort    string
	Order   string
	Page    int
	PerPage int
}

type pageLink struct {
	Num    int
	URL    string
	Active bool
	Gap    bool
}

func (q ledgerQuery) values() url.Values {
	v := url.Values{}
	if q.Q != "" {
		v.Set("q", q.Q)
	}
	v.Set("sort", q.Sort)
	v.Set("order", q.Order)
	v.Set("per_page", strconv.Itoa(q.PerPage))
	return v
}

// SortHref builds the target URL for clicking the given column header.
func (d ledgerData) SortHref(column string) string {
	order := "asc"
	switch {
	case d.Query.Sort == column && d.Query.Order == "asc":
		order = "desc"
	case d.Query.Sort != column && (column == "created_at" || column == "id"):
		order = "desc"
	}
	v := d.Query.values()
	v.Set("sort", column)
	v.Set("order", order)
	return "/entries?" + v.Encode()
}

// SortMark returns the arrow shown on the active sort column.
func (d ledgerData) SortMark(column string) string {
	if d.Query.Sort != column {
		return ""
	}
	if d.Query.Order == "asc" {
		return "↑"
	}
	return "↓"
}

// SortLabel describes the current ordering in words.
func (q ledgerQuery) SortLabel() string {
	switch q.Sort {
	case "word", "meaning":
		if q.Order == "asc" {
			return "A to Z"
		}
		return "Z to A"
	default:
		if q.Order == "asc" {
			return "oldest first"
		}
		return "newest first"
	}
}

// PageHref builds pagination URLs preserving the current query state.
func (d ledgerData) PageHref(n int) string {
	v := d.Query.values()
	v.Set("page", strconv.Itoa(n))
	return "/entries?" + v.Encode()
}

func (w *Web) handleIndex(rw http.ResponseWriter, r *http.Request) {
	data, err := w.buildPage(r)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	w.render(rw, http.StatusOK, "base", data)
}

func (w *Web) handleLedger(rw http.ResponseWriter, r *http.Request) {
	ledger, err := w.buildLedger(r)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	w.render(rw, http.StatusOK, "ledger", pageData{Umami: w.umami, Ledger: ledger})
}

func (w *Web) handleCreate(rw http.ResponseWriter, r *http.Request) {
	isHTMX := r.Header.Get("HX-Request") != ""
	ip := httpx.ClientIP(r, w.trustProxy)

	if !w.limiter.Allow(ip) {
		rw.Header().Set("Retry-After", strconv.Itoa(w.limiter.RetryAfterSeconds()))
		const message = "Too many entries added from this connection. Try again later."
		if isHTMX {
			w.renderFormError(rw, message)
		} else {
			w.renderIndexError(rw, r, http.StatusTooManyRequests, message, "", "")
		}
		return
	}

	_ = r.ParseForm()
	word := strings.TrimSpace(r.PostFormValue("word"))
	meaning := strings.TrimSpace(r.PostFormValue("meaning"))

	if err := store.ValidateEntry(word, meaning); err != nil {
		message := capitalize(err.Error())
		if isHTMX {
			w.renderFormError(rw, message)
		} else {
			w.renderIndexError(rw, r, http.StatusUnprocessableEntity, message, word, meaning)
		}
		return
	}

	entry, err := w.store.CreateEntry(r.Context(), word, meaning)
	if err != nil {
		w.logger.Error("create entry", "error", err)
		http.Error(rw, "could not add entry", http.StatusInternalServerError)
		return
	}

	if !isHTMX {
		http.Redirect(rw, r, "/?added=1", http.StatusSeeOther)
		return
	}

	trigger, _ := json.Marshal(map[string]any{
		"entryAdded": map[string]any{"id": entry.ID, "word": entry.Word},
	})
	rw.Header().Set("HX-Trigger-After-Swap", string(trigger))

	ledger, err := w.buildLedger(r)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	w.render(rw, http.StatusOK, "ledger", pageData{Umami: w.umami, Ledger: ledger})
}

func (w *Web) buildPage(r *http.Request) (pageData, error) {
	ledger, err := w.buildLedger(r)
	if err != nil {
		return pageData{}, err
	}
	stats, err := w.store.Stats(r.Context())
	if err != nil {
		return pageData{}, err
	}
	span := 1
	if stats.FirstEntryAt != nil && stats.LastEntryAt != nil {
		if years := stats.LastEntryAt.Year() - stats.FirstEntryAt.Year() + 1; years > 0 {
			span = years
		}
	}
	query := r.URL.Query()
	return pageData{
		Umami:     w.umami,
		Stats:     stats,
		SpanYears: span,
		Ledger:    ledger,
		Added:     query.Get("added") == "1",
		ShowAdd:   query.Get("add") == "1",
		ShowInfo:  query.Get("info") == "1",
		Year:      time.Now().Year(),
	}, nil
}

func (w *Web) buildLedger(r *http.Request) (ledgerData, error) {
	params := store.NormalizeListParams(parseListParams(r))
	page, err := w.store.ListEntries(r.Context(), params)
	if err != nil {
		return ledgerData{}, err
	}
	d := ledgerData{
		Entries: page.Entries,
		Query: ledgerQuery{
			Q:       params.Query,
			Sort:    params.Sort,
			Order:   params.Order,
			Page:    page.Page,
			PerPage: page.PerPage,
		},
		Total: page.Total,
		Pages: page.Pages,
	}
	if page.Total > 0 && len(page.Entries) > 0 {
		d.From = (page.Page-1)*page.PerPage + 1
		d.To = d.From + len(page.Entries) - 1
	}
	d.PageLinks = buildPageLinks(page.Page, page.Pages, d.PageHref)
	return d, nil
}

func parseListParams(r *http.Request) store.ListParams {
	_ = r.ParseForm()
	return store.ListParams{
		Query:   r.FormValue("q"),
		Sort:    r.FormValue("sort"),
		Order:   r.FormValue("order"),
		Page:    formInt(r, "page", 1),
		PerPage: formInt(r, "per_page", store.DefaultPerPage),
	}
}

func formInt(r *http.Request, key string, fallback int) int {
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

func buildPageLinks(current, total int, href func(int) string) []pageLink {
	if total <= 1 {
		return nil
	}
	const window = 2
	lo, hi := current-window, current+window
	if lo < 1 {
		lo = 1
	}
	if hi > total {
		hi = total
	}

	links := make([]pageLink, 0, 8)
	add := func(n int) {
		links = append(links, pageLink{Num: n, URL: href(n), Active: n == current})
	}

	if lo > 1 {
		add(1)
		if lo > 2 {
			links = append(links, pageLink{Gap: true})
		}
	}
	for n := lo; n <= hi; n++ {
		add(n)
	}
	if hi < total {
		if hi < total-1 {
			links = append(links, pageLink{Gap: true})
		}
		add(total)
	}
	return links
}

func (w *Web) renderFormError(rw http.ResponseWriter, message string) {
	rw.Header().Set("HX-Retarget", "#form-error")
	rw.Header().Set("HX-Reswap", "innerHTML")
	w.render(rw, http.StatusOK, "form-error", message)
}

func (w *Web) renderIndexError(rw http.ResponseWriter, r *http.Request, status int, message, word, meaning string) {
	page, err := w.buildPage(r)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	page.ShowAdd = true
	page.FormError = message
	page.FormWord = word
	page.FormMeaning = meaning
	w.render(rw, status, "base", page)
}

func (w *Web) renderInternalError(rw http.ResponseWriter, err error) {
	w.logger.Error("internal error", "error", err)
	http.Error(rw, "internal server error", http.StatusInternalServerError)
}

func (w *Web) render(rw http.ResponseWriter, status int, name string, data any) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(status)
	if err := w.tmpl.ExecuteTemplate(rw, name, data); err != nil {
		w.logger.Error("render template", "template", name, "error", err)
	}
}

func cacheable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(rw, r)
	})
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
