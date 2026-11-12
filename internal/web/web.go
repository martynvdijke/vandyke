// Package web renders the server-side HTML ledger with HTMX enhancements.
package web

import (
	"embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/metrics"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/searchquery"
	"vandyke.thor.edu/vandyke/internal/slip"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/upload"
	"vandyke.thor.edu/vandyke/internal/webhook"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Web serves the HTML UI.
type Web struct {
	store         *store.Store
	limiter       *ratelimit.Limiter
	trustProxy    bool
	publicBaseURL string
	umami         func() config.Umami
	hooks         *webhook.Engine
	metrics       *metrics.Registry
	slip          *slip.Resolver
	logger        *slog.Logger
	tmpl          *template.Template
	assets        http.Handler
}

// New parses templates and prepares the static asset handler.
func New(st *store.Store, limiter *ratelimit.Limiter, umami func() config.Umami, hooks *webhook.Engine, trustProxy bool, publicBaseURL string, registry *metrics.Registry, logger *slog.Logger) (*Web, error) {
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
		store:         st,
		limiter:       limiter,
		trustProxy:    trustProxy,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		umami:         umami,
		hooks:         hooks,
		metrics:       registry,
		slip:          slip.New(st),
		logger:        logger,
		tmpl:          tmpl,
		assets:        http.FileServer(http.FS(sub)),
	}, nil
}

// Register wires the UI routes into mux.
func (w *Web) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", w.handleIndex)
	mux.HandleFunc("GET /stats", w.handleStats)
	mux.HandleFunc("GET /entries", w.handleLedger)
	mux.HandleFunc("GET /words", w.handleWords)
	mux.HandleFunc("GET /tags", w.handleTagsIndex)
	mux.HandleFunc("GET /tags/{slug}", w.handleTagPage)
	mux.HandleFunc("GET /y/{year}", w.handleYearReview)
	mux.HandleFunc("GET /p/{person}", w.handlePerson)
	mux.HandleFunc("GET /p/{person}/feed.xml", w.handlePersonFeed)
	mux.HandleFunc("GET /random", w.handleRandom)
	mux.HandleFunc("GET /photos", w.handlePhotos)
	mux.HandleFunc("GET /e/{id}", w.handlePermalink)
	mux.HandleFunc("GET /feed.xml", w.handleFeed)
	mux.HandleFunc("POST /entries", w.handleCreate)
	mux.HandleFunc("GET /robots.txt", w.handleRobots)
	mux.HandleFunc("GET /sitemap.xml", w.handleSitemap)
	mux.HandleFunc("GET /site.webmanifest", w.handleManifest)
	mux.HandleFunc("GET /opensearch.xml", w.handleOpenSearch)
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheable(w.assets)))
}

type pageData struct {
	Umami         config.Umami
	Stats         store.Stats
	SpanYears     int
	Ledger        ledgerData
	Recent        []store.Entry
	Tags          []store.Tag
	Tag           store.Tag
	Title         string
	Description   string
	CanonicalURL  string
	Robots        string
	ToolbarAction string
	Added         bool
	ShowAdd       bool
	ShowInfo      bool
	FormError     string
	FormWord      string
	FormMeaning   string
	FormPerson    string
	Year          int

	// Slip is the day's chosen entry; nil while a listing parameter is active
	// or when the ledger has nothing live.
	Slip *slip.Slip

	// Social preview fields. OGImage is the committed fallback on the home
	// page; the other heads either replace it (permalinks) or omit the block.
	OGImage    string
	OGImageAlt string
	OGType     string
}

type ledgerData struct {
	Entries   []ledgerRow
	Persons   []string
	Query     ledgerQuery
	Years     []int
	Total     int
	Pages     int
	From      int
	To        int
	PageLinks []pageLink
	// Path is the page's own base path when the ledger is scoped by something
	// other than a query string (the person page). Empty means /entries.
	Path string
	// LockedPerson is the person fixed by the path; the toolbar then renders a
	// static chip instead of a select that could silently widen the scope.
	LockedPerson string
}

// ledgerRow is one rendered entry. The plain store fields stay untouched; the
// HTML variants carry the source text escaped and only our own <mark> tags.
type ledgerRow struct {
	store.Entry
	WordHTML    template.HTML
	MeaningHTML template.HTML
	Tags        []store.Tag
}

type ledgerQuery struct {
	Q         string
	Sort      string
	Order     string
	Person    string
	Page      int
	PerPage   int
	PhotoOnly bool
	Year      int
	From      string
	To        string
	Tags      []string
	Highlight bool
}

type pageLink struct {
	Num    int
	URL    string
	Active bool
	Gap    bool
}

// statsPageData is the view model for the /stats page. Ratios and bar heights
// are computed here so the template only has to place numbers.
type statsPageData struct {
	Umami     config.Umami
	Total     int
	Unique    int
	Last30d   int
	WithPhoto int
	PhotoPct  int
	First     *time.Time
	Last      *time.Time
	SpanYears int
	SpanText  string
	PerYear   int
	Years     []yearBar
	Persons   []personBar
	Empty     bool
	Year      int

	CanonicalURL  string
	Robots        string
	OGTitle       string
	OGDescription string
	OGImage       string
	OGImageAlt    string
	JSONLD        template.JS

	// Ledger and the dialog fields let the shared masthead's add/info triggers
	// resolve to dialogs rendered on this page.
	Ledger      ledgerData
	ShowAdd     bool
	ShowInfo    bool
	FormError   string
	FormWord    string
	FormMeaning string
	FormPerson  string
}

