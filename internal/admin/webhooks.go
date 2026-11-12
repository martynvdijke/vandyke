package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/webhook"
)

const webhookDeliveryPerPage = 25

type webhookEndpointView struct {
	store.WebhookEndpoint
	Host        string
	MaskedURL   string
	SecretSet   bool
	EventsLabel string
	Counts      map[string]int
}

type webhookLegacyView struct {
	Configured bool
	Active     bool
	MaskedURL  string
	SecretSet  bool
}

type webhookDeliveryRow struct {
	store.WebhookDelivery
	EndpointLabel string
	CanRetry      bool
}

type webhookPageData struct {
	Admin         store.Admin
	Umami         config.Umami
	CSRF          string
	Endpoints     []webhookEndpointView
	Legacy        webhookLegacyView
	Deliveries    []webhookDeliveryRow
	DeliveryTotal int
	DeliveryPage  int
	DeliveryPages int
	PerPage       int

	FilterEndpoint string
	FilterStatus   string
	Statuses       []string
	EventNames     []string
	Presets        []string

	FormAction       string
	FormTitle        string
	EditID           int64
	FormName         string
	FormURL          string
	FormPreset       string
	FormEvents       map[string]bool
	FormAllowPrivate bool
	FormSecretSet    bool
	FormEnabled      bool
	IsOwner          bool

	Flash      string
	Error      string
	TestResult string
}

// handleWebhooks renders the endpoint list, the create/edit form and the
// delivery log. Reads are available to every authenticated admin.
func (a *Admin) handleWebhooks(rw http.ResponseWriter, r *http.Request) {
	data, err := a.webhookPage(r, "", "")
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.render(rw, http.StatusOK, "admin-webhooks", data)
}

