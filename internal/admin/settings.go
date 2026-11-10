package admin

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
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
	WebhookResult       string
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
		state := stateFrom(r)
		a.render(rw, http.StatusUnprocessableEntity, "admin-settings", settingsData{
			Admin:            state.Admin,
			Umami:            a.settings.Current(),
			Env:              a.settings.Env(),
			CSRF:             state.Session.CSRFToken,
			Values:           values,
			Error:            capitalize(err.Error()),
			WebhookURL:       a.settings.CurrentWebhook().URL,
			WebhookEnvURL:    a.settings.EnvWebhook().URL,
			WebhookSecretSet: a.settings.CurrentWebhook().Secret != "",
		})
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

// handleWebhookTestFire sends one production-shaped delivery and reports the
// receiver's status or a sanitised error on the settings page. Unlike entry
// creation this waits, because the operator asked for a result.
func (a *Admin) handleWebhookTestFire(rw http.ResponseWriter, r *http.Request) {
	data, err := a.settingsPage(r, "")
	if err != nil {
		a.internalError(rw, err)
		return
	}

	effective := a.settings.CurrentWebhook()
	if !effective.Enabled() {
		data.WebhookResult = "No webhook configured; no request was sent."
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
	status, err := a.hooks.Send(r.Context(), payload)
	if err != nil {
		a.logger.Warn("webhook test-fire failed", "error", err)
		data.WebhookResult = "Test delivery failed: " + deliveryError(err)
		a.render(rw, http.StatusOK, "admin-settings", data)
		return
	}
	data.WebhookResult = fmt.Sprintf("Test delivery returned HTTP %d.", status)
	a.render(rw, http.StatusOK, "admin-settings", data)
}

// deliveryError strips the target URL from a transport error so credentials
// embedded in the operator-supplied URL cannot be echoed into HTML.
func deliveryError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
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