type yearBar struct {
	Year    int
	Count   int
	Height  int
	Current bool
	Href    string
}

type personBar struct {
	Person string
	Count  int
	Width  int
}

type permalinkData struct {
	Umami        config.Umami
	Entry        store.Entry
	Tags         []store.Tag
	CanonicalURL string
	OGImage      string
	OGImageAlt   string
	OGWidth      int
	OGHeight     int
	ShareTitle   string
	ShareText    string
	IsSlip       bool
	JSONLD       template.JS
	Year         int
}

type errorData struct {
	Umami   config.Umami
	Title   string
	Message string
	Year    int
}

func (q ledgerQuery) values() url.Values {
	v := url.Values{}
	if q.Q != "" {
		v.Set("q", q.Q)
	}
	if q.Person != "" {
		v.Set("person", q.Person)
	}
	v.Set("sort", q.Sort)
	v.Set("order", q.Order)
	v.Set("per_page", strconv.Itoa(q.PerPage))
	if q.PhotoOnly {
		v.Set("photo", "1")
	}
	if q.Year > 0 {
		v.Set("year", strconv.Itoa(q.Year))
	}
	if q.From != "" {
		v.Set("from", q.From)
	}
	if q.To != "" {
		v.Set("to", q.To)
	}
	for _, tag := range q.Tags {
		v.Add("tag", tag)
	}
	if !q.Highlight {
		v.Set("highlight", "0")
	}
	return v
}

// BasePath is the path the ledger's own controls target. The shared /entries
// view leaves Path empty; scoped pages such as the person page set it so sort,
// pager and clear-search links keep the scope.
func (d ledgerData) BasePath() string {
	if d.Path != "" {
		return d.Path
	}
	return "/entries"
}

// queryValues is the query state for the ledger's own links. A person fixed by
// the path is not repeated in the query string: the path already carries it.
func (d ledgerData) queryValues() url.Values {
	v := d.Query.values()
	if d.LockedPerson != "" {
		v.Del("person")
	}
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
	v := d.queryValues()
	v.Set("sort", column)
	v.Set("order", order)
	return d.BasePath() + "?" + v.Encode()
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
	v := d.queryValues()
	v.Set("page", strconv.Itoa(n))
	return d.BasePath() + "?" + v.Encode()
}

func (w *Web) handleIndex(rw http.ResponseWriter, r *http.Request) {
	data, err := w.buildPage(r)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	w.render(rw, http.StatusOK, "base", data)
}

// handleStats renders the full statistics page. It reads the ledger summary
// once and renders everything server-side.
func (w *Web) handleStats(rw http.ResponseWriter, r *http.Request) {
	stats, err := w.store.Stats(r.Context())
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	data := buildStatsPage(stats, w.umami(), time.Now())
	data.CanonicalURL = w.absoluteURL(r, "/stats")
	data.Robots = "index,follow"
	data.OGTitle = "The ledger in numbers · vanDyke"
	data.OGDescription = "Statistics for the vanDyke misspelling ledger: slips per year, unique misspellings, photographs and the names that turn up most."
	data.OGImage = w.absoluteURL(r, ogDefaultPath)
	data.OGImageAlt = "vanDyke — the LANCo misspelling ledger"
	data.JSONLD = statsJSONLD(stats, data.CanonicalURL, w.absoluteURL(r, "/api/v1/stats"))
	w.render(rw, http.StatusOK, "stats", data)
}

func (w *Web) handleLedger(rw http.ResponseWriter, r *http.Request) {
	ledger, err := w.buildLedger(r)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	recent, err := w.recentEntries(r)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	data := pageData{
		Umami:         w.umami(),
		Ledger:        ledger,
		Recent:        recent,
		Year:          time.Now().Year(),
		ToolbarAction: "/entries",
		Title:         "The ledger · vanDyke",
		Description:   "Browse, search and sort every misspelling in the LANCo ledger, one correction at a time.",
		// The canonical tag pages are the indexable tag documents; the filter
		// view, including ?tag=, must not compete with them.
		Robots: "noindex,follow",
	}
	// HTMX swaps the bare ledger fragment; a plain navigation or a crawler gets
	// a complete document so sort, pager and clear-search links keep working.
	if r.Header.Get("HX-Request") != "" {
		w.render(rw, http.StatusOK, "ledger", data)
		return
	}
	w.render(rw, http.StatusOK, "ledger-page", data)
}