func (a *Admin) webhookPage(r *http.Request, errMsg, testResult string) (webhookPageData, error) {
	endpoints, err := a.store.ListWebhookEndpoints(r.Context())
	if err != nil {
		return webhookPageData{}, err
	}
	counts, err := a.store.WebhookDeliveryCounts(r.Context())
	if err != nil {
		return webhookPageData{}, err
	}
	byEndpoint := make(map[int64]map[string]int)
	for _, count := range counts {
		if count.EndpointID == nil {
			continue
		}
		m := byEndpoint[*count.EndpointID]
		if m == nil {
			m = make(map[string]int)
			byEndpoint[*count.EndpointID] = m
		}
		m[count.Status] += count.Count
	}

	names := make(map[int64]string, len(endpoints))
	views := make([]webhookEndpointView, 0, len(endpoints))
	for _, ep := range endpoints {
		host, masked := maskWebhookURL(ep.URL)
		eventCounts := byEndpoint[ep.ID]
		if eventCounts == nil {
			eventCounts = make(map[string]int)
		}
		for _, status := range []string{store.WebhookStatusPending, store.WebhookStatusDelivered,
			store.WebhookStatusFailed, store.WebhookStatusCancelled} {
			if _, ok := eventCounts[status]; !ok {
				eventCounts[status] = 0
			}
		}
		names[ep.ID] = ep.Name
		views = append(views, webhookEndpointView{
			WebhookEndpoint: ep,
			Host:            host,
			MaskedURL:       masked,
			SecretSet:       ep.Secret != "",
			EventsLabel:     strings.Join(ep.Events, ", "),
			Counts:          eventCounts,
		})
	}

	legacyStatus, err := a.hooks.LegacyStatus(r.Context())
	if err != nil {
		return webhookPageData{}, err
	}
	_, legacyMasked := maskWebhookURL(legacyStatus.URL)
	data := webhookPageData{
		Admin:       stateFrom(r).Admin,
		Umami:       a.settings.Current(),
		CSRF:        stateFrom(r).Session.CSRFToken,
		Endpoints:   views,
		Legacy:      webhookLegacyView{Configured: legacyStatus.Configured, Active: legacyStatus.Active, MaskedURL: legacyMasked, SecretSet: legacyStatus.SecretSet},
		Statuses:    []string{store.WebhookStatusPending, store.WebhookStatusDelivered, store.WebhookStatusFailed, store.WebhookStatusCancelled},
		EventNames:  store.WebhookEvents(),
		Presets:     []string{store.WebhookPresetGeneric, store.WebhookPresetDiscord, store.WebhookPresetSlack},
		FormAction:  "/admin/webhooks",
		FormTitle:   "Add an endpoint",
		FormPreset:  store.WebhookPresetGeneric,
		FormEvents:  map[string]bool{store.WebhookEventCreated: true},
		FormEnabled: true,
		IsOwner:     stateFrom(r).Admin.IsOwner(),
		Flash:       webhookFlash(r),
		Error:       errMsg,
		TestResult:  testResult,
	}

	if edit := strings.TrimSpace(r.URL.Query().Get("edit")); edit != "" {
		id, err := parseID(edit)
		if err == nil {
			ep, getErr := a.store.GetWebhookEndpoint(r.Context(), id)
			if getErr == nil {
				data.EditID = ep.ID
				data.FormAction = fmt.Sprintf("/admin/webhooks/%d", ep.ID)
				data.FormTitle = "Edit " + ep.Name
				data.FormName = ep.Name
				data.FormURL = ep.URL
				data.FormPreset = ep.Preset
				data.FormEvents = make(map[string]bool, len(ep.Events))
				for _, event := range ep.Events {
					data.FormEvents[event] = true
				}
				data.FormAllowPrivate = ep.AllowPrivate
				data.FormSecretSet = ep.Secret != ""
				data.FormEnabled = ep.Enabled
			}
		}
	}

	filterStatus := strings.TrimSpace(r.URL.Query().Get("status"))
	if !validDeliveryStatus(filterStatus) {
		filterStatus = ""
	}
	data.FilterStatus = filterStatus
	data.FilterEndpoint = strings.TrimSpace(r.URL.Query().Get("endpoint"))
	filter := store.WebhookDeliveryFilter{
		Status:  filterStatus,
		Page:    adminFormInt(r, "page", 1),
		PerPage: webhookDeliveryPerPage,
	}
	if data.FilterEndpoint != "" {
		if id, err := parseID(data.FilterEndpoint); err == nil {
			filter.EndpointID = &id
		}
	}
	page, err := a.store.ListWebhookDeliveries(r.Context(), filter)
	if err != nil {
		return webhookPageData{}, err
	}
	data.DeliveryTotal = page.Total
	data.DeliveryPage = page.Page
	data.DeliveryPages = page.Pages
	data.PerPage = page.PerPage
	for _, delivery := range page.Deliveries {
		label := "Legacy"
		if delivery.EndpointID != nil {
			if name := names[*delivery.EndpointID]; name != "" {
				label = name
			} else {
				label = fmt.Sprintf("Endpoint #%d", *delivery.EndpointID)
			}
		}
		data.Deliveries = append(data.Deliveries, webhookDeliveryRow{
			WebhookDelivery: delivery,
			EndpointLabel:   label,
			CanRetry:        delivery.Status == store.WebhookStatusFailed,
		})
	}
	return data, nil
}

func validDeliveryStatus(status string) bool {
	switch status {
	case store.WebhookStatusPending, store.WebhookStatusDelivered,
		store.WebhookStatusFailed, store.WebhookStatusCancelled:
		return true
	}
	return false
}

func webhookFlash(r *http.Request) string {
	q := r.URL.Query()
	switch {
	case q.Get("saved") == "1":
		return "Webhook endpoint saved."
	case q.Get("toggled") == "1":
		return "Webhook endpoint updated."
	case q.Get("deleted") == "1":
		return "Webhook endpoint deleted."
	case q.Get("retried") == "1":
		return "Delivery queued for a retry."
	}
	if n := q.Get("pruned"); n != "" {
		if count, err := strconv.Atoi(n); err == nil && count >= 0 {
			return fmt.Sprintf("%d delivery %s pruned.", count, plural(count, "record", "records"))
		}
	}
	return ""
}

// parseWebhookForm reads the shared create/edit form. A blank secret means
// unchanged on edit; clear_secret removes it.
func parseWebhookForm(r *http.Request, existing *store.WebhookEndpoint) store.WebhookEndpoint {
	ep := store.WebhookEndpoint{
		Enabled: true,
		Name:    strings.TrimSpace(r.FormValue("name")),
		URL:     strings.TrimSpace(r.FormValue("url")),
		Preset:  strings.TrimSpace(r.FormValue("preset")),
		Events:  r.Form["events"],
	}
	if existing != nil {
		ep = *existing
		ep.Name = strings.TrimSpace(r.FormValue("name"))
		ep.URL = strings.TrimSpace(r.FormValue("url"))
		ep.Preset = strings.TrimSpace(r.FormValue("preset"))
		ep.Events = r.Form["events"]
	}
	if existing == nil || r.FormValue("clear_secret") != "" {
		ep.Secret = strings.TrimSpace(r.FormValue("secret"))
	} else if secret := strings.TrimSpace(r.FormValue("secret")); secret != "" {
		ep.Secret = secret
	}
	return ep
}

