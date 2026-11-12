// Package httpapi exposes the JSON REST API of the vanDyke ledger.
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/metrics"
	"vandyke.thor.edu/vandyke/internal/notify"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/slip"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/upload"
	"vandyke.thor.edu/vandyke/internal/webhook"
)

const (
	maxBodyBytes       = 64 << 10
	maxUploadBodyBytes = store.MaxPhotoBytes + (1 << 20)
)

// API serves /api/v1 endpoints.
type API struct {
	store         *store.Store
	limiter       *ratelimit.Limiter
	trustProxy    bool
	publicBaseURL string
	hooks         *webhook.Engine
	notify        *notify.Notifier
	metrics       *metrics.Registry
	metricsToken  string
	slip          *slip.Resolver
	logger        *slog.Logger
}

// New creates the API. hooks is the process-wide delivery engine and registry
// the metrics registry; both may be shared with the web and admin servers.
func New(st *store.Store, limiter *ratelimit.Limiter, trustProxy bool, publicBaseURL string,
	hooks *webhook.Engine, notifier *notify.Notifier, registry *metrics.Registry, metricsToken string, logger *slog.Logger) *API {
	return &API{
		store:         st,
		limiter:       limiter,
		trustProxy:    trustProxy,
		publicBaseURL: publicBaseURL,
		hooks:         hooks,
		notify:        notifier,
		metrics:       registry,
		metricsToken:  metricsToken,
		slip:          slip.New(st),
		logger:        logger,
	}
}

// Register wires the API routes into mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", a.handleHealth)
	mux.HandleFunc("GET /metrics", a.handleMetrics)
	mux.HandleFunc("GET /readyz", a.handleReady)
	mux.HandleFunc("GET /api/v1/entries", a.handleListEntries)
	mux.HandleFunc("GET /api/v1/entries/{id}", a.handleGetEntry)
	mux.HandleFunc("POST /api/v1/entries", a.rateLimited(a.handleCreateEntry))
	mux.HandleFunc("GET /api/v1/entries/{id}/photo", a.handleGetEntryPhoto)
	mux.HandleFunc("GET /api/v1/random", a.handleRandom)
	mux.HandleFunc("GET /api/v1/slip", a.handleSlip)
	mux.HandleFunc("GET /api/v1/stats", a.handleStats)
	mux.HandleFunc("GET /api/v1/persons", a.handlePersons)

	mux.HandleFunc("GET /api/v1/push/config", a.handlePushConfig)
	mux.HandleFunc("GET /api/v1/events", a.handleEvents)
	mux.HandleFunc("POST /api/v1/push/devices", a.rateLimited(a.handleUpsertPushDevice))
	mux.HandleFunc("DELETE /api/v1/push/devices", a.handleDeletePushDevice)
	mux.HandleFunc("POST /api/v1/push/subscriptions", a.rateLimited(a.handleUpsertWebPushSubscription))
	mux.HandleFunc("DELETE /api/v1/push/subscriptions", a.handleDeleteWebPushSubscription)
}

