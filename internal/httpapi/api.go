// Package httpapi exposes the JSON REST API of the vanDyke ledger.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/upload"
)

const (
	maxBodyBytes       = 64 << 10
	maxUploadBodyBytes = store.MaxPhotoBytes + (1 << 20)
)

// API serves /api/v1 endpoints.
type API struct {
	store      *store.Store
	limiter    *ratelimit.Limiter
	trustProxy bool
	logger     *slog.Logger
}

// New creates the API.
func New(st *store.Store, limiter *ratelimit.Limiter, trustProxy bool, logger *slog.Logger) *API {
	return &API{store: st, limiter: limiter, trustProxy: trustProxy, logger: logger}
}

// Register wires the API routes into mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", a.handleHealth)
	mux.HandleFunc("GET /api/v1/entries", a.handleListEntries)
	mux.HandleFunc("GET /api/v1/entries/{id}", a.handleGetEntry)
	mux.HandleFunc("POST /api/v1/entries", a.rateLimited(a.handleCreateEntry))
	mux.HandleFunc("GET /api/v1/entries/{id}/photo", a.handleGetEntryPhoto)
	mux.HandleFunc("GET /api/v1/stats", a.handleStats)
}

func (a *API) handleHealth(rw http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(rw, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleListEntries(rw http.ResponseWriter, r *http.Request) {
	page, err := a.store.ListEntries(r.Context(), store.ListParams{
		Query:    r.URL.Query().Get("q"),
		Sort:     r.URL.Query().Get("sort"),
		Order:    r.URL.Query().Get("order"),
		Page:     queryInt(r, "page", 1),
		PerPage:  queryInt(r, "per_page", store.DefaultPerPage),
		HasPhoto: queryOptionalBool(r, "photo"),
		Year:     queryInt(r, "year", 0),
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
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&input); err != nil {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_json", "request body must be JSON: {\"word\": \"...\", \"meaning\": \"...\"} or multipart/form-data with an optional photo")
		return
	}

	input.Word = strings.TrimSpace(input.Word)
	input.Meaning = strings.TrimSpace(input.Meaning)
	if err := store.ValidateEntry(input.Word, input.Meaning, false); err != nil {
		httpx.WriteError(rw, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	a.writeCreatedEntry(rw, r, input.Word, input.Meaning, nil)
}

func (a *API) createEntryMultipart(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, maxUploadBodyBytes)
	if err := r.ParseMultipartForm(64 << 10); err != nil {
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

	photo, err := upload.Photo(r, "photo")
	if err != nil {
		httpx.WriteError(rw, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	if err := store.ValidateEntry(word, meaning, photo != nil); err != nil {
		httpx.WriteError(rw, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	a.writeCreatedEntry(rw, r, word, meaning, photo)
}

func (a *API) writeCreatedEntry(rw http.ResponseWriter, r *http.Request, word, meaning string, photo *store.Photo) {
	entry, err := a.store.CreateEntry(r.Context(), word, meaning, photo)
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			httpx.WriteError(rw, http.StatusConflict, "duplicate_entry", "that entry is already in the ledger")
			return
		}
		a.logger.Error("create entry", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not create entry")
		return
	}
	rw.Header().Set("Location", fmt.Sprintf("/api/v1/entries/%d", entry.ID))
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

// rateLimited applies the per-IP write limiter.
func (a *API) rateLimited(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		ip := httpx.ClientIP(r, a.trustProxy)
		if !a.limiter.Allow(ip) {
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