// prepareWebhookEndpoint applies the owner-only private-target rule, validates
// the form and the target address, and returns a 422 message on failure.
func (a *Admin) prepareWebhookEndpoint(r *http.Request, ep store.WebhookEndpoint) (store.WebhookEndpoint, string) {
	owner := stateFrom(r).Admin.IsOwner()
	if owner {
		ep.AllowPrivate = r.FormValue("allow_private") != ""
	}
	var err error
	if ep, err = store.CheckWebhookEndpoint(ep); err != nil {
		return store.WebhookEndpoint{}, capitalize(err.Error())
	}
	if err := webhook.ValidateTarget(r.Context(), ep.URL, ep.AllowPrivate); err != nil {
		if errors.Is(err, webhook.ErrTargetDenied) {
			return store.WebhookEndpoint{}, "That host is a private, loopback or otherwise reserved address. An owner can allow private targets."
		}
		return store.WebhookEndpoint{}, capitalize(err.Error())
	}
	return ep, ""
}

func (a *Admin) handleWebhookCreate(rw http.ResponseWriter, r *http.Request) {
	ep, message := a.prepareWebhookEndpoint(r, parseWebhookForm(r, nil))
	if message == "" {
		created, err := a.store.CreateWebhookEndpoint(r.Context(), ep)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrWebhookEndpointLimit), errors.Is(err, store.ErrWebhookNameTaken):
				message = capitalize(err.Error())
			default:
				a.internalError(rw, err)
				return
			}
		} else {
			a.reloadWebhooks(r)
			a.auditAs(r, "webhook.endpoint.create", strconv.FormatInt(created.ID, 10),
				"host="+webhook.TargetHost(created.URL)+" changed: "+webhookFieldList(created))
			http.Redirect(rw, r, "/admin/webhooks?saved=1", http.StatusSeeOther)
			return
		}
	}
	data, err := a.webhookPage(r, message, "")
	if err != nil {
		a.internalError(rw, err)
		return
	}
	data.FormName = ep.Name
	data.FormURL = ep.URL
	data.FormPreset = ep.Preset
	data.FormEvents = eventSet(ep.Events)
	data.FormAllowPrivate = ep.AllowPrivate
	a.render(rw, http.StatusUnprocessableEntity, "admin-webhooks", data)
}

func (a *Admin) handleWebhookUpdate(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid endpoint id", http.StatusBadRequest)
		return
	}
	existing, err := a.store.GetWebhookEndpoint(r.Context(), id)
	if errors.Is(err, store.ErrWebhookEndpointNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}
	ep, message := a.prepareWebhookEndpoint(r, parseWebhookForm(r, &existing))
	if message == "" {
		updated, updateErr := a.store.UpdateWebhookEndpoint(r.Context(), ep)
		if updateErr != nil {
			switch {
			case errors.Is(updateErr, store.ErrWebhookNameTaken):
				message = capitalize(updateErr.Error())
			default:
				a.internalError(rw, updateErr)
				return
			}
		} else {
			a.reloadWebhooks(r)
			a.auditAs(r, "webhook.endpoint.update", strconv.FormatInt(updated.ID, 10),
				"host="+webhook.TargetHost(updated.URL)+" changed: "+webhookFieldDiff(existing, updated))
			http.Redirect(rw, r, "/admin/webhooks?saved=1", http.StatusSeeOther)
			return
		}
	}
	data, err := a.webhookPage(r, message, "")
	if err != nil {
		a.internalError(rw, err)
		return
	}
	data.EditID = id
	data.FormTitle = "Edit " + existing.Name
	data.FormAction = fmt.Sprintf("/admin/webhooks/%d", id)
	data.FormName = ep.Name
	data.FormURL = ep.URL
	data.FormPreset = ep.Preset
	data.FormEvents = eventSet(ep.Events)
	data.FormAllowPrivate = existing.AllowPrivate
	data.FormSecretSet = existing.Secret != ""
	data.FormEnabled = existing.Enabled
	a.render(rw, http.StatusUnprocessableEntity, "admin-webhooks", data)
}