func (w *Web) handlePermalink(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		w.renderNotFound(rw)
		return
	}
	entry, err := w.store.GetEntry(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		w.renderNotFound(rw)
		return
	}
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	canonical := w.absoluteURL(r, fmt.Sprintf("/e/%d", entry.ID))
	image := w.absoluteURL(r, ogDefaultPath)
	width, height := ogDefaultWidth, ogDefaultHeight
	alt := "vanDyke — the LANCo misspelling ledger"
	if entry.PhotoURL != "" {
		image = w.absoluteURL(r, entry.PhotoURL+"?size=display")
		width, height = 0, 0
		alt = fmt.Sprintf("Photograph attached to entry #%04d", entry.ID)
	}
	tags, err := w.store.EntryTags(r.Context(), entry.ID)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	isSlip := false
	if today, err := w.slip.Today(r.Context()); err == nil {
		isSlip = today.Entry.ID == entry.ID
	} else if !errors.Is(err, slip.ErrNoSlip) {
		w.logger.Error("resolve daily slip", "error", err)
	}

	shareTitle := shareTitleFor(entry)
	w.render(rw, http.StatusOK, "permalink", permalinkData{
		Umami:        w.umami(),
		Entry:        entry,
		Tags:         tags,
		CanonicalURL: canonical,
		OGImage:      image,
		OGImageAlt:   alt,
		OGWidth:      width,
		OGHeight:     height,
		ShareTitle:   shareTitle,
		ShareText:    fmt.Sprintf("Entry #%04d in the LANCo misspelling ledger.", entry.ID),
		IsSlip:       isSlip,
		JSONLD:       entryJSONLD(entry, canonical, w.absoluteURL(r, "/")),
		Year:         time.Now().Year(),
	})
}

func (w *Web) renderNotFound(rw http.ResponseWriter) {
	w.renderErrorPage(rw,
		"Entry not found",
		"That slip is not in the ledger. It may have been removed, or the link may be wrong.")
}

// renderErrorPage renders the shared not-found document with a page-specific
// title and message. It never carries an ETag.
func (w *Web) renderErrorPage(rw http.ResponseWriter, title, message string) {
	w.renderStatusErrorPage(rw, http.StatusNotFound, title, message)
}

// renderStatusErrorPage is renderErrorPage with an explicit status for the few
// 400s the browse surfaces answer (a malformed gallery cursor).
func (w *Web) renderStatusErrorPage(rw http.ResponseWriter, status int, title, message string) {
	w.render(rw, status, "not-found", errorData{
		Umami:   w.umami(),
		Title:   title,
		Message: message,
		Year:    time.Now().Year(),
	})
}

// handleRobots serves crawler directives and points at the absolute sitemap.
func (w *Web) handleRobots(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = fmt.Fprintf(rw, "User-agent: *\nDisallow: /admin\n\nSitemap: %s\n", w.absoluteURL(r, "/sitemap.xml"))
}

