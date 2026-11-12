package admin

import (
	"fmt"
	"net/http"
	"strings"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/push"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/webhook"
)

type settingsData struct {
	Admin   store.Admin
	Umami   config.Umami
	Env     config.Umami
	CSRF    string
	Values  config.Umami
	Error   string
	Saved   bool
	Rebuilt bool

	// WebhookURL is the effective target shown in the URL field. The stored
	// secret is never loaded into the view; WebhookSecretSet only reports
	// whether one is configured.
	WebhookURL          string
	WebhookEnvURL       string
	WebhookSecretSet    bool
	WebhookEnvSecretSet bool
	WebhookActive       bool
	WebhookSuppressed   bool
	WebhookResult       string

	// Notification settings. Notifications holds the persisted overrides (with
	// private keys blanked); NotificationsEnv is the non-secret environment
	// fallback copy. The private-set flags report the effective configuration
	// without ever loading a secret into the view.
	Notifications    config.Notifications
	NotificationsEnv config.Notifications
	VAPIDPrivateSet  bool
	FCMPrivateSet    bool
	NotifyActive     bool
	NotifyResult     string
}

// settingsPage loads the persisted overrides and builds the view model shared
// by the settings GET, the save error path, the search rebuild action and the
// webhook test-fire.
func (a *Admin) settingsPage(r *http.Request, errMsg string) (settingsData, error) {
	overrides, err := a.settings.Overrides(r.Context())
	if err != nil {
		return settingsData{}, err
	}
	state := stateFrom(r)
	effective := a.settings.CurrentWebhook()
	legacy, err := a.hooks.LegacyStatus(r.Context())
	if err != nil {
		return settingsData{}, err
	}
	notif, err := a.settings.OverridesNotifications(r.Context())
	if err != nil {
		return settingsData{}, err
	}
	effectiveNotif := a.settings.CurrentNotifications()
	envNotif := a.settings.EnvNotifications()
	// Private keys are write-only: never carry them into the view model.
	envNotif.VAPIDPrivateKey = ""
	envNotif.FCMPrivateKey = ""
	return settingsData{
		Admin:               state.Admin,
		Umami:               a.settings.Current(),
		Env:                 a.settings.Env(),
		CSRF:                state.Session.CSRFToken,
		Values:              overrides,
		Saved:               r.URL.Query().Get("saved") == "1",
		Rebuilt:             r.URL.Query().Get("rebuilt") == "1",
		Error:               errMsg,
		WebhookURL:          effective.URL,
		WebhookEnvURL:       a.settings.EnvWebhook().URL,
		WebhookSecretSet:    effective.Secret != "",
		WebhookEnvSecretSet: a.settings.EnvWebhook().Secret != "",
		WebhookActive:       legacy.Configured && legacy.Active,
		WebhookSuppressed:   legacy.Configured && !legacy.Active,
		Notifications:       notif,
		NotificationsEnv:    envNotif,
		VAPIDPrivateSet:     effectiveNotif.VAPIDPrivateKey != "",
		FCMPrivateSet:       effectiveNotif.FCMPrivateKey != "",
		NotifyActive:        a.notify != nil && effectiveNotif.Enabled,
	}, nil
}

func (a *Admin) handleSettings(rw http.ResponseWriter, r *http.Request) {
	data, err := a.settingsPage(r, "")
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.render(rw, http.StatusOK, "admin-settings", data)
}

