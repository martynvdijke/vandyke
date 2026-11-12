package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWebhookMigrationCreatesTablesAndIndexes(t *testing.T) {
	st := newTestStore(t)
	for _, table := range []string{"webhook_endpoints", "webhook_deliveries"} {
		var name string
		err := st.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s: %v", table, err)
		}
	}
	for _, index := range []string{
		"webhook_endpoints_name",
		"webhook_endpoints_enabled",
		"webhook_deliveries_due",
		"webhook_deliveries_endpoint",
		"webhook_deliveries_created_at",
	} {
		var name string
		err := st.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&name)
		if err != nil {
			t.Fatalf("index %s: %v", index, err)
		}
	}
	// Every embedded migration must be recorded on a fresh database.
	var applied int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied != 14 {
		t.Fatalf("applied migrations = %d, want 14", applied)
	}
}

func TestWebhookEndpointCRUD(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	created, err := st.CreateWebhookEndpoint(ctx, WebhookEndpoint{
		Name:   "  Ops  ",
		URL:    "https://example.com/hooks/one?token=secret",
		Secret: " s3cret ",
		Events: []string{WebhookEventUpdated, WebhookEventCreated, WebhookEventCreated},
		Preset: WebhookPresetDiscord,
	})
	if err != nil {
		t.Fatalf("CreateWebhookEndpoint: %v", err)
	}
	if created.Name != "Ops" || created.Secret != "s3cret" || !created.Enabled {
		t.Fatalf("created = %+v", created)
	}
	if got := strings.Join(created.Events, ","); got != "entry.created,entry.updated" {
		t.Fatalf("events = %q, want sorted de-duplicated list", got)
	}

	got, err := st.GetWebhookEndpoint(ctx, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("GetWebhookEndpoint = %+v (%v)", got, err)
	}

	updated, err := st.UpdateWebhookEndpoint(ctx, WebhookEndpoint{
		ID:           created.ID,
		Name:         "Ops two",
		URL:          "https://example.com/hooks/two",
		Secret:       "",
		Enabled:      false,
		Events:       []string{WebhookEventDeleted},
		Preset:       WebhookPresetSlack,
		AllowPrivate: true,
	})
	if err != nil {
		t.Fatalf("UpdateWebhookEndpoint: %v", err)
	}
	if updated.Name != "Ops two" || updated.Enabled || updated.Preset != WebhookPresetSlack || !updated.AllowPrivate {
		t.Fatalf("updated = %+v", updated)
	}

	toggled, err := st.SetWebhookEndpointEnabled(ctx, created.ID, true)
	if err != nil || !toggled.Enabled {
		t.Fatalf("SetWebhookEndpointEnabled = %+v (%v)", toggled, err)
	}

	if err := st.DeleteWebhookEndpoint(ctx, created.ID); err != nil {
		t.Fatalf("DeleteWebhookEndpoint: %v", err)
	}
	if _, err := st.GetWebhookEndpoint(ctx, created.ID); !errors.Is(err, ErrWebhookEndpointNotFound) {
		t.Fatalf("GetWebhookEndpoint after delete = %v", err)
	}
}

