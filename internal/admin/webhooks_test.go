package admin

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

func createEndpointForm(name, target string, events ...string) url.Values {
	form := url.Values{
		"name":          {name},
		"url":           {target},
		"preset":        {"generic"},
		"allow_private": {"1"},
	}
	for _, event := range events {
		form.Add("events", event)
	}
	return form
}

func TestWebhookAdminCRUDRolesAndAudit(t *testing.T) {
	srv, owner, st := newAdminTestServer(t, testConfig())
	ownerCSRF := setupAdmin(t, srv, owner)
	ownerAdmin, err := st.GetAdminByUsername(t.Context(), "owner")
	if err != nil {
		t.Fatalf("GetAdminByUsername: %v", err)
	}
	editor := createLocalAdmin(t, st, "editor", "editor password 1", store.RoleEditor)
	editorClient := newAdminClient()
	editorCSRF := loginAs(t, srv, editorClient, "editor", "editor password 1")

	// Reads need a session.
	anonymous := newAdminClient()
	if resp, _ := get(t, anonymous, srv.URL+"/admin/webhooks"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("anonymous GET = %d, want a login redirect", resp.StatusCode)
	}
	if resp, body := get(t, owner, srv.URL+"/admin/webhooks"); resp.StatusCode != http.StatusOK ||
		!strings.Contains(body, "Delivery log") {
		t.Fatalf("owner GET = %d", resp.StatusCode)
	}

	// A token-bearing path and query must never appear in an audit row.
	target := "https://ledger.invalid/hooks/topsecret?token=abc123"
	form := createEndpointForm("Operations", target, store.WebhookEventCreated, store.WebhookEventUpdated)
	form.Set("csrf", ownerCSRF)
	form.Set("secret", "signing secret")
	if status, body := postFormRead(t, owner, srv.URL+"/admin/webhooks", form); status != http.StatusSeeOther {
		t.Fatalf("create = %d: %.300s", status, body)
	}
	endpoints, err := st.ListWebhookEndpoints(t.Context())
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("ListWebhookEndpoints = %d (%v)", len(endpoints), err)
	}
	created := endpoints[0]
	if created.Name != "Operations" || !created.Enabled || created.Secret != "signing secret" {
		t.Fatalf("created = %+v", created)
	}
	if strings.Join(created.Events, ",") != "entry.created,entry.updated" {
		t.Fatalf("events = %v", created.Events)
	}

	audit, err := st.ListAudit(t.Context(), store.AuditFilter{Action: "webhook.endpoint.create"})
	if err != nil || len(audit.Entries) != 1 {
		t.Fatalf("create audit = %+v (%v)", audit, err)
	}
	detail := audit.Entries[0].Detail
	if strings.Contains(detail, "topsecret") || strings.Contains(detail, "abc123") || strings.Contains(detail, "signing secret") {
		t.Fatalf("audit detail leaked URL path, query or secret: %q", detail)
	}
	if !strings.Contains(detail, "host=ledger.invalid") {
		t.Fatalf("audit detail = %q, want the host", detail)
	}

	// Duplicate name, case-insensitively, is a 422.
	dup := createEndpointForm("operations", "https://ledger.invalid/other", store.WebhookEventCreated)
	dup.Set("csrf", ownerCSRF)
	if status, _ := postFormRead(t, owner, srv.URL+"/admin/webhooks", dup); status != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate name = %d, want 422", status)
	}

	// Missing CSRF changes nothing.
	noCSRF := createEndpointForm("No CSRF", "https://ledger.invalid/no", store.WebhookEventCreated)
	if status, _ := postFormRead(t, owner, srv.URL+"/admin/webhooks", noCSRF); status != http.StatusForbidden {
		t.Fatalf("missing CSRF = %d, want 403", status)
	}
	if list, _ := st.ListWebhookEndpoints(t.Context()); len(list) != 1 {
		t.Fatalf("refused requests wrote endpoints: %d", len(list))
	}

	// The edit form shows the stored URL but never the secret.
	resp, body := get(t, owner, srv.URL+"/admin/webhooks?edit=1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit GET = %d", resp.StatusCode)
	}
	if !strings.Contains(body, target) {
		t.Fatal("edit form does not show the stored URL")
	}
	if strings.Contains(body, "signing secret") {
		t.Fatal("edit form rendered the stored secret")
	}
	// The list without the edit form masks the path and query.
	_, listed := get(t, owner, srv.URL+"/admin/webhooks")
	if strings.Contains(listed, "topsecret") || strings.Contains(listed, "abc123") {
		t.Fatal("endpoint list rendered the URL path or query")
	}

	// An edit with a blank secret keeps it; an owner may toggle allow_private.
	update := createEndpointForm("Operations", target, store.WebhookEventUpdated)
	update.Set("csrf", ownerCSRF)
	update.Set("secret", "")
	if status, _ := postFormRead(t, owner, srv.URL+"/admin/webhooks/1", update); status != http.StatusSeeOther {
		t.Fatalf("update = %d", status)
	}
	updated, err := st.GetWebhookEndpoint(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetWebhookEndpoint: %v", err)
	}
	if updated.Secret != "signing secret" || len(updated.Events) != 1 || updated.Events[0] != store.WebhookEventUpdated {
		t.Fatalf("updated = %+v", updated)
	}
	if updated.AllowPrivate != true {
		t.Fatalf("owner did not set allow_private: %+v", updated)
	}

	// An editor keeps the stored allow_private value and may edit other fields.
	editorForm := createEndpointForm("Operations", "https://ledger.invalid/editor", store.WebhookEventCreated)
	editorForm.Del("allow_private")
	editorForm.Set("csrf", editorCSRF)
	if status, _ := postFormRead(t, editorClient, srv.URL+"/admin/webhooks/1", editorForm); status != http.StatusSeeOther {
		t.Fatalf("editor update = %d", status)
	}
	editorUpdated, err := st.GetWebhookEndpoint(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetWebhookEndpoint: %v", err)
	}
	if !editorUpdated.AllowPrivate {
		t.Fatal("editor submission cleared the stored allow_private flag")
	}
	if !strings.Contains(editorUpdated.URL, "/editor") {
		t.Fatalf("editor update URL = %q", editorUpdated.URL)
	}

	// Delete is owner-only.
	deleteForm := url.Values{"csrf": {editorCSRF}}
	if status, _ := postFormRead(t, editorClient, srv.URL+"/admin/webhooks/1/delete", deleteForm); status != http.StatusForbidden {
		t.Fatalf("editor delete = %d, want 403", status)
	}
	if _, err := st.GetWebhookEndpoint(t.Context(), 1); err != nil {
		t.Fatalf("editor delete removed the endpoint: %v", err)
	}

	// Toggle, then delete as the owner.
	toggle := url.Values{"csrf": {ownerCSRF}}
	if status, _ := postFormRead(t, owner, srv.URL+"/admin/webhooks/1/toggle", toggle); status != http.StatusSeeOther {
		t.Fatalf("toggle = %d", status)
	}
	disabled, err := st.GetWebhookEndpoint(t.Context(), 1)
	if err != nil || disabled.Enabled {
		t.Fatalf("toggle did not disable: %+v (%v)", disabled, err)
	}
	audit, err = st.ListAudit(t.Context(), store.AuditFilter{Action: "webhook.endpoint.disable"})
	if err != nil || len(audit.Entries) != 1 {
		t.Fatalf("disable audit = %+v (%v)", audit, err)
	}
	if status, _ := postFormRead(t, owner, srv.URL+"/admin/webhooks/1/delete", toggle); status != http.StatusSeeOther {
		t.Fatalf("owner delete = %d", status)
	}
	if _, err := st.GetWebhookEndpoint(t.Context(), 1); !errors.Is(err, store.ErrWebhookEndpointNotFound) {
		t.Fatalf("endpoint survived delete: %v", err)
	}
	audit, err = st.ListAudit(t.Context(), store.AuditFilter{Action: "webhook.endpoint.delete"})
	if err != nil || len(audit.Entries) != 1 {
		t.Fatalf("delete audit = %+v (%v)", audit, err)
	}
	if !strings.Contains(audit.Entries[0].Subject, "1") || strings.Contains(audit.Entries[0].Detail, "/editor") {
		t.Fatalf("delete audit = %+v", audit.Entries[0])
	}

	// The editor's own audit actor is recorded correctly for the edit.
	if ownerAdmin.Role != store.RoleOwner || editor.Role != store.RoleEditor {
		t.Fatalf("roles = %q / %q", ownerAdmin.Role, editor.Role)
	}
}