func (a *API) handleHealth(rw http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(rw, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMetrics serves the hand-rolled Prometheus exposition. It is public
// unless METRICS_TOKEN is configured, in which case it requires a constant-time
// bearer match.
func (a *API) handleMetrics(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Cache-Control", "no-store")
	if a.metricsToken != "" {
		got := r.Header.Get("Authorization")
		want := "Bearer " + a.metricsToken
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	if a.metrics == nil {
		rw.WriteHeader(http.StatusOK)
		return
	}
	rw.Header().Set("Content-Type", metrics.ContentType)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.metrics.Write(ctx, rw); err != nil {
		a.logger.Warn("write metrics", "error", err)
	}
}

// handleReady checks SQLite and the applied migration set. It never appears in
// the public JSON API namespace and never needs a token, because probes must
// work without credentials.
func (a *API) handleReady(rw http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	state := a.store.CheckReady(ctx)
	checks := map[string]string{"database": "ok", "migrations": "ok"}
	status := http.StatusOK
	overall := "ready"
	if !state.Database {
		checks["database"] = "error"
	}
	if !state.Migrations {
		checks["migrations"] = "error"
	}
	if !state.Database || !state.Migrations {
		status = http.StatusServiceUnavailable
		overall = "unavailable"
	}
	rw.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(rw, status, map[string]any{"status": overall, "checks": checks})
}

func (a *API) handleListEntries(rw http.ResponseWriter, r *http.Request) {
	from, err := queryDate(r, "from")
	if err != nil {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_date", "from must be a date in YYYY-MM-DD form")
		return
	}
	to, err := queryDate(r, "to")
	if err != nil {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_date", "to must be a date in YYYY-MM-DD form")
		return
	}

	page, err := a.store.ListEntries(r.Context(), store.ListParams{
		Query:    r.URL.Query().Get("q"),
		Sort:     r.URL.Query().Get("sort"),
		Order:    r.URL.Query().Get("order"),
		Person:   r.URL.Query().Get("person"),
		Page:     queryInt(r, "page", 1),
		PerPage:  queryInt(r, "per_page", store.DefaultPerPage),
		HasPhoto: queryOptionalBool(r, "photo"),
		Year:     queryInt(r, "year", 0),
		From:     from,
		To:       to,
		Tags:     r.URL.Query()["tag"],
	})
	if err != nil {
		a.logger.Error("list entries", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not list entries")
		return
	}
	rw.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(rw, http.StatusOK, page)
}

func (a *API) handleGetEntry(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_id", "id must be a positive integer")
		return
	}
	entry, err := a.store.GetEntry(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(rw, http.StatusNotFound, "not_found", "entry does not exist")
	case err != nil:
		a.logger.Error("get entry", "error", err, "id", id)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not load entry")
	default:
		httpx.WriteJSON(rw, http.StatusOK, entry)
	}
}

func (a *API) handleCreateEntry(rw http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		a.createEntryMultipart(rw, r)
		return
	}

	r.Body = http.MaxBytesReader(rw, r.Body, maxBodyBytes)

	var input struct {
		Word    string `json:"word"`
		Meaning string `json:"meaning"`
		Person  string `json:"person"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&input); err != nil {
		a.metrics.IncEntryAddition("invalid")
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_json", "request body must be JSON: {\"word\": \"...\", \"meaning\": \"...\"} or multipart/form-data with an optional photo")
		return
	}

	input.Word = strings.TrimSpace(input.Word)
	input.Meaning = strings.TrimSpace(input.Meaning)
	input.Person = strings.TrimSpace(input.Person)
	if err := store.ValidateEntry(input.Word, input.Meaning, false); err != nil {
		a.metrics.IncEntryAddition("invalid")
		httpx.WriteError(rw, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	if err := store.ValidatePerson(input.Person); err != nil {
		a.metrics.IncEntryAddition("invalid")
		httpx.WriteError(rw, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	a.writeCreatedEntry(rw, r, input.Word, input.Meaning, input.Person, nil)
}

func (a *API) createEntryMultipart(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, maxUploadBodyBytes)
	if err := r.ParseMultipartForm(64 << 10); err != nil {
		a.metrics.IncEntryAddition("invalid")
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		httpx.WriteError(rw, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large or not valid multipart/form-data")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	word := strings.TrimSpace(r.PostFormValue("word"))
	meaning := strings.TrimSpace(r.PostFormValue("meaning"))
	person := strings.TrimSpace(r.PostFormValue("person"))

	photo, err := upload.Photo(r, "photo")
	if err != nil {
		a.metrics.IncEntryAddition("invalid")
		httpx.WriteError(rw, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	if err := store.ValidateEntry(word, meaning, photo != nil); err != nil {
		a.metrics.IncEntryAddition("invalid")
		httpx.WriteError(rw, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	if err := store.ValidatePerson(person); err != nil {
		a.metrics.IncEntryAddition("invalid")
		httpx.WriteError(rw, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	a.writeCreatedEntry(rw, r, word, meaning, person, photo)
}

func (a *API) writeCreatedEntry(rw http.ResponseWriter, r *http.Request, word, meaning, person string, photo *store.Photo) {
	entry, err := a.store.CreateEntry(r.Context(), word, meaning, person, photo)
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			a.metrics.IncEntryAddition("duplicate")
			httpx.WriteError(rw, http.StatusConflict, "duplicate_entry", "that entry is already in the ledger")
			return
		}
		a.metrics.IncEntryAddition("error")
		a.logger.Error("create entry", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not create entry")
		return
	}
	a.metrics.IncEntryAddition("created")
	rw.Header().Set("Location", fmt.Sprintf("/api/v1/entries/%d", entry.ID))
	// The public API carries no admin identity, so the audit row has a null
	// actor and records the client IP instead.
	if err := a.store.RecordAudit(r.Context(), store.AuditRecord{
		Action:    "entry.create",
		Subject:   strconv.FormatInt(entry.ID, 10),
		Detail:    "word=" + entry.Word,
		IP:        httpx.ClientIP(r, a.trustProxy),
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		a.logger.Error("record audit", "action", "entry.create", "error", err)
	}
	// Fire-and-forget: emission returns before any receiver is contacted.
	base := httpx.BaseURL(r, a.publicBaseURL, a.trustProxy)
	a.hooks.Emit(webhook.Change{Event: webhook.EventEntryCreated, Entry: entry, BaseURL: base})
	if a.notify != nil {
		a.notify.EntryCreated(r.Context(), entry, base)
	}
	httpx.WriteJSON(rw, http.StatusCreated, entry)
}

func (a *API) handleGetEntryPhoto(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_id", "id must be a positive integer")
		return
	}
	size := strings.TrimSpace(r.URL.Query().Get("size"))
	if size == "" {
		size = "display"
	}
	if size != "display" && size != "thumb" {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_size", "size must be display or thumb")
		return
	}
	if _, err := a.store.GetEntry(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.WriteError(rw, http.StatusNotFound, "not_found", "entry does not exist")
			return
		}
		a.logger.Error("get entry for photo", "error", err, "id", id)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not load entry")
		return
	}
	photo, err := a.store.GetEntryPhotoSize(r.Context(), id, size)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(rw, http.StatusNotFound, "not_found", "entry photo does not exist")
		return
	case err != nil:
		a.logger.Error("get entry photo", "error", err, "id", id)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not load entry photo")
		return
	}

	etag := `"` + photo.SHA256 + `"`
	rw.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		rw.WriteHeader(http.StatusNotModified)
		return
	}
	rw.Header().Set("Content-Type", photo.ContentType)
	rw.Header().Set("Content-Length", strconv.Itoa(len(photo.Data)))
	rw.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	rw.WriteHeader(http.StatusOK)
	if _, err := rw.Write(photo.Data); err != nil {
		a.logger.Warn("write entry photo", "error", err, "id", id)
	}
}

// handleRandom draws one live entry with the optional photo and person
// filters. It is a sibling of the collection, not a literal under the id path,
// so GET /api/v1/entries/random keeps answering 400 invalid_id.
func (a *API) handleRandom(rw http.ResponseWriter, r *http.Request) {
	entry, err := a.store.RandomEntry(r.Context(),
		strings.TrimSpace(r.URL.Query().Get("person")),
		queryOptionalBool(r, "photo"))
	rw.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(rw, http.StatusNotFound, "not_found", "no entry matches the draw")
	case err != nil:
		a.logger.Error("random entry", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not draw an entry")
	default:
		httpx.WriteJSON(rw, http.StatusOK, entry)
	}
}

// slipResponse is the JSON surface for the day's pinned entry. The entry uses
// the same shape as GET /api/v1/entries/{id}.
type slipResponse struct {
	Date  string      `json:"date"`
	Entry store.Entry `json:"entry"`
}

// handleSlip serves the deterministic slip of the day with a strong ETag and a
// short public cache lifetime, mirroring the Atom feed's conditional pattern.
func (a *API) handleSlip(rw http.ResponseWriter, r *http.Request) {
	today, err := a.slip.Today(r.Context())
	switch {
	case errors.Is(err, slip.ErrNoSlip):
		httpx.WriteError(rw, http.StatusNotFound, "not_found", "the ledger has no entry for today")
		return
	case err != nil:
		a.logger.Error("resolve daily slip", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not load the slip")
		return
	}

	tag := fmt.Sprintf(`"slip-%s-%d"`, today.Date, today.Entry.ID)
	rw.Header().Set("ETag", tag)
	rw.Header().Set("Cache-Control", "public, max-age=60")
	if etagMatches(r.Header.Get("If-None-Match"), tag) {
		rw.WriteHeader(http.StatusNotModified)
		return
	}
	httpx.WriteJSON(rw, http.StatusOK, slipResponse{Date: today.Date, Entry: today.Entry})
}

// etagMatches reports whether an If-None-Match header names tag. Weak prefixes
// and "*" are accepted, matching the feed's helper.
func etagMatches(header, tag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == tag || candidate == "*" {
			return true
		}
	}
	return false
}

func (a *API) handleStats(rw http.ResponseWriter, r *http.Request) {
	stats, err := a.store.Stats(r.Context())
	if err != nil {
		a.logger.Error("stats", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not load stats")
		return
	}
	rw.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(rw, http.StatusOK, stats)
}

type personCountsResponse struct {
	Items []store.PersonCount `json:"items"`
	Total int                 `json:"total"`
}

func (a *API) handlePersons(rw http.ResponseWriter, r *http.Request) {
	counts, err := a.store.PersonCounts(r.Context())
	if err != nil {
		a.logger.Error("person counts", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not load persons")
		return
	}
	if counts == nil {
		counts = []store.PersonCount{}
	}
	rw.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(rw, http.StatusOK, personCountsResponse{Items: counts, Total: len(counts)})
}

// rateLimited applies the per-IP write limiter.
func (a *API) rateLimited(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		ip := httpx.ClientIP(r, a.trustProxy)
		if !a.limiter.Allow(ip) {
			a.metrics.IncRateLimited()
			a.metrics.IncEntryAddition("rate_limited")
			rw.Header().Set("Retry-After", strconv.Itoa(a.limiter.RetryAfterSeconds()))
			httpx.WriteError(rw, http.StatusTooManyRequests, "rate_limited", "too many entries added; try again later")
			return
		}
		next(rw, r)
	}
}

func queryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func queryOptionalBool(r *http.Request, key string) *bool {
	raw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key)))
	if raw == "" {
		return nil
	}
	value := raw == "1" || raw == "true" || raw == "yes"
	return &value
}

// queryDate parses an optional YYYY-MM-DD parameter. A missing value is the
// zero time (unset); a malformed one is reported so the handler can answer
// invalid_date instead of silently dropping the filter.
func queryDate(r *http.Request, key string) (time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02", raw)
}
