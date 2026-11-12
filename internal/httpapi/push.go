package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"vandyke.thor.edu/vandyke/internal/auth"
	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/store"
)

// adminSessionCookie is the cookie the admin UI sets after a successful login.
// The API only reads it to associate a browser push subscription with the
// administrator who created it; it does not authenticate any API request.
const adminSessionCookie = "vandyke_admin"

const (
	maxPushDeviceBody       = 8 << 10
	maxPushSubscriptionBody = 16 << 10
)

type pushFirebaseConfig struct {
	APIKey            string `json:"api_key"`
	AppID             string `json:"app_id"`
	MessagingSenderID string `json:"messaging_sender_id"`
	ProjectID         string `json:"project_id"`
}

type pushConfigResponse struct {
	Enabled        bool               `json:"enabled"`
	Realtime       bool               `json:"realtime"`
	WebPush        bool               `json:"web_push"`
	Android        bool               `json:"android"`
	VAPIDPublicKey string             `json:"vapid_public_key"`
	Firebase       pushFirebaseConfig `json:"firebase"`
}

// handlePushConfig exposes the non-secret client capability configuration.
func (a *API) handlePushConfig(rw http.ResponseWriter, _ *http.Request) {
	cfg := a.notifications()
	rw.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(rw, http.StatusOK, pushConfigResponse{
		Enabled:        cfg.Enabled,
		Realtime:       cfg.Realtime,
		WebPush:        cfg.WebPush,
		Android:        cfg.Android,
		VAPIDPublicKey: cfg.VAPIDPublicKey,
		Firebase: pushFirebaseConfig{
			APIKey:            cfg.FirebaseAPIKey,
			AppID:             cfg.FirebaseAppID,
			MessagingSenderID: cfg.FirebaseSenderID,
			ProjectID:         cfg.FCMProjectID,
		},
	})
}

// handleEvents streams realtime events. ServeHTTP clears the server write
// deadline itself, so this endpoint can stay open past WriteTimeout.
func (a *API) handleEvents(rw http.ResponseWriter, r *http.Request) {
	if a.notify == nil || a.notify.Hub() == nil {
		httpx.WriteError(rw, http.StatusServiceUnavailable, "internal_error", "realtime updates are unavailable")
		return
	}
	a.notify.Hub().ServeHTTP(rw, r)
}

type pushDeviceRequest struct {
	Token      string `json:"token"`
	Platform   string `json:"platform"`
	DeviceName string `json:"device_name"`
	AppVersion string `json:"app_version"`
}

// handleUpsertPushDevice registers or refreshes an Android FCM token.
func (a *API) handleUpsertPushDevice(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, maxPushDeviceBody)
	var input pushDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_request", "request body must be JSON: {\"token\": \"...\", \"platform\": \"android\"}")
		return
	}

	token := strings.TrimSpace(input.Token)
	if token == "" {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	platform := strings.TrimSpace(input.Platform)
	if platform == "" {
		platform = "android"
	}
	if platform != "android" {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_request", "platform must be android")
		return
	}

	device, err := a.store.UpsertPushDevice(r.Context(), store.PushDevice{
		Token:      token,
		Platform:   platform,
		DeviceName: strings.TrimSpace(input.DeviceName),
		AppVersion: strings.TrimSpace(input.AppVersion),
	})
	if err != nil {
		a.logger.Error("upsert push device", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not register the device")
		return
	}
	httpx.WriteJSON(rw, http.StatusCreated, map[string]int64{"id": device.ID})
}

// handleDeletePushDevice removes an FCM token. It is idempotent.
func (a *API) handleDeletePushDevice(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, maxPushDeviceBody)
	var input struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_request", "request body must be JSON: {\"token\": \"...\"}")
		return
	}
	token := strings.TrimSpace(input.Token)
	if token == "" {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	if err := a.store.DeletePushDevice(r.Context(), token); err != nil {
		a.logger.Error("delete push device", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not remove the device")
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

type webPushSubscriptionRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// handleUpsertWebPushSubscription stores a browser subscription, associating it
// with the signed-in administrator when the admin cookie resolves.
func (a *API) handleUpsertWebPushSubscription(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, maxPushSubscriptionBody)
	var input webPushSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_request", "request body must be JSON: {\"endpoint\": \"...\", \"keys\": {...}}")
		return
	}

	endpoint := strings.TrimSpace(input.Endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_request", "endpoint must be an absolute https URL")
		return
	}
	p256dh := strings.TrimSpace(input.Keys.P256DH)
	authKey := strings.TrimSpace(input.Keys.Auth)
	if p256dh == "" || authKey == "" {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_request", "keys.p256dh and keys.auth are required")
		return
	}

	var adminID *int64
	if cookie, cookieErr := r.Cookie(adminSessionCookie); cookieErr == nil && cookie.Value != "" {
		if info, sessionErr := a.store.GetSession(r.Context(), auth.HashToken(cookie.Value)); sessionErr == nil {
			id := info.Admin.ID
			adminID = &id
		}
	}

	sub, err := a.store.UpsertWebPushSubscription(r.Context(), store.WebPushSubscription{
		Endpoint:  endpoint,
		P256DH:    p256dh,
		Auth:      authKey,
		UserAgent: r.UserAgent(),
		AdminID:   adminID,
	})
	if err != nil {
		a.logger.Error("upsert web push subscription", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not register the subscription")
		return
	}
	httpx.WriteJSON(rw, http.StatusCreated, map[string]int64{"id": sub.ID})
}

// handleDeleteWebPushSubscription removes a browser subscription. It is
// idempotent.
func (a *API) handleDeleteWebPushSubscription(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, maxPushSubscriptionBody)
	var input struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_request", "request body must be JSON: {\"endpoint\": \"...\"}")
		return
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	if endpoint == "" {
		httpx.WriteError(rw, http.StatusBadRequest, "invalid_request", "endpoint is required")
		return
	}
	if err := a.store.DeleteWebPushSubscription(r.Context(), endpoint); err != nil {
		a.logger.Error("delete web push subscription", "error", err)
		httpx.WriteError(rw, http.StatusInternalServerError, "internal_error", "could not remove the subscription")
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

// notifications returns the effective configuration, or zero when no notifier
// is wired (as in most API unit tests).
func (a *API) notifications() config.Notifications {
	if a.notify == nil {
		return config.Notifications{}
	}
	return a.notify.Notifications()
}
