package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

func TestBulkDeleteEmitsOneEventPerChangedRow(t *testing.T) {
	srv, client, st, engine := newAdminTestServerWithEngine(t, testConfig())
	csrf := setupAdmin(t, srv, client)
	clearAdminEntries(t, st)

	var hits atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	endpoint, err := st.CreateWebhookEndpoint(t.Context(), store.WebhookEndpoint{
		Name: "Bulk", URL: receiver.URL, Events: []string{store.WebhookEventDeleted},
		Preset: store.WebhookPresetGeneric, AllowPrivate: true,
	})
	if err != nil {
		t.Fatalf("CreateWebhookEndpoint: %v", err)
	}
	if err := engine.Reload(t.Context()); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	form := url.Values{"csrf": {csrf}}
	for i := 0; i < 100; i++ {
		entry, err := st.CreateEntry(t.Context(), fmt.Sprintf("bulk-%03d", i), fmt.Sprintf("row %d", i), "", nil)
		if err != nil {
			t.Fatalf("CreateEntry %d: %v", i, err)
		}
		form.Add("ids", fmt.Sprintf("%d", entry.ID))
	}
	if status, _ := postFormRead(t, client, srv.URL+"/admin/entries/delete", form); status != http.StatusSeeOther {
		t.Fatalf("bulk delete = %d, want 303", status)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && hits.Load() < 100 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := hits.Load(); got != 100 {
		t.Fatalf("receiver hits = %d, want 100", got)
	}
	page, err := st.ListWebhookDeliveries(t.Context(), store.WebhookDeliveryFilter{EndpointID: &endpoint.ID})
	if err != nil || page.Total != 100 {
		t.Fatalf("delivery rows = %d (%v), want 100", page.Total, err)
	}
}

func TestImportEmitsNoWebhookEvents(t *testing.T) {
	srv, client, st, engine := newAdminTestServerWithEngine(t, testConfig())
	csrf := setupAdmin(t, srv, client)
	clearAdminEntries(t, st)

	var hits atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	if _, err := st.CreateWebhookEndpoint(t.Context(), store.WebhookEndpoint{
		Name: "Import", URL: receiver.URL,
		Events: []string{store.WebhookEventCreated, store.WebhookEventUpdated},
		Preset: store.WebhookPresetGeneric, AllowPrivate: true,
	}); err != nil {
		t.Fatalf("CreateWebhookEndpoint: %v", err)
	}
	if err := engine.Reload(t.Context()); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	payload := importEnvelopeJSON(t, []store.ExportRow{
		{ID: 9001, Word: "imported", Meaning: "reconciled"},
		{ID: 9002, Word: "also-imported", Meaning: "reconciled too"},
	})
	resp, body := uploadImport(t, client, srv.URL+"/admin/import", csrf, "export.json", payload, "apply", "skip", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import = %d: %s", resp.StatusCode, body)
	}
	// ApplyImport is a reconciliation action, not an interactive change.
	time.Sleep(300 * time.Millisecond)
	if got := hits.Load(); got != 0 {
		t.Fatalf("import emitted %d webhook events, want 0", got)
	}
	page, err := st.ListWebhookDeliveries(t.Context(), store.WebhookDeliveryFilter{})
	if err != nil || page.Total != 0 {
		t.Fatalf("delivery rows after import = %d (%v), want 0", page.Total, err)
	}
	if count, err := st.CountEntries(t.Context()); err != nil || count != 2 {
		t.Fatalf("entries after import = %d (%v), want 2", count, err)
	}
}

func TestEntryChangesDiff(t *testing.T) {
	previous := store.Entry{
		ID: 1, Word: "oud", Meaning: "old", Person: "Alice",
		CreatedAt:   time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
		PhotoSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	updated := previous
	changes, changed := entryChanges(previous, updated)
	if len(changes) != 0 || len(changed) != 0 {
		t.Fatalf("no-op diff = %+v %v", changes, changed)
	}

	updated.Meaning = "new"
	updated.PhotoSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	changes, changed = entryChanges(previous, updated)
	if len(changes) != 2 || len(changed) != 2 {
		t.Fatalf("diff = %+v %v", changes, changed)
	}
	if got := changes["meaning"]; got.From != "old" || got.To != "new" {
		t.Fatalf("meaning change = %+v", got)
	}
	if got := changes["photo"]; got.From != "aaaaaaaaaaaa" || got.To != "bbbbbbbbbbbb" {
		t.Fatalf("photo change = %+v, want 12-character hashes", got)
	}

	updated.PhotoSHA256 = ""
	changes, _ = entryChanges(previous, updated)
	if got := changes["photo"]; got.To != nil {
		t.Fatalf("removed photo change = %+v, want a null to", got)
	}
}

func TestWebhookAllowPrivateIsOwnerOnlyOnCreate(t *testing.T) {
	srv, owner, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	setupAdmin(t, srv, owner)
	createLocalAdmin(t, st, "editor", "editor password 1", store.RoleEditor)
	editor := newAdminClient()
	editorCSRF := loginAs(t, srv, editor, "editor", "editor password 1")

	form := url.Values{
		"csrf":          {editorCSRF},
		"name":          {"Editor target"},
		"url":           {"https://ledger.invalid/editor-hook"},
		"preset":        {"generic"},
		"events":        {store.WebhookEventCreated},
		"allow_private": {"1"},
	}
	if status, body := postFormRead(t, editor, srv.URL+"/admin/webhooks", form); status != http.StatusSeeOther {
		t.Fatalf("editor create = %d: %.300s", status, body)
	}
	endpoints, err := st.ListWebhookEndpoints(t.Context())
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints = %d (%v)", len(endpoints), err)
	}
	if endpoints[0].AllowPrivate {
		t.Fatal("an editor enabled allow_private on create")
	}
}
