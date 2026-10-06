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
)

const maxBodyBytes = 64 << 10

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
	mux.HandleFunc("GET /api/v1/stats", a.handleStats)
}

func (a *API) handleHealth(rw http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(rw, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleListEntries(rw http.ResponseWriter, r *http.Request) {
	page, err := a.store.ListEntries(r.Context(), store.ListParams{
		Query:   r.URL.Query().Get("q"),
		Sort:    r.URL.Query().Get("sort"),
		Order:   r.URL.Query().Get("order"),
		Page:    queryInt(r, "page", 1),
		PerPage: queryInt(r, "per_page", store.DefaultPerPage),
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
	r.Body = http.MaxBytesReader(rw, r.Body, maxBodyBytes)

	var input struct {
		Word    string `json:"word"`
		Meaning string `json:"meaning"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&input); err != nil {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_json", "request body must be JSON: {\"word\": \"...\", \"meaning\": \"...\"}")
		return
	}

	input.Word = strings.TrimSpace(input.Word)
	input.Meaning = strings.TrimSpace(input.Meaning)
	if err := store.ValidateEntry(input.Word, input.Meaning); err != nil {
		httpx.WriteError(rw, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	entry, err := a.store.CreateEntry(r.Context(), input.Word, input.Meaning)
	if err != nil {
		a.logger.Error("create entry", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not create entry")
		return
	}
	rw.Header().Set("Location", fmt.Sprintf("/api/v1/entries/%d", entry.ID))
	httpx.WriteJSON(rw, http.StatusCreated, entry)
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