func (a *Admin) handleWebhookToggle(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid endpoint id", http.StatusBadRequest)
		return
	}
	existing, err := a.store.GetWebhookEndpoint(r.Context(), id)
	if errors.Is(err, store.ErrWebhookEndpointNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}
	updated, err := a.store.SetWebhookEndpointEnabled(r.Context(), id, !existing.Enabled)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.reloadWebhooks(r)
	action := "webhook.endpoint.disable"
	if updated.Enabled {
		action = "webhook.endpoint.enable"
	}
	a.auditAs(r, action, strconv.FormatInt(id, 10), "host="+webhook.TargetHost(updated.URL))
	http.Redirect(rw, r, "/admin/webhooks?toggled=1", http.StatusSeeOther)
}

func (a *Admin) handleWebhookDelete(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid endpoint id", http.StatusBadRequest)
		return
	}
	existing, err := a.store.GetWebhookEndpoint(r.Context(), id)
	if errors.Is(err, store.ErrWebhookEndpointNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if err := a.store.DeleteWebhookEndpoint(r.Context(), id); err != nil {
		a.internalError(rw, err)
		return
	}
	a.reloadWebhooks(r)
	a.auditAs(r, "webhook.endpoint.delete", strconv.FormatInt(id, 10),
		"host="+webhook.TargetHost(existing.URL))
	http.Redirect(rw, r, "/admin/webhooks?deleted=1", http.StatusSeeOther)
}

func (a *Admin) handleWebhookTest(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid endpoint id", http.StatusBadRequest)
		return
	}
	endpoint, err := a.store.GetWebhookEndpoint(r.Context(), id)
	if errors.Is(err, store.ErrWebhookEndpointNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}

	entry := store.Entry{
		Word:      "test",
		Meaning:   "webhook test",
		CreatedAt: time.Now().UTC(),
	}
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
	target := webhook.Endpoint{
		ID:           endpoint.ID,
		Name:         endpoint.Name,
		URL:          endpoint.URL,
		Secret:       endpoint.Secret,
		Preset:       endpoint.Preset,
		AllowPrivate: endpoint.AllowPrivate,
	}
	status, sendErr := a.hooks.SendTest(r.Context(), target, payload)
	a.auditAs(r, "webhook.test", strconv.FormatInt(id, 10), "host="+webhook.TargetHost(endpoint.URL))

	result := fmt.Sprintf("Test delivery returned HTTP %d.", status)
	if sendErr != nil {
		result = "Test delivery failed: " + webhook.SanitiseError(sendErr, endpoint.URL)
	}
	data, pageErr := a.webhookPage(r, "", result)
	if pageErr != nil {
		a.internalError(rw, pageErr)
		return
	}
	a.render(rw, http.StatusOK, "admin-webhooks", data)
}

func (a *Admin) handleWebhookAdopt(rw http.ResponseWriter, r *http.Request) {
	legacy, ok := a.hooks.LegacyEndpoint()
	if !ok {
		a.renderWebhookError(rw, r, "No legacy webhook is configured to adopt.")
		return
	}
	owner := stateFrom(r).Admin.IsOwner()
	ep := store.WebhookEndpoint{
		Name:         "Environment default",
		URL:          legacy.URL,
		Secret:       legacy.Secret,
		Enabled:      true,
		Events:       []string{store.WebhookEventCreated},
		Preset:       store.WebhookPresetGeneric,
		AllowPrivate: owner,
	}
	ep, message := a.prepareWebhookEndpointValue(r.Context(), ep)
	if message != "" {
		a.renderWebhookError(rw, r, message)
		return
	}
	created, err := a.store.CreateWebhookEndpoint(r.Context(), ep)
	if err != nil {
		if errors.Is(err, store.ErrWebhookNameTaken) {
			a.renderWebhookError(rw, r, "An endpoint named \"Environment default\" already exists; edit it instead.")
			return
		}
		if errors.Is(err, store.ErrWebhookEndpointLimit) {
			a.renderWebhookError(rw, r, capitalize(err.Error()))
			return
		}
		a.internalError(rw, err)
		return
	}
	a.reloadWebhooks(r)
	a.auditAs(r, "webhook.endpoint.adopt", strconv.FormatInt(created.ID, 10),
		"host="+webhook.TargetHost(created.URL))
	http.Redirect(rw, r, "/admin/webhooks?saved=1", http.StatusSeeOther)
}