type sitemapURL struct {
	Loc string `xml:"loc"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// handleSitemap lists the home page, statistics, the word index, the photo
// gallery, the tag index, every tag and year with live entries, and every live
// permalink as absolute URLs built by the same helper as canonical links.
// Person pages and the random redirect are deliberately absent.
func (w *Web) handleSitemap(rw http.ResponseWriter, r *http.Request) {
	ids, err := w.store.AllEntryIDs(r.Context())
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	tags, err := w.store.ListTags(r.Context())
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	years, err := w.store.EntryYears(r.Context())
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	set := sitemapSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs: []sitemapURL{
			{Loc: w.absoluteURL(r, "/")},
			{Loc: w.absoluteURL(r, "/stats")},
			{Loc: w.absoluteURL(r, "/words")},
			{Loc: w.absoluteURL(r, "/photos")},
			{Loc: w.absoluteURL(r, "/tags")},
		},
	}
	// A tag page with no live entry is not advertised: it would be a crawlable
	// duplicate of the empty ledger.
	for _, tag := range tags {
		if tag.LiveCount == 0 {
			continue
		}
		set.URLs = append(set.URLs, sitemapURL{Loc: w.absoluteURL(r, "/tags/"+tag.Slug)})
	}
	// EntryYears only returns years with at least one live entry.
	for _, year := range years {
		set.URLs = append(set.URLs, sitemapURL{Loc: w.absoluteURL(r, fmt.Sprintf("/y/%d", year))})
	}
	for _, id := range ids {
		set.URLs = append(set.URLs, sitemapURL{Loc: w.absoluteURL(r, fmt.Sprintf("/e/%d", id))})
	}
	body, err := xml.Marshal(set)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	rw.Header().Set("Content-Type", "application/xml; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	_, _ = rw.Write([]byte(xml.Header + string(body) + "\n"))
}

type manifestIcon struct {
	Src   string `json:"src"`
	Sizes string `json:"sizes"`
	Type  string `json:"type"`
}

type webManifest struct {
	Name            string         `json:"name"`
	ShortName       string         `json:"short_name"`
	StartURL        string         `json:"start_url"`
	Display         string         `json:"display"`
	ThemeColor      string         `json:"theme_color"`
	BackgroundColor string         `json:"background_color"`
	Icons           []manifestIcon `json:"icons"`
}

// handleManifest serves the web app manifest using the existing icon assets.
func (w *Web) handleManifest(rw http.ResponseWriter, r *http.Request) {
	manifest := webManifest{
		Name:            "vanDyke — the LANCo misspelling ledger",
		ShortName:       "vanDyke",
		StartURL:        "/",
		Display:         "standalone",
		ThemeColor:      "#f4eee1",
		BackgroundColor: "#f4eee1",
		Icons: []manifestIcon{
			{Src: "/static/images/favicon.svg", Sizes: "any", Type: "image/svg+xml"},
			{Src: "/static/images/apple-touch-icon.png", Sizes: "180x180", Type: "image/png"},
		},
	}
	rw.Header().Set("Content-Type", "application/manifest+json")
	rw.Header().Set("Cache-Control", "public, max-age=3600")
	_ = json.NewEncoder(rw).Encode(manifest)
}

func (w *Web) absoluteURL(r *http.Request, path string) string {
	return httpx.BaseURL(r, w.publicBaseURL, w.trustProxy) + path
}

func (w *Web) handleCreate(rw http.ResponseWriter, r *http.Request) {
	isHTMX := r.Header.Get("HX-Request") != ""
	ip := httpx.ClientIP(r, w.trustProxy)

	if !w.limiter.Allow(ip) {
		w.metrics.IncRateLimited()
		w.metrics.IncEntryAddition("rate_limited")
		rw.Header().Set("Retry-After", strconv.Itoa(w.limiter.RetryAfterSeconds()))
		const message = "Too many entries added from this connection. Try again later."
		if isHTMX {
			w.renderFormError(rw, message)
		} else {
			w.renderIndexError(rw, r, http.StatusTooManyRequests, message, "", "", "")
		}
		return
	}

	r.Body = http.MaxBytesReader(rw, r.Body, store.MaxPhotoBytes+(1<<20))
	parseErr := r.ParseForm()
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		parseErr = r.ParseMultipartForm(64 << 10)
	}
	if parseErr != nil {
		w.metrics.IncEntryAddition("invalid")
		message := fmt.Sprintf("The upload is too large or could not be read. Photos can be up to %d MB.", store.MaxPhotoBytes/(1<<20))
		if isHTMX {
			w.renderFormError(rw, message)
		} else {
			w.renderIndexError(rw, r, http.StatusRequestEntityTooLarge, message, "", "", "")
		}
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}

	word := strings.TrimSpace(r.PostFormValue("word"))
	meaning := strings.TrimSpace(r.PostFormValue("meaning"))
	person := strings.TrimSpace(r.PostFormValue("person"))

	var photo *store.Photo
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		var photoErr error
		photo, photoErr = upload.Photo(r, "photo")
		if photoErr != nil {
			w.metrics.IncEntryAddition("invalid")
			message := capitalize(photoErr.Error())
			if isHTMX {
				w.renderFormError(rw, message)
			} else {
				w.renderIndexError(rw, r, http.StatusUnprocessableEntity, message, word, meaning, person)
			}
			return
		}
	}

	if err := store.ValidateEntry(word, meaning, photo != nil); err != nil {
		w.metrics.IncEntryAddition("invalid")
		message := capitalize(err.Error())
		if isHTMX {
			w.renderFormError(rw, message)
		} else {
			w.renderIndexError(rw, r, http.StatusUnprocessableEntity, message, word, meaning, person)
		}
		return
	}
	if err := store.ValidatePerson(person); err != nil {
		w.metrics.IncEntryAddition("invalid")
		message := capitalize(err.Error())
		if isHTMX {
			w.renderFormError(rw, message)
		} else {
			w.renderIndexError(rw, r, http.StatusUnprocessableEntity, message, word, meaning, person)
		}
		return
	}

	entry, err := w.store.CreateEntry(r.Context(), word, meaning, person, photo)
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			w.metrics.IncEntryAddition("duplicate")
			const message = "That entry is already in the ledger."
			if isHTMX {
				w.renderFormError(rw, message)
			} else {
				w.renderIndexError(rw, r, http.StatusConflict, message, word, meaning, person)
			}
			return
		}
		w.metrics.IncEntryAddition("error")
		w.logger.Error("create entry", "error", err)
		http.Error(rw, "could not add entry", http.StatusInternalServerError)
		return
	}
	w.metrics.IncEntryAddition("created")

	// Fire-and-forget: emission returns immediately, so a slow receiver cannot
	// delay or change this response.
	w.hooks.Emit(webhook.Change{
		Event:   webhook.EventEntryCreated,
		Entry:   entry,
		BaseURL: httpx.BaseURL(r, w.publicBaseURL, w.trustProxy),
	})

	if !isHTMX {
		http.Redirect(rw, r, "/?added=1", http.StatusSeeOther)
		return
	}

	trigger, _ := json.Marshal(map[string]any{
		"entryAdded": map[string]any{"id": entry.ID, "word": entry.Word, "photo": entry.PhotoURL != ""},
	})
	rw.Header().Set("HX-Trigger-After-Swap", string(trigger))

	ledger, err := w.buildLedger(r)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	recent, err := w.recentEntries(r)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	w.render(rw, http.StatusOK, "ledger", pageData{Umami: w.umami(), Ledger: ledger, Recent: recent})
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
	recent, err := w.recentEntries(r)
	if err != nil {
		return pageData{}, err
	}
	data := pageData{
		Umami:       w.umami(),
		Stats:       stats,
		SpanYears:   span,
		Ledger:      ledger,
		Recent:      recent,
		Added:       query.Get("added") == "1",
		ShowAdd:     query.Get("add") == "1",
		ShowInfo:    query.Get("info") == "1",
		Year:        time.Now().Year(),
		Title:       "vanDyke — the LANCo misspelling ledger",
		Description: "A ledger of spelling errors, misheard words and creative typos. Born as an internal joke of the LANCo committee.",
		OGType:      "website",
		OGImage:     w.absoluteURL(r, ogDefaultPath),
		OGImageAlt:  "vanDyke — the LANCo misspelling ledger",
	}
	if hasListingParams(r) {
		// A filtered or paginated URL must not compete with the canonical
		// home page, and a canonical pointing elsewhere alongside noindex
		// would send conflicting signals, so it carries none.
		data.Robots = "noindex,follow"
		return data, nil
	}
	data.Robots = "index,follow"
	data.CanonicalURL = w.absoluteURL(r, "/")
	if today, err := w.slip.Today(r.Context()); err == nil {
		data.Slip = &today
	} else if !errors.Is(err, slip.ErrNoSlip) {
		w.logger.Error("resolve daily slip", "error", err)
	}
	return data, nil
}

// hasListingParams reports whether the request narrows or pages the ledger.
// The slip section and the self-canonical appear only when none is present.
func hasListingParams(r *http.Request) bool {
	query := r.URL.Query()
	for _, key := range []string{"q", "person", "year", "from", "to", "photo", "page", "per_page", "sort", "order"} {
		if _, ok := query[key]; ok {
			return true
		}
	}
	return false
}

// recentEntries returns the newest entries for the recent feed.
func (w *Web) recentEntries(r *http.Request) ([]store.Entry, error) {
	page, err := w.store.ListEntries(r.Context(), store.ListParams{
		Sort:    "created_at",
		Order:   "desc",
		PerPage: 5,
	})
	if err != nil {
		return nil, err
	}
	return page.Entries, nil
}

func (w *Web) buildLedger(r *http.Request) (ledgerData, error) {
	return w.buildLedgerWith(r, "", "", "")
}

// buildLedgerWithTag renders the ledger with an extra tag slug appended to the
// request parameters. The /tags/{slug} page uses it so the path, not a query
// string, carries the filter.
func (w *Web) buildLedgerWithTag(r *http.Request, tag string) (ledgerData, error) {
	return w.buildLedgerWith(r, tag, "", "")
}

// buildLedgerWith renders the ledger with an extra tag slug and/or a person
// fixed from the request path. path is the page's own base path; the toolbar,
// sort headers, pager and clear-search link use it so a scoped page keeps its
// scope. A person set here wins over any person query parameter.
func (w *Web) buildLedgerWith(r *http.Request, tag, person, path string) (ledgerData, error) {
	params := parseListParams(r)
	if tag != "" {
		params.Tags = append(params.Tags, tag)
	}
	if person != "" {
		params.Person = person
	}
	params = store.NormalizeListParams(params)
	page, err := w.store.ListEntries(r.Context(), params)
	if err != nil {
		return ledgerData{}, err
	}
	years, err := w.store.EntryYears(r.Context())
	if err != nil {
		return ledgerData{}, err
	}
	persons, err := w.store.Persons(r.Context())
	if err != nil {
		return ledgerData{}, err
	}
	ids := make([]int64, 0, len(page.Entries))
	for _, entry := range page.Entries {
		ids = append(ids, entry.ID)
	}
	tagsByEntry, err := w.store.TagsByEntry(r.Context(), ids)
	if err != nil {
		return ledgerData{}, err
	}
	query := ledgerQuery{
		Q:         params.Query,
		Sort:      params.Sort,
		Order:     params.Order,
		Person:    params.Person,
		Page:      page.Page,
		PerPage:   page.PerPage,
		PhotoOnly: params.HasPhoto != nil && *params.HasPhoto,
		Year:      params.Year,
		Tags:      params.Tags,
		Highlight: highlightEnabled(r),
	}
	if !params.From.IsZero() {
		query.From = params.From.Format("2006-01-02")
	}
	if !params.To.IsZero() {
		query.To = params.To.Format("2006-01-02")
	}

	parsed := searchquery.Parse(params.Query)
	rows := make([]ledgerRow, 0, len(page.Entries))
	for _, entry := range page.Entries {
		row := ledgerRow{Entry: entry, Tags: tagsByEntry[entry.ID]}
		if query.Highlight {
			row.WordHTML = markMatches(entry.Word, parsed.Include)
			row.MeaningHTML = markMatches(entry.Meaning, parsed.Include)
		}
		rows = append(rows, row)
	}

	d := ledgerData{
		Entries: rows,
		Persons: persons,
		Query:   query,
		Years:   years,
		Total:   page.Total,
		Pages:   page.Pages,
		Path:    path,
	}
	if person != "" {
		d.LockedPerson = params.Person
	}
	if page.Total > 0 && len(page.Entries) > 0 {
		d.From = (page.Page-1)*page.PerPage + 1
		d.To = d.From + len(page.Entries) - 1
	}
	d.PageLinks = buildPageLinks(page.Page, page.Pages, d.PageHref)
	return d, nil
}

// markMatches escapes text and wraps each case-insensitive occurrence of an
// included phrase in a <mark> element. Every byte of user text is escaped
// before it reaches the output, so only our own tags are markup.
func markMatches(text string, phrases []searchquery.Phrase) template.HTML {
	if text == "" || len(phrases) == 0 {
		return template.HTML(html.EscapeString(text))
	}

	type span struct{ start, end int }
	src := []rune(text)
	var spans []span
	for _, phrase := range phrases {
		needle := []rune(phrase.Text)
		if len(needle) == 0 {
			continue
		}
		for i := 0; i+len(needle) <= len(src); {
			if runesEqualFold(src[i:i+len(needle)], needle) {
				spans = append(spans, span{i, i + len(needle)})
				i += len(needle)
				continue
			}
			i++
		}
	}
	if len(spans) == 0 {
		return template.HTML(html.EscapeString(text))
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var b strings.Builder
	last := 0
	for _, s := range spans {
		if s.start < last {
			if s.end > last {
				last = s.end
			}
			continue
		}
		b.WriteString(html.EscapeString(string(src[last:s.start])))
		b.WriteString("<mark>")
		b.WriteString(html.EscapeString(string(src[s.start:s.end])))
		b.WriteString("</mark>")
		last = s.end
	}
	b.WriteString(html.EscapeString(string(src[last:])))
	return template.HTML(b.String())
}

// runesEqualFold compares two equal-length rune slices case-insensitively.
func runesEqualFold(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if unicode.ToLower(a[i]) != unicode.ToLower(b[i]) {
			return false
		}
	}
	return true
}

// highlightEnabled reports whether the ledger should mark matches. Highlighting
// is on unless the request explicitly turns it off with highlight=0.
func highlightEnabled(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue("highlight"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

func parseListParams(r *http.Request) store.ListParams {
	_ = r.ParseForm()
	return store.ListParams{
		Query:    r.FormValue("q"),
		Sort:     r.FormValue("sort"),
		Order:    r.FormValue("order"),
		Person:   r.FormValue("person"),
		Page:     formInt(r, "page", 1),
		PerPage:  formInt(r, "per_page", store.DefaultPerPage),
		HasPhoto: formOptionalBool(r, "photo", "filter_photo"),
		Year:     formInt(r, "year", 0),
		From:     formDate(r, "from"),
		To:       formDate(r, "to"),
		Tags:     r.Form["tag"],
	}
}

// formDate parses an optional YYYY-MM-DD filter. The web page ignores a
// malformed value, leaving that bound unapplied.
func formDate(r *http.Request, key string) time.Time {
	raw := strings.TrimSpace(r.FormValue(key))
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func formOptionalBool(r *http.Request, keys ...string) *bool {
	for _, key := range keys {
		switch strings.ToLower(strings.TrimSpace(r.FormValue(key))) {
		case "1", "true", "yes":
			value := true
			return &value
		case "0", "false", "no":
			value := false
			return &value
		}
	}
	return nil
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

// buildStatsPage turns a store.Stats snapshot into template-ready figures.
// Nil slices in Stats (an empty ledger) yield no bars.
func buildStatsPage(stats store.Stats, umami config.Umami, now time.Time) statsPageData {
	d := statsPageData{
		Umami:     umami,
		Total:     stats.Total,
		Unique:    stats.UniqueWords,
		Last30d:   stats.EntriesLast30d,
		WithPhoto: stats.WithPhoto,
		First:     stats.FirstEntryAt,
		Last:      stats.LastEntryAt,
		Empty:     stats.Total == 0,
		Year:      now.Year(),
		Ledger:    ledgerData{Query: ledgerQuery{}},
	}
	if d.Total > 0 {
		d.PhotoPct = (d.WithPhoto*100 + d.Total/2) / d.Total
	}
	if stats.FirstEntryAt != nil && stats.LastEntryAt != nil {
		d.SpanYears = stats.LastEntryAt.Year() - stats.FirstEntryAt.Year() + 1
		if d.SpanYears < 1 {
			d.SpanYears = 1
		}
		d.SpanText = humanSpan(*stats.FirstEntryAt, *stats.LastEntryAt)
		d.PerYear = (d.Total + d.SpanYears/2) / d.SpanYears
	}
	d.Years = buildYearBars(stats.ByYear, now.Year())
	d.Persons = buildPersonBars(stats.TopPersons)
	return d
}

// buildYearBars fills the gaps between the first and last year so the chart
// reads as a timeline instead of a list. A single value scales the bars.
func buildYearBars(counts []store.YearCount, current int) []yearBar {
	if len(counts) == 0 {
		return nil
	}
	byYear := make(map[int]int, len(counts))
	firstYear, lastYear := 0, 0
	for _, c := range counts {
		if c.Year < 1000 || c.Year > 9999 {
			continue
		}
		byYear[c.Year] += c.Count
		if firstYear == 0 || c.Year < firstYear {
			firstYear = c.Year
		}
		if c.Year > lastYear {
			lastYear = c.Year
		}
	}
	if firstYear == 0 {
		return nil
	}

	years := make([]int, 0, lastYear-firstYear+1)
	// Never draw more than a readable wall of bars; fall back to only the
	// years that actually hold entries.
	if lastYear-firstYear <= 40 {
		for y := firstYear; y <= lastYear; y++ {
			years = append(years, y)
		}
	} else {
		for y := range byYear {
			years = append(years, y)
		}
		sort.Ints(years)
	}

	peak := 0
	for _, y := range years {
		if n := byYear[y]; n > peak {
			peak = n
		}
	}

	bars := make([]yearBar, 0, len(years))
	for _, y := range years {
		count := byYear[y]
		height := 0
		if peak > 0 && count > 0 {
			height = (count*100 + peak/2) / peak
			if height < 5 {
				height = 5
			}
		}
		bars = append(bars, yearBar{Year: y, Count: count, Height: height, Current: y == current, Href: yearHref(y, count)})
	}
	return bars
}

// yearHref links a year with live entries to its recap. BuildYearBars emits
// zero-count gap bars between the first and last year; those must stay
// unlinked because /y/{yyyy} answers 404 for them.
func yearHref(year, count int) string {
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf("/y/%d", year)
}

// buildPersonBars scales each name against the most frequent one.
func buildPersonBars(persons []store.PersonCount) []personBar {
	peak := 0
	for _, p := range persons {
		if p.Count > peak {
			peak = p.Count
		}
	}
	bars := make([]personBar, 0, len(persons))
	for _, p := range persons {
		if strings.TrimSpace(p.Person) == "" {
			continue
		}
		width := 0
		if peak > 0 && p.Count > 0 {
			width = (p.Count*100 + peak/2) / peak
			if width < 4 {
				width = 4
			}
		}
		bars = append(bars, personBar{Person: p.Person, Count: p.Count, Width: width})
	}
	return bars
}

// humanSpan describes the distance between two dates in plain words.
func humanSpan(first, last time.Time) string {
	if last.Before(first) {
		first, last = last, first
	}
	months := (last.Year()-first.Year())*12 + int(last.Month()) - int(first.Month())
	if last.Day() < first.Day() {
		months--
	}
	if months < 1 {
		days := int(last.Sub(first).Hours() / 24)
		switch {
		case days <= 0:
			return "a single day"
		case days == 1:
			return "1 day"
		case days < 14:
			return fmt.Sprintf("%d days", days)
		case days < 60:
			return fmt.Sprintf("%d weeks", days/7)
		default:
			return "about a month"
		}
	}
	years, rest := months/12, months%12
	switch {
	case years == 0:
		if months == 1 {
			return "1 month"
		}
		return fmt.Sprintf("%d months", months)
	case years == 1 && rest == 0:
		return "1 year"
	case rest == 0:
		return fmt.Sprintf("%d years", years)
	case years == 1 && rest == 1:
		return "1 year and 1 month"
	case years == 1:
		return fmt.Sprintf("1 year and %d months", rest)
	case rest == 1:
		return fmt.Sprintf("%d years and 1 month", years)
	default:
		return fmt.Sprintf("%d years and %d months", years, rest)
	}
}

func (w *Web) renderFormError(rw http.ResponseWriter, message string) {
	rw.Header().Set("HX-Retarget", "#form-error")
	rw.Header().Set("HX-Reswap", "innerHTML")
	w.render(rw, http.StatusOK, "form-error", message)
}

func (w *Web) renderIndexError(rw http.ResponseWriter, r *http.Request, status int, message, word, meaning, person string) {
	page, err := w.buildPage(r)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	page.ShowAdd = true
	page.FormError = message
	page.FormWord = word
	page.FormMeaning = meaning
	page.FormPerson = person
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

const (
	ogDefaultPath   = "/static/images/og-default.png"
	ogDefaultWidth  = 1200
	ogDefaultHeight = 630
)

// shareTitleFor names an entry in the same words as its <title> and Open Graph
// title, so a shared card and the page agree.
func shareTitleFor(entry store.Entry) string {
	if entry.Word == "" {
		return "Photo entry"
	}
	if entry.Meaning == "" {
		return entry.Word
	}
	return entry.Word + " → " + entry.Meaning
}

// definedTermSet names the ledger a DefinedTerm belongs to.
type definedTermSet struct {
	Type string `json:"@type"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// definedTerm is the schema.org document for one entry: the misspelling is the
// term and the correction is its definition.
type definedTerm struct {
	Context          string         `json:"@context"`
	Type             string         `json:"@type"`
	Name             string         `json:"name"`
	Description      string         `json:"description,omitempty"`
	URL              string         `json:"url"`
	Identifier       string         `json:"identifier"`
	DateCreated      string         `json:"dateCreated"`
	Image            string         `json:"image,omitempty"`
	InDefinedTermSet definedTermSet `json:"inDefinedTermSet"`
}

// entryJSONLD encodes the permalink's structured data. encoding/json escapes
// <, > and & by default, so a </script> sequence in entry text cannot break out
// of the script element.
func entryJSONLD(entry store.Entry, canonical, base string) template.JS {
	name := entry.Word
	if name == "" {
		name = fmt.Sprintf("Photo entry #%04d", entry.ID)
	}
	doc := definedTerm{
		Context:     "https://schema.org",
		Type:        "DefinedTerm",
		Name:        name,
		Description: entry.Meaning,
		URL:         canonical,
		Identifier:  strconv.FormatInt(entry.ID, 10),
		DateCreated: entry.CreatedAt.UTC().Format(time.RFC3339),
		InDefinedTermSet: definedTermSet{
			Type: "DefinedTermSet",
			Name: "The vanDyke Corrections Ledger",
			URL:  base,
		},
	}
	if entry.PhotoURL != "" {
		doc.Image = base + strings.TrimPrefix(entry.PhotoURL, "/") + "?size=display"
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return ""
	}
	return template.JS(body)
}

type propertyValue struct {
	Type  string `json:"@type"`
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type datasetCreator struct {
	Type string `json:"@type"`
	Name string `json:"name"`
}

type dataDownload struct {
	Type           string `json:"@type"`
	ContentURL     string `json:"contentUrl"`
	EncodingFormat string `json:"encodingFormat"`
}

// datasetDocument is the schema.org Dataset for /stats.
type datasetDocument struct {
	Context             string          `json:"@context"`
	Type                string          `json:"@type"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	URL                 string          `json:"url"`
	IsAccessibleForFree bool            `json:"isAccessibleForFree"`
	TemporalCoverage    string          `json:"temporalCoverage,omitempty"`
	VariableMeasured    []propertyValue `json:"variableMeasured"`
	Creator             datasetCreator  `json:"creator"`
	Distribution        dataDownload    `json:"distribution"`
}

// statsJSONLD encodes the Dataset for the statistics page. An empty ledger has
// no temporal coverage, so the field is omitted rather than invented.
func statsJSONLD(stats store.Stats, canonical, apiURL string) template.JS {
	doc := datasetDocument{
		Context:             "https://schema.org",
		Type:                "Dataset",
		Name:                "The vanDyke ledger in numbers",
		Description:         "Statistics for the vanDyke misspelling ledger: slips per year, unique misspellings, photographs and the names that turn up most.",
		URL:                 canonical,
		IsAccessibleForFree: true,
		VariableMeasured: []propertyValue{
			{Type: "PropertyValue", Name: "Entries", Value: stats.Total},
			{Type: "PropertyValue", Name: "Unique misspellings", Value: stats.UniqueWords},
			{Type: "PropertyValue", Name: "Entries with a photograph", Value: stats.WithPhoto},
			{Type: "PropertyValue", Name: "Entries in the last 30 days", Value: stats.EntriesLast30d},
		},
		Creator: datasetCreator{Type: "Organization", Name: "LANCo committee"},
		Distribution: dataDownload{
			Type:           "DataDownload",
			ContentURL:     apiURL,
			EncodingFormat: "application/json",
		},
	}
	if stats.FirstEntryAt != nil && stats.LastEntryAt != nil {
		doc.TemporalCoverage = stats.FirstEntryAt.Format("2006-01-02") + "/" + stats.LastEntryAt.Format("2006-01-02")
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return ""
	}
	return template.JS(body)
}

type openSearchImage struct {
	Width  int    `xml:"width,attr"`
	Height int    `xml:"height,attr"`
	Type   string `xml:"type,attr"`
	Value  string `xml:",chardata"`
}

type openSearchURL struct {
	Type     string `xml:"type,attr"`
	Template string `xml:"template,attr"`
}

type openSearchDescription struct {
	XMLName       xml.Name        `xml:"OpenSearchDescription"`
	Xmlns         string          `xml:"xmlns,attr"`
	ShortName     string          `xml:"ShortName"`
	Description   string          `xml:"Description"`
	InputEncoding string          `xml:"InputEncoding"`
	Image         openSearchImage `xml:"Image"`
	URL           openSearchURL   `xml:"Url"`
}

// handleOpenSearch serves the OpenSearch 1.1 description document. The
// template submits to the same /?q= surface the toolbar uses, so the existing
// search grammar (quoted phrases, '-' exclusions) applies unchanged. No
// suggestions URL is advertised.
func (w *Web) handleOpenSearch(rw http.ResponseWriter, r *http.Request) {
	doc := openSearchDescription{
		Xmlns:         "http://a9.com/-/spec/opensearch/1.1/",
		ShortName:     "vanDyke",
		Description:   "Search the vanDyke misspelling ledger",
		InputEncoding: "UTF-8",
		Image: openSearchImage{
			Width:  16,
			Height: 16,
			Type:   "image/svg+xml",
			Value:  w.absoluteURL(r, "/static/images/favicon.svg"),
		},
		URL: openSearchURL{
			Type:     "text/html",
			Template: w.absoluteURL(r, "/?q={searchTerms}"),
		},
	}
	body, err := xml.Marshal(doc)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	rw.Header().Set("Content-Type", "application/opensearchdescription+xml; charset=utf-8")
	rw.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = rw.Write([]byte(xml.Header + string(body) + "\n"))
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