func (a *Admin) handleSaveSettings(rw http.ResponseWriter, r *http.Request) {
	// The page carries separate Umami and webhook forms posting to this route;
	// the marker keeps one save from blanking the other section. An unmarked
	// post keeps the historical behaviour and saves Umami only.
	if r.FormValue("settings_form") == "webhook" {
		a.saveWebhook(rw, r)
		return
	}
	if r.FormValue("settings_form") == "notifications" {
		a.saveNotifications(rw, r)
		return
	}

	values := config.Umami{
		URL:       r.FormValue("umami_url"),
		WebsiteID: r.FormValue("umami_website_id"),
		Domains:   r.FormValue("umami_domains"),
	}
	previous, err := a.settings.Overrides(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if err := a.settings.Save(r.Context(), values); err != nil {
		data, pageErr := a.settingsPage(r, capitalize(err.Error()))
		if pageErr != nil {
			a.internalError(rw, pageErr)
			return
		}
		data.Values = values
		a.render(rw, http.StatusUnprocessableEntity, "admin-settings", data)
		return
	}
	var changed []string
	if strings.TrimSpace(values.URL) != previous.URL {
		changed = append(changed, "umami_url")
	}
	if strings.TrimSpace(values.WebsiteID) != previous.WebsiteID {
		changed = append(changed, "umami_website_id")
	}
	if strings.TrimSpace(values.Domains) != previous.Domains {
		changed = append(changed, "umami_domains")
	}
	if len(changed) > 0 {
		a.auditAs(r, "settings.update", "settings", "changed: "+strings.Join(changed, ","))
	} else {
		a.auditAs(r, "settings.update", "settings", "no change")
	}
	http.Redirect(rw, r, "/admin/settings?saved=1", http.StatusSeeOther)
}

func (a *Admin) saveWebhook(rw http.ResponseWriter, r *http.Request) {
	values := config.Webhook{
		URL:    r.FormValue("webhook_url"),
		Secret: r.FormValue("webhook_secret"),
	}
	clearSecret := r.FormValue("webhook_clear_secret") != ""
	previous := a.settings.CurrentWebhook()
	if err := a.settings.SaveWebhook(r.Context(), values, clearSecret); err != nil {
		data, pageErr := a.settingsPage(r, capitalize(err.Error()))
		if pageErr != nil {
			a.internalError(rw, pageErr)
			return
		}
		data.WebhookURL = values.URL
		a.render(rw, http.StatusUnprocessableEntity, "admin-settings", data)
		return
	}
	// Record which keys changed, never the secret value itself.
	var changed []string
	if strings.TrimSpace(values.URL) != previous.URL {
		changed = append(changed, "webhook.url")
	}
	if clearSecret || strings.TrimSpace(values.Secret) != "" {
		changed = append(changed, "webhook.secret")
	}
	if len(changed) > 0 {
		a.auditAs(r, "settings.update", "settings", "changed: "+strings.Join(changed, ","))
	} else {
		a.auditAs(r, "settings.update", "settings", "no change")
	}
	http.Redirect(rw, r, "/admin/settings?saved=1", http.StatusSeeOther)
}

// saveNotifications validates and persists the notification configuration.
// Invalid values (notably a malformed click URL) re-render the page with an
// inline error instead of redirecting, so the operator can correct the form.
func (a *Admin) saveNotifications(rw http.ResponseWriter, r *http.Request) {
	values := config.Notifications{
		Enabled:          r.FormValue("notifications_enabled") != "",
		Realtime:         r.FormValue("notifications_realtime") != "",
		WebPush:          r.FormValue("notifications_webpush") != "",
		Android:          r.FormValue("notifications_android") != "",
		EntryCreated:     r.FormValue("notifications_entry_created") != "",
		TitleTemplate:    r.FormValue("notifications_title"),
		BodyTemplate:     r.FormValue("notifications_body"),
		ClickURL:         r.FormValue("notifications_click_url"),
		VAPIDSubject:     r.FormValue("notifications_vapid_subject"),
		VAPIDPublicKey:   r.FormValue("notifications_vapid_public_key"),
		VAPIDPrivateKey:  r.FormValue("notifications_vapid_private_key"),
		FCMProjectID:     r.FormValue("notifications_fcm_project_id"),
		FCMClientEmail:   r.FormValue("notifications_fcm_client_email"),
		FCMPrivateKey:    r.FormValue("notifications_fcm_private_key"),
		FirebaseAPIKey:   r.FormValue("notifications_firebase_api_key"),
		FirebaseAppID:    r.FormValue("notifications_firebase_app_id"),
		FirebaseSenderID: r.FormValue("notifications_firebase_sender_id"),
	}
	clearVAPID := r.FormValue("notifications_vapid_clear_private") != ""
	clearFCM := r.FormValue("notifications_fcm_clear_private") != ""
	previous := a.settings.CurrentNotifications()

	if err := a.settings.SaveNotifications(r.Context(), values, clearVAPID, clearFCM); err != nil {
		data, pageErr := a.settingsPage(r, capitalize(err.Error()))
		if pageErr != nil {
			a.internalError(rw, pageErr)
			return
		}
		// Keep the operator's entries on screen; private values are never
		// rendered, so retaining them here only affects non-secret fields.
		data.Notifications = values
		a.render(rw, http.StatusUnprocessableEntity, "admin-settings", data)
		return
	}

	changed := notificationChanges(values, previous, clearVAPID, clearFCM)
	if len(changed) > 0 {
		a.auditAs(r, "settings.update", "settings", "changed: "+strings.Join(changed, ","))
	} else {
		a.auditAs(r, "settings.update", "settings", "no change")
	}
	http.Redirect(rw, r, "/admin/settings?saved=1#notifications", http.StatusSeeOther)
}

// notificationChanges lists the settings keys whose effective value changed.
// Secret values are compared but never reported.
func notificationChanges(v, previous config.Notifications, clearVAPID, clearFCM bool) []string {
	var changed []string
	add := func(name string, different bool) {
		if different {
			changed = append(changed, name)
		}
	}
	add("notifications.enabled", v.Enabled != previous.Enabled)
	add("notifications.realtime", v.Realtime != previous.Realtime)
	add("notifications.webpush", v.WebPush != previous.WebPush)
	add("notifications.android", v.Android != previous.Android)
	add("notifications.entry_created", v.EntryCreated != previous.EntryCreated)
	add("notifications.title", strings.TrimSpace(v.TitleTemplate) != previous.TitleTemplate)
	add("notifications.body", strings.TrimSpace(v.BodyTemplate) != previous.BodyTemplate)
	add("notifications.click_url", strings.TrimSpace(v.ClickURL) != previous.ClickURL)
	add("notifications.vapid.subject", strings.TrimSpace(v.VAPIDSubject) != previous.VAPIDSubject)
	add("notifications.vapid.public_key", strings.TrimSpace(v.VAPIDPublicKey) != previous.VAPIDPublicKey)
	add("notifications.vapid.private_key", clearVAPID || (strings.TrimSpace(v.VAPIDPrivateKey) != "" && strings.TrimSpace(v.VAPIDPrivateKey) != previous.VAPIDPrivateKey))
	add("notifications.fcm.project_id", strings.TrimSpace(v.FCMProjectID) != previous.FCMProjectID)
	add("notifications.fcm.client_email", strings.TrimSpace(v.FCMClientEmail) != previous.FCMClientEmail)
	add("notifications.fcm.private_key", clearFCM || (strings.TrimSpace(v.FCMPrivateKey) != "" && strings.TrimSpace(v.FCMPrivateKey) != previous.FCMPrivateKey))
	add("notifications.firebase.api_key", strings.TrimSpace(v.FirebaseAPIKey) != previous.FirebaseAPIKey)
	add("notifications.firebase.app_id", strings.TrimSpace(v.FirebaseAppID) != previous.FirebaseAppID)
	add("notifications.firebase.sender_id", strings.TrimSpace(v.FirebaseSenderID) != previous.FirebaseSenderID)
	return changed
}

// handleNotificationTest sends one test notification through every configured
// channel and reports the outcome on the settings page. Unlike entry fan-out it
// waits, because the operator explicitly asked for a result.
func (a *Admin) handleNotificationTest(rw http.ResponseWriter, r *http.Request) {
	data, err := a.settingsPage(r, "")
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if a.notify == nil {
		data.NotifyResult = "Notifications are not wired in this environment."
		a.render(rw, http.StatusOK, "admin-settings", data)
		return
	}

	sent, failed, sendErr := a.notify.TestSend(r.Context())
	switch {
	case sendErr != nil:
		data.NotifyResult = "Test failed: " + capitalize(sendErr.Error())
	default:
		data.NotifyResult = fmt.Sprintf("Test sent to %d %s (%d failed).",
			sent, plural(sent, "device", "devices"), failed)
	}
	a.render(rw, http.StatusOK, "admin-settings", data)
}

// handleVAPIDGenerate replaces the VAPID key pair with a freshly generated one.
// The public key becomes visible in the form; the private key is write-only and
// is never echoed back.
func (a *Admin) handleVAPIDGenerate(rw http.ResponseWriter, r *http.Request) {
	publicKey, privateKey, err := push.GenerateVAPIDKeys()
	if err != nil {
		a.internalError(rw, err)
		return
	}
	overrides, err := a.settings.OverridesNotifications(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	// Preserve every stored override, keep the effective on/off switches (so an
	// environment-enabled channel is not accidentally disabled), and change
	// only the VAPID key pair. Empty non-secret fields keep falling back to the
	// environment/defaults instead of pinning a built-in default as an override.
	current := a.settings.CurrentNotifications()
	values := overrides
	values.Enabled = current.Enabled
	values.Realtime = current.Realtime
	values.WebPush = current.WebPush
	values.Android = current.Android
	values.EntryCreated = current.EntryCreated
	values.VAPIDPublicKey = publicKey
	values.VAPIDPrivateKey = privateKey
	if err := a.settings.SaveNotifications(r.Context(), values, false, false); err != nil {
		data, pageErr := a.settingsPage(r, capitalize(err.Error()))
		if pageErr != nil {
			a.internalError(rw, pageErr)
			return
		}
		a.render(rw, http.StatusUnprocessableEntity, "admin-settings", data)
		return
	}
	a.auditAs(r, "settings.update", "settings", "notifications.vapid.generated")
	http.Redirect(rw, r, "/admin/settings?saved=1#notifications", http.StatusSeeOther)
}

// handleWebhookTestFire sends one production-shaped delivery and reports the
// receiver's status or a sanitised error on the settings page. Unlike entry
// creation this waits, because the operator asked for a result.
func (a *Admin) handleWebhookTestFire(rw http.ResponseWriter, r *http.Request) {
	data, err := a.settingsPage(r, "")
	if err != nil {
		a.internalError(rw, err)
		return
	}

	legacy, err := a.hooks.LegacyStatus(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if !legacy.Configured {
		data.WebhookResult = "No legacy webhook configured; no request was sent."
		a.render(rw, http.StatusOK, "admin-settings", data)
		return
	}
	if !legacy.Active {
		data.WebhookResult = "The legacy webhook is suppressed while a persisted endpoint is enabled. Manage endpoints at /admin/webhooks."
		a.render(rw, http.StatusOK, "admin-settings", data)
		return
	}
	endpoint, ok := a.hooks.LegacyEndpoint()
	if !ok {
		data.WebhookResult = "No legacy webhook configured; no request was sent."
		a.render(rw, http.StatusOK, "admin-settings", data)
		return
	}

	var entry store.Entry
	page, err := a.store.ListEntries(r.Context(), store.ListParams{
		Sort: "created_at", Order: "desc", PerPage: 1,
	})
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if len(page.Entries) > 0 {
		entry = page.Entries[0]
	}

	base := httpx.BaseURL(r, a.cfg.PublicBaseURL, a.cfg.TrustProxy)
	payload := webhook.BuildPayload(entry, fmt.Sprintf("%s/e/%d", base, entry.ID))
	status, err := a.hooks.SendTest(r.Context(), endpoint, payload)
	if err != nil {
		a.logger.Warn("webhook test-fire failed", "error", err)
		data.WebhookResult = "Test delivery failed: " + webhook.SanitiseError(err, endpoint.URL)
		a.render(rw, http.StatusOK, "admin-settings", data)
		return
	}
	data.WebhookResult = fmt.Sprintf("Test delivery returned HTTP %d.", status)
	a.render(rw, http.StatusOK, "admin-settings", data)
}

// handleSearchRebuild regenerates the trigram index from the live entries. It
// is the operator's repair action for a drifted index; failures are reported on
// the settings page instead of a false confirmation.
func (a *Admin) handleSearchRebuild(rw http.ResponseWriter, r *http.Request) {
	if err := a.store.RebuildSearchIndex(r.Context()); err != nil {
		a.logger.Error("rebuild search index", "error", err)
		data, pageErr := a.settingsPage(r, "Could not rebuild the search index. Try again.")
		if pageErr != nil {
			a.internalError(rw, pageErr)
			return
		}
		a.render(rw, http.StatusInternalServerError, "admin-settings", data)
		return
	}
	http.Redirect(rw, r, "/admin/settings?rebuilt=1", http.StatusSeeOther)
}