// prepareWebhookEndpointValue is prepareWebhookEndpoint for a synthetic form
// value: allow_private is already decided and only validation remains.
func (a *Admin) prepareWebhookEndpointValue(ctx context.Context, ep store.WebhookEndpoint) (store.WebhookEndpoint, string) {
	normalized, err := store.CheckWebhookEndpoint(ep)
	if err != nil {
		return store.WebhookEndpoint{}, capitalize(err.Error())
	}
	if err := webhook.ValidateTarget(ctx, normalized.URL, normalized.AllowPrivate); err != nil {
		if errors.Is(err, webhook.ErrTargetDenied) {
			return store.WebhookEndpoint{}, "That host is a private, loopback or otherwise reserved address. An owner can allow private targets."
		}
		return store.WebhookEndpoint{}, capitalize(err.Error())
	}
	return normalized, ""
}

func (a *Admin) handleWebhookDeliveryRetry(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid delivery id", http.StatusBadRequest)
		return
	}
	if err := a.store.RetryWebhookDeliveryNow(r.Context(), id, time.Now().UTC()); err != nil {
		switch {
		case errors.Is(err, store.ErrWebhookDeliveryNotFound):
			http.NotFound(rw, r)
		case errors.Is(err, store.ErrWebhookDeliveryNotRetryable):
			a.renderWebhookError(rw, r, "Only a failed delivery can be retried.")
		default:
			a.internalError(rw, err)
		}
		return
	}
	a.auditAs(r, "webhook.delivery.retry", strconv.FormatInt(id, 10), "queued")
	http.Redirect(rw, r, "/admin/webhooks?retried=1", http.StatusSeeOther)
}

func (a *Admin) handleWebhookPrune(rw http.ResponseWriter, r *http.Request) {
	n, err := a.hooks.PruneNow(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.auditAs(r, "webhook.prune", "webhook_deliveries", fmt.Sprintf("deleted=%d", n))
	http.Redirect(rw, r, fmt.Sprintf("/admin/webhooks?pruned=%d", n), http.StatusSeeOther)
}

func (a *Admin) renderWebhookError(rw http.ResponseWriter, r *http.Request, message string) {
	data, err := a.webhookPage(r, message, "")
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.render(rw, http.StatusUnprocessableEntity, "admin-webhooks", data)
}

// reloadWebhooks rebuilds the shared endpoint snapshot after a mutation. A
// failure is logged and the retry tick reloads within ten seconds.
func (a *Admin) reloadWebhooks(r *http.Request) {
	if err := a.hooks.Reload(r.Context()); err != nil {
		a.logger.Warn("reload webhook endpoints", "error", err)
	}
}

func maskWebhookURL(raw string) (host, masked string) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", "—"
	}
	return parsed.Host, parsed.Scheme + "://" + parsed.Host + "/…"
}

func eventSet(events []string) map[string]bool {
	set := make(map[string]bool, len(events))
	for _, event := range events {
		set[event] = true
	}
	return set
}

// webhookFieldList names the fields present on a newly created endpoint.
func webhookFieldList(ep store.WebhookEndpoint) string {
	fields := []string{"name", "url", "events", "preset"}
	if ep.Secret != "" {
		fields = append(fields, "secret")
	}
	if ep.AllowPrivate {
		fields = append(fields, "allow_private")
	}
	return strings.Join(fields, ",")
}

// webhookFieldDiff names the fields that changed without recording any value.
func webhookFieldDiff(previous, updated store.WebhookEndpoint) string {
	var fields []string
	if previous.Name != updated.Name {
		fields = append(fields, "name")
	}
	if previous.URL != updated.URL {
		fields = append(fields, "url")
	}
	if previous.Secret != updated.Secret {
		fields = append(fields, "secret")
	}
	if previous.Enabled != updated.Enabled {
		fields = append(fields, "enabled")
	}
	if strings.Join(previous.Events, ",") != strings.Join(updated.Events, ",") {
		fields = append(fields, "events")
	}
	if previous.Preset != updated.Preset {
		fields = append(fields, "preset")
	}
	if previous.AllowPrivate != updated.AllowPrivate {
		fields = append(fields, "allow_private")
	}
	if len(fields) == 0 {
		return "no change"
	}
	return strings.Join(fields, ",")
}