func TestWebhookEndpointValidation(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	tests := []struct {
		name string
		ep   WebhookEndpoint
		want string
	}{
		{"empty name", WebhookEndpoint{URL: "https://example.com", Events: []string{WebhookEventCreated}}, "name is required"},
		{"long name", WebhookEndpoint{Name: strings.Repeat("a", MaxWebhookNameLen+1), URL: "https://example.com", Events: []string{WebhookEventCreated}}, "name must be at most"},
		{"empty url", WebhookEndpoint{Name: "x", Events: []string{WebhookEventCreated}}, "URL is required"},
		{"file scheme", WebhookEndpoint{Name: "x", URL: "file:///etc/passwd", Events: []string{WebhookEventCreated}}, "absolute http or https"},
		{"userinfo", WebhookEndpoint{Name: "x", URL: "https://user:pass@example.com", Events: []string{WebhookEventCreated}}, "must not contain credentials"},
		{"unknown event", WebhookEndpoint{Name: "x", URL: "https://example.com", Events: []string{"entry.exploded"}}, "unknown webhook event"},
		{"empty events", WebhookEndpoint{Name: "x", URL: "https://example.com"}, "select at least one event"},
		{"bad preset", WebhookEndpoint{Name: "x", URL: "https://example.com", Events: []string{WebhookEventCreated}, Preset: "teams"}, "preset must be"},
		{"long secret", WebhookEndpoint{Name: "x", URL: "https://example.com", Events: []string{WebhookEventCreated}, Secret: strings.Repeat("s", MaxWebhookSecretLen+1)}, "secret must be at most"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := st.CreateWebhookEndpoint(ctx, tc.ep); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CreateWebhookEndpoint error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWebhookEndpointNameAndCap(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	if _, err := st.CreateWebhookEndpoint(ctx, WebhookEndpoint{
		Name: "Primary", URL: "https://example.com/one", Events: []string{WebhookEventCreated},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.CreateWebhookEndpoint(ctx, WebhookEndpoint{
		Name: "primary", URL: "https://example.com/two", Events: []string{WebhookEventCreated},
	}); !errors.Is(err, ErrWebhookNameTaken) {
		t.Fatalf("duplicate name error = %v, want ErrWebhookNameTaken", err)
	}

	for i := 1; i < MaxWebhookEndpoints; i++ {
		ep := WebhookEndpoint{
			Name:   fmt.Sprintf("endpoint %d", i),
			URL:    fmt.Sprintf("https://example.com/%d", i),
			Events: []string{WebhookEventCreated},
		}
		if _, err := st.CreateWebhookEndpoint(ctx, ep); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if _, err := st.CreateWebhookEndpoint(ctx, WebhookEndpoint{
		Name: "one too many", URL: "https://example.com/over", Events: []string{WebhookEventCreated},
	}); !errors.Is(err, ErrWebhookEndpointLimit) {
		t.Fatalf("21st endpoint error = %v, want ErrWebhookEndpointLimit", err)
	}
	list, err := st.ListWebhookEndpoints(ctx)
	if err != nil || len(list) != MaxWebhookEndpoints {
		t.Fatalf("ListWebhookEndpoints = %d (%v), want %d", len(list), err, MaxWebhookEndpoints)
	}
	enabled, err := st.CountEnabledWebhookEndpoints(ctx)
	if err != nil || enabled != MaxWebhookEndpoints {
		t.Fatalf("CountEnabledWebhookEndpoints = %d (%v)", enabled, err)
	}
}

func newDelivery(entryID int64) WebhookDelivery {
	now := time.Now().UTC()
	return WebhookDelivery{
		Event:         WebhookEventCreated,
		EntryID:       &entryID,
		Status:        WebhookStatusPending,
		NextAttemptAt: &now,
		Payload:       `{"event":"entry.created","id":1}`,
	}
}

func TestWebhookDeliveryStorage(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	ep, err := st.CreateWebhookEndpoint(ctx, WebhookEndpoint{
		Name: "Delivery", URL: "https://example.com/hook", Events: []string{WebhookEventCreated},
	})
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	for i := 0; i < 3; i++ {
		d := newDelivery(int64(100 + i))
		d.EndpointID = &ep.ID
		if _, err := st.CreateWebhookDelivery(ctx, d); err != nil {
			t.Fatalf("CreateWebhookDelivery %d: %v", i, err)
		}
	}
	if _, err := st.CreateWebhookDelivery(ctx, newDelivery(200)); err != nil {
		t.Fatalf("legacy delivery: %v", err)
	}

	page, err := st.ListWebhookDeliveries(ctx, WebhookDeliveryFilter{})
	if err != nil || page.Total != 4 || len(page.Deliveries) != 4 {
		t.Fatalf("all deliveries = %+v (%v), want 4", page, err)
	}
	page, err = st.ListWebhookDeliveries(ctx, WebhookDeliveryFilter{EndpointID: &ep.ID, PerPage: 2})
	if err != nil || page.Total != 3 || len(page.Deliveries) != 2 || page.Pages != 2 {
		t.Fatalf("endpoint paging = %+v (%v)", page, err)
	}
	page, err = st.ListWebhookDeliveries(ctx, WebhookDeliveryFilter{Status: WebhookStatusPending})
	if err != nil || page.Total != 4 {
		t.Fatalf("status filter = %+v (%v)", page, err)
	}

	due, err := st.DueWebhookDeliveries(ctx, time.Now().UTC().Add(time.Second), 100)
	if err != nil || len(due) != 4 {
		t.Fatalf("DueWebhookDeliveries = %d (%v), want 4", len(due), err)
	}
	future, err := st.DueWebhookDeliveries(ctx, time.Now().UTC().Add(-time.Hour), 100)
	if err != nil || len(future) != 0 {
		t.Fatalf("DueWebhookDeliveries past = %d (%v), want 0", len(future), err)
	}

	counts, err := st.WebhookDeliveryCounts(ctx)
	if err != nil || len(counts) == 0 {
		t.Fatalf("WebhookDeliveryCounts = %+v (%v)", counts, err)
	}
}

func TestWebhookDeliveryPayloadRetention(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	ep, err := st.CreateWebhookEndpoint(ctx, WebhookEndpoint{
		Name: "Payload", URL: "https://example.com/hook", Events: []string{WebhookEventCreated},
	})
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	d, err := st.CreateWebhookDelivery(ctx, func() WebhookDelivery {
		delivery := newDelivery(1)
		delivery.EndpointID = &ep.ID
		return delivery
	}())
	if err != nil {
		t.Fatalf("create delivery: %v", err)
	}

	d.Status = WebhookStatusFailed
	d.Attempts = 1
	if err := st.UpdateWebhookDelivery(ctx, d); err != nil {
		t.Fatalf("fail delivery: %v", err)
	}
	kept, err := st.GetWebhookDelivery(ctx, d.ID)
	if err != nil || kept.Payload == "" {
		t.Fatalf("failed payload = %q (%v), want retained", kept.Payload, err)
	}

	kept.Status = WebhookStatusDelivered
	if err := st.UpdateWebhookDelivery(ctx, kept); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	cleared, err := st.GetWebhookDelivery(ctx, d.ID)
	if err != nil || cleared.Payload != "" {
		t.Fatalf("delivered payload = %q (%v), want cleared", cleared.Payload, err)
	}
	if cleared.Status != WebhookStatusDelivered || cleared.Attempts != 1 {
		t.Fatalf("delivered row = %+v", cleared)
	}
}

func TestWebhookRetryNowAndPrune(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	ep, err := st.CreateWebhookEndpoint(ctx, WebhookEndpoint{
		Name: "Retry", URL: "https://example.com/hook", Events: []string{WebhookEventCreated},
	})
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	d, err := st.CreateWebhookDelivery(ctx, func() WebhookDelivery {
		delivery := newDelivery(1)
		delivery.EndpointID = &ep.ID
		return delivery
	}())
	if err != nil {
		t.Fatalf("create delivery: %v", err)
	}
	d.Status = WebhookStatusFailed
	d.Attempts = 8
	if err := st.UpdateWebhookDelivery(ctx, d); err != nil {
		t.Fatalf("fail delivery: %v", err)
	}

	ok, err := st.CanWebhookDeliveryRetry(ctx, d.ID)
	if err != nil || !ok {
		t.Fatalf("CanWebhookDeliveryRetry = %v (%v), want true", ok, err)
	}
	now := time.Now().UTC()
	if err := st.RetryWebhookDeliveryNow(ctx, d.ID, now); err != nil {
		t.Fatalf("RetryWebhookDeliveryNow: %v", err)
	}
	retried, err := st.GetWebhookDelivery(ctx, d.ID)
	if err != nil {
		t.Fatalf("GetWebhookDelivery: %v", err)
	}
	if retried.Status != WebhookStatusPending || retried.Attempts != 8 {
		t.Fatalf("retried = %+v, want pending with attempts unchanged", retried)
	}
	if retried.NextAttemptAt == nil || retried.NextAttemptAt.After(now.Add(2*time.Second)) {
		t.Fatalf("next attempt = %v, want now", retried.NextAttemptAt)
	}
	if err := st.RetryWebhookDeliveryNow(ctx, d.ID, now); !errors.Is(err, ErrWebhookDeliveryNotRetryable) {
		t.Fatalf("retry pending error = %v, want ErrWebhookDeliveryNotRetryable", err)
	}
	if err := st.RetryWebhookDeliveryNow(ctx, 99999, now); !errors.Is(err, ErrWebhookDeliveryNotFound) {
		t.Fatalf("retry missing error = %v, want ErrWebhookDeliveryNotFound", err)
	}

	// Prune cut-offs: delivered 8 days ago, failed 8 days ago, failed 31 days
	// ago, and a pending row that must survive.
	insertAgedDelivery(t, st, ep.ID, WebhookStatusDelivered, now.Add(-8*24*time.Hour))
	insertAgedDelivery(t, st, ep.ID, WebhookStatusFailed, now.Add(-8*24*time.Hour))
	insertAgedDelivery(t, st, ep.ID, WebhookStatusFailed, now.Add(-31*24*time.Hour))
	insertAgedDelivery(t, st, ep.ID, WebhookStatusCancelled, now.Add(-8*24*time.Hour))
	insertAgedDelivery(t, st, ep.ID, WebhookStatusPending, now.Add(-90*24*time.Hour))

	n, err := st.PruneWebhookDeliveries(ctx, now)
	if err != nil || n != 3 {
		t.Fatalf("PruneWebhookDeliveries = %d (%v), want 3", n, err)
	}
	var pending int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'pending'`).Scan(&pending); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 2 {
		t.Fatalf("pending rows = %d, want the retried row plus the old pending row", pending)
	}
}

func insertAgedDelivery(t *testing.T, st *Store, endpointID int64, status string, createdAt time.Time) {
	t.Helper()
	entryID := int64(1)
	if _, err := st.db.Exec(
		`INSERT INTO webhook_deliveries (endpoint_id, event, entry_id, status, payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, '{}', ?, ?)`,
		endpointID, WebhookEventCreated, entryID, status,
		createdAt.UTC().Format(time.RFC3339), createdAt.UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert aged delivery: %v", err)
	}
}

func TestWebhookEndpointDeleteCascadesDeliveries(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	ep, err := st.CreateWebhookEndpoint(ctx, WebhookEndpoint{
		Name: "Cascade", URL: "https://example.com/hook", Events: []string{WebhookEventCreated},
	})
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	d := newDelivery(1)
	d.EndpointID = &ep.ID
	if _, err := st.CreateWebhookDelivery(ctx, d); err != nil {
		t.Fatalf("create delivery: %v", err)
	}
	if err := st.DeleteWebhookEndpoint(ctx, ep.ID); err != nil {
		t.Fatalf("delete endpoint: %v", err)
	}
	var rows int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries WHERE endpoint_id = ?`, ep.ID).Scan(&rows); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if rows != 0 {
		t.Fatalf("deliveries after endpoint delete = %d, want 0", rows)
	}
}

func TestWebhookEndpointDisableCancelsPending(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	ep, err := st.CreateWebhookEndpoint(ctx, WebhookEndpoint{
		Name: "Disable", URL: "https://example.com/hook", Events: []string{WebhookEventCreated},
	})
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	d := newDelivery(1)
	d.EndpointID = &ep.ID
	created, err := st.CreateWebhookDelivery(ctx, d)
	if err != nil {
		t.Fatalf("create delivery: %v", err)
	}
	if _, err := st.SetWebhookEndpointEnabled(ctx, ep.ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := st.GetWebhookDelivery(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetWebhookDelivery: %v", err)
	}
	if got.Status != WebhookStatusCancelled || got.Payload != "" || got.NextAttemptAt != nil {
		t.Fatalf("cancelled delivery = %+v", got)
	}

	// The standalone canceller is idempotent and reports zero afterwards.
	n, err := st.CancelPendingWebhookDeliveries(ctx, ep.ID)
	if err != nil || n != 0 {
		t.Fatalf("CancelPendingWebhookDeliveries = %d (%v), want 0", n, err)
	}
}

func TestPurgeRemovesOnlyTerminalDeliveryHistory(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)
	entry, err := st.CreateEntry(ctx, "purge", "remove", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	ids := make([]int64, 0, 4)
	for i, status := range []string{WebhookStatusDelivered, WebhookStatusCancelled, WebhookStatusPending, WebhookStatusFailed} {
		d := newDelivery(entry.ID)
		d.Event = WebhookEventPurged
		d.Status = status
		if status == WebhookStatusDelivered || status == WebhookStatusCancelled {
			d.Payload = ""
		}
		created, err := st.CreateWebhookDelivery(ctx, d)
		if err != nil {
			t.Fatalf("create delivery %d: %v", i, err)
		}
		ids = append(ids, created.ID)
	}

	if _, err := st.SoftDeleteEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	// A soft delete leaves delivery rows untouched.
	page, err := st.ListWebhookDeliveries(ctx, WebhookDeliveryFilter{})
	if err != nil || page.Total != 4 {
		t.Fatalf("deliveries after soft delete = %d (%v), want 4", page.Total, err)
	}

	if _, err := st.PurgeEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("PurgeEntries: %v", err)
	}
	for i, id := range ids {
		_, err := st.GetWebhookDelivery(ctx, id)
		switch i {
		case 0, 1:
			if !errors.Is(err, ErrWebhookDeliveryNotFound) {
				t.Fatalf("terminal delivery %d survived purge: %v", id, err)
			}
		default:
			if err != nil {
				t.Fatalf("pending/failed delivery %d disappeared: %v", id, err)
			}
		}
	}
}

func TestWebhookLifecycleReturnsChangedRows(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	first, err := st.CreateEntry(ctx, "eerste", "first", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	second, err := st.CreateEntry(ctx, "tweede", "second", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	removed, err := st.SoftDeleteEntries(ctx, []int64{first.ID, first.ID})
	if err != nil || len(removed) != 1 || removed[0].ID != first.ID {
		t.Fatalf("SoftDeleteEntries = %+v (%v)", removed, err)
	}
	if removed[0].DeletedAt == nil {
		t.Fatal("SoftDeleteEntries snapshot has no deleted_at")
	}
	again, err := st.SoftDeleteEntries(ctx, []int64{first.ID})
	if err != nil || len(again) != 0 {
		t.Fatalf("already trashed SoftDeleteEntries = %+v (%v)", again, err)
	}
	restored, err := st.RestoreEntries(ctx, []int64{first.ID, second.ID})
	if err != nil || len(restored) != 1 || restored[0].ID != first.ID {
		t.Fatalf("RestoreEntries = %+v (%v)", restored, err)
	}
	if restored[0].DeletedAt != nil {
		t.Fatalf("RestoreEntries snapshot still carries deleted_at: %+v", restored[0])
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{second.ID}); err != nil {
		t.Fatalf("trash second: %v", err)
	}
	purged, err := st.PurgeEntries(ctx, []int64{second.ID, second.ID})
	if err != nil || len(purged) != 1 || purged[0].ID != second.ID || purged[0].DeletedAt == nil {
		t.Fatalf("PurgeEntries = %+v (%v)", purged, err)
	}
}

func TestUpdateEntryReturnsPreviousSnapshotWithPhotoHash(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(ctx, "oud", "old", "Alice", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	previous, updated, err := st.UpdateEntry(ctx, EntryUpdate{
		ID: entry.ID, Word: "nieuw", Meaning: "new", Person: "Bob",
		CreatedAt: entry.CreatedAt, RemovePhoto: true,
	})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	if previous.Word != "oud" || previous.Meaning != "old" || previous.Person != "Alice" {
		t.Fatalf("previous snapshot = %+v", previous)
	}
	if previous.PhotoSHA256 != photo.SHA256 || previous.PhotoURL == "" {
		t.Fatalf("previous photo = %q url %q", previous.PhotoSHA256, previous.PhotoURL)
	}
	if updated.Word != "nieuw" || updated.Person != "Bob" || updated.PhotoSHA256 != "" {
		t.Fatalf("updated = %+v", updated)
	}

	// A no-op edit returns identical snapshots.
	unchangedPrevious, unchanged, err := st.UpdateEntry(ctx, EntryUpdate{
		ID: entry.ID, Word: updated.Word, Meaning: updated.Meaning,
		Person: updated.Person, CreatedAt: updated.CreatedAt,
	})
	if err != nil {
		t.Fatalf("no-op UpdateEntry: %v", err)
	}
	if unchangedPrevious.Word != unchanged.Word || unchangedPrevious.Meaning != unchanged.Meaning ||
		unchangedPrevious.Person != unchanged.Person || !unchangedPrevious.CreatedAt.Equal(unchanged.CreatedAt) ||
		unchangedPrevious.PhotoSHA256 != unchanged.PhotoSHA256 {
		t.Fatalf("no-op diff: previous %+v updated %+v", unchangedPrevious, unchanged)
	}
}

func TestCheckReady(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	state := st.CheckReady(ctx)
	if !state.Database || !state.Migrations {
		t.Fatalf("fresh store readiness = %+v", state)
	}

	if _, err := st.db.Exec(`DELETE FROM schema_migrations WHERE version = '013_webhook_endpoints.sql'`); err != nil {
		t.Fatalf("delete migration: %v", err)
	}
	state = st.CheckReady(ctx)
	if !state.Database || state.Migrations {
		t.Fatalf("missing migration readiness = %+v", state)
	}

	closed := newTestStore(t)
	if err := closed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	state = closed.CheckReady(ctx)
	if state.Database || state.Migrations {
		t.Fatalf("closed database readiness = %+v", state)
	}
}

func TestMetricsSnapshot(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	if _, err := st.CreateEntry(ctx, "levend", "live", "", photo); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	trashed, err := st.CreateEntry(ctx, "weg", "gone", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{trashed.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.CreateWebhookDelivery(ctx, newDelivery(1)); err != nil {
		t.Fatalf("CreateWebhookDelivery: %v", err)
	}

	got, err := st.MetricsSnapshot(ctx)
	if err != nil {
		t.Fatalf("MetricsSnapshot: %v", err)
	}
	if got.Total != 2 || got.Live != 1 || got.Trashed != 1 || got.WithPhoto != 1 || got.PendingDeliveries != 1 {
		t.Fatalf("MetricsSnapshot = %+v", got)
	}
}