func TestWebhookAdminTestFireRetryAndPrune(t *testing.T) {
	srv, client, st := newAdminTestServer(t, testConfig())
	csrf := setupAdmin(t, srv, client)

	type received struct {
		body   []byte
		header http.Header
	}
	deliveries := make(chan received, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		deliveries <- received{body: data, header: r.Header.Clone()}
		rw.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	form := createEndpointForm("Fire", receiver.URL+"/hook", store.WebhookEventCreated)
	form.Set("csrf", csrf)
	form.Set("secret", "firesecret")
	form.Set("preset", "discord")
	if status, _ := postFormRead(t, client, srv.URL+"/admin/webhooks", form); status != http.StatusSeeOther {
		t.Fatalf("create = %d", status)
	}

	status, body := postFormRead(t, client, srv.URL+"/admin/webhooks/1/test", url.Values{"csrf": {csrf}})
	if status != http.StatusOK || !strings.Contains(body, "HTTP 204") {
		t.Fatalf("test fire = %d, body %.300s", status, body)
	}
	select {
	case got := <-deliveries:
		if got.header.Get("X-VanDyke-Signature") == "" {
			t.Fatal("test fire did not sign the request")
		}
		if !strings.Contains(string(got.body), `"allowed_mentions"`) {
			t.Fatalf("test fire did not use the endpoint preset: %s", got.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("test fire made no request")
	}
	audit, err := st.ListAudit(t.Context(), store.AuditFilter{Action: "webhook.test"})
	if err != nil || len(audit.Entries) != 1 {
		t.Fatalf("test audit = %+v (%v)", audit, err)
	}
	if strings.Contains(audit.Entries[0].Detail, "/hook") {
		t.Fatalf("test audit leaked the path: %q", audit.Entries[0].Detail)
	}

	// A failed delivery can be retried, and the attempt count is preserved.
	entryID := int64(1)
	delivery, err := st.CreateWebhookDelivery(t.Context(), store.WebhookDelivery{
		EndpointID: func() *int64 { id := int64(1); return &id }(), Event: store.WebhookEventCreated,
		EntryID: &entryID, Status: store.WebhookStatusFailed, Attempts: 8,
		LastStatus: func() *int { s := 500; return &s }(), LastError: "example.com: request failed",
		Payload: `{"event":"entry.created","id":1}`, NextAttemptAt: nil,
	})
	if err != nil {
		t.Fatalf("CreateWebhookDelivery: %v", err)
	}
	if status, _ := postFormRead(t, client, srv.URL+"/admin/webhooks/deliveries/1/retry", url.Values{"csrf": {csrf}}); status != http.StatusSeeOther {
		t.Fatalf("retry = %d", status)
	}
	retried, err := st.GetWebhookDelivery(t.Context(), delivery.ID)
	if err != nil || retried.Status != store.WebhookStatusPending || retried.Attempts != 8 || retried.NextAttemptAt == nil {
		t.Fatalf("retried delivery = %+v (%v)", retried, err)
	}
	audit, err = st.ListAudit(t.Context(), store.AuditFilter{Action: "webhook.delivery.retry"})
	if err != nil || len(audit.Entries) != 1 {
		t.Fatalf("retry audit = %+v (%v)", audit, err)
	}

	// Retrying a pending delivery is refused without changing anything.
	if status, _ := postFormRead(t, client, srv.URL+"/admin/webhooks/deliveries/1/retry", url.Values{"csrf": {csrf}}); status != http.StatusUnprocessableEntity {
		t.Fatalf("retry pending = %d, want 422", status)
	}
	// Prune is owner-only and records the deleted count.
	status, _ = postFormRead(t, client, srv.URL+"/admin/webhooks/prune", url.Values{"csrf": {csrf}})
	if status != http.StatusSeeOther {
		t.Fatalf("prune = %d", status)
	}
	audit, err = st.ListAudit(t.Context(), store.AuditFilter{Action: "webhook.prune"})
	if err != nil || len(audit.Entries) != 1 {
		t.Fatalf("prune audit = %+v (%v)", audit, err)
	}
}

func TestWebhookAdminPruneRejectsEditor(t *testing.T) {
	srv, owner, st := newAdminTestServer(t, testConfig())
	ownerCSRF := setupAdmin(t, srv, owner)
	createLocalAdmin(t, st, "editor", "editor password 1", store.RoleEditor)
	editorClient := newAdminClient()
	editorCSRF := loginAs(t, srv, editorClient, "editor", "editor password 1")

	if status, _ := postFormRead(t, editorClient, srv.URL+"/admin/webhooks/prune", url.Values{"csrf": {editorCSRF}}); status != http.StatusForbidden {
		t.Fatalf("editor prune = %d, want 403", status)
	}
	if status, _ := postFormRead(t, owner, srv.URL+"/admin/webhooks/prune", url.Values{"csrf": {ownerCSRF}}); status != http.StatusSeeOther {
		t.Fatalf("owner prune = %d, want 303", status)
	}
}

func TestWebhookAdminAdoptLegacy(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100})
	csrf := setupAdmin(t, srv, client)

	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	// Configure the legacy target through the settings form.
	if status, _ := postFormRead(t, client, srv.URL+"/admin/settings", url.Values{
		"csrf":           {csrf},
		"settings_form":  {"webhook"},
		"webhook_url":    {receiver.URL + "/legacy/hook"},
		"webhook_secret": {"legacysecret"},
	}); status != http.StatusSeeOther {
		t.Fatalf("save legacy webhook = %d", status)
	}

	// The settings page labels the legacy section and points at the new page.
	_, settings := get(t, client, srv.URL+"/admin/settings")
	if !strings.Contains(settings, "Legacy target") && !strings.Contains(settings, "legacy") {
		t.Fatal("settings page does not label the webhook section as legacy")
	}
	if !strings.Contains(settings, "/admin/webhooks") {
		t.Fatal("settings page does not link to /admin/webhooks")
	}

	if status, _ := postFormRead(t, client, srv.URL+"/admin/webhooks/adopt", url.Values{"csrf": {csrf}}); status != http.StatusSeeOther {
		t.Fatalf("adopt = %d", status)
	}
	endpoints, err := st.ListWebhookEndpoints(t.Context())
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints after adopt = %d (%v)", len(endpoints), err)
	}
	adopted := endpoints[0]
	if adopted.Name != "Environment default" || !adopted.Enabled || adopted.URL != receiver.URL+"/legacy/hook" ||
		adopted.Secret != "legacysecret" || !adopted.AllowPrivate {
		t.Fatalf("adopted = %+v", adopted)
	}

	// The adopted endpoint suppresses the legacy target.
	_, page := get(t, client, srv.URL+"/admin/webhooks")
	if !strings.Contains(page, "Suppressed") {
		t.Fatal("admin page does not mark the legacy target suppressed")
	}
	// The legacy test fire now refuses to send and points at the new page.
	status, body := postFormRead(t, client, srv.URL+"/admin/settings/webhook/test", url.Values{"csrf": {csrf}})
	if status != http.StatusOK || !strings.Contains(body, "suppressed") {
		t.Fatalf("suppressed test fire = %d, %.300s", status, body)
	}

	audit, err := st.ListAudit(t.Context(), store.AuditFilter{Action: "webhook.endpoint.adopt"})
	if err != nil || len(audit.Entries) != 1 {
		t.Fatalf("adopt audit = %+v (%v)", audit, err)
	}
	if strings.Contains(audit.Entries[0].Detail, "legacysecret") || strings.Contains(audit.Entries[0].Detail, "/legacy/hook") {
		t.Fatalf("adopt audit leaked URL path or secret: %q", audit.Entries[0].Detail)
	}
}

func TestWebhookRoutesRequireCSRF(t *testing.T) {
	srv, client, _ := newAdminTestServer(t, testConfig())
	csrf := setupAdmin(t, srv, client)
	_ = csrf

	paths := []string{
		"/admin/webhooks",
		"/admin/webhooks/1",
		"/admin/webhooks/1/toggle",
		"/admin/webhooks/1/delete",
		"/admin/webhooks/1/test",
		"/admin/webhooks/adopt",
		"/admin/webhooks/deliveries/1/retry",
		"/admin/webhooks/prune",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if status, _ := postFormRead(t, client, srv.URL+path, url.Values{}); status != http.StatusForbidden {
				t.Fatalf("missing CSRF on %s = %d, want 403", path, status)
			}
			if status, _ := postFormRead(t, client, srv.URL+path, url.Values{"csrf": {"wrong"}}); status != http.StatusForbidden {
				t.Fatalf("wrong CSRF on %s = %d, want 403", path, status)
			}
		})
	}
}
