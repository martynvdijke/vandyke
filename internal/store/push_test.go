package store

import (
	"context"
	"testing"
)

func TestPushDeviceUpsertDisableDelete(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	first, err := st.UpsertPushDevice(ctx, PushDevice{Token: "  tok-1  ", DeviceName: "Pixel"})
	if err != nil {
		t.Fatalf("UpsertPushDevice: %v", err)
	}
	if first.Token != "tok-1" {
		t.Fatalf("token = %q, want trimmed value", first.Token)
	}
	if first.Platform != "android" {
		t.Fatalf("platform = %q, want android default", first.Platform)
	}
	if !first.NotificationsEnabled || first.DisabledAt != nil {
		t.Fatalf("new device = %+v, want enabled and not disabled", first)
	}
	if first.CreatedAt.IsZero() || first.LastSeenAt.IsZero() {
		t.Fatalf("timestamps not populated: %+v", first)
	}

	// Age last_seen_at so the refresh performed by the second upsert is
	// observable despite the second-resolution timestamp format.
	if _, err := st.db.Exec(
		`UPDATE push_devices SET last_seen_at = '2020-01-01T00:00:00Z' WHERE token = 'tok-1'`); err != nil {
		t.Fatalf("age last_seen_at: %v", err)
	}

	second, err := st.UpsertPushDevice(ctx, PushDevice{
		Token: "tok-1", Platform: "ios", DeviceName: "iPhone", AppVersion: "2.0",
	})
	if err != nil {
		t.Fatalf("UpsertPushDevice second: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("id changed: %d -> %d", first.ID, second.ID)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("created_at changed: %v -> %v", first.CreatedAt, second.CreatedAt)
	}
	if second.LastSeenAt.Year() == 2020 {
		t.Fatalf("last_seen_at was not bumped: %v", second.LastSeenAt)
	}
	if second.Platform != "ios" || second.DeviceName != "iPhone" || second.AppVersion != "2.0" {
		t.Fatalf("refresh did not update fields: %+v", second)
	}

	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM push_devices`).Scan(&count); err != nil {
		t.Fatalf("count devices: %v", err)
	}
	if count != 1 {
		t.Fatalf("device rows = %d, want 1", count)
	}

	devices, err := st.ListActivePushDevices(ctx)
	if err != nil {
		t.Fatalf("ListActivePushDevices: %v", err)
	}
	if len(devices) != 1 || devices[0].Token != "tok-1" {
		t.Fatalf("active devices = %+v", devices)
	}

	if err := st.DisablePushDevices(ctx, []string{"tok-1", "unknown-token"}); err != nil {
		t.Fatalf("DisablePushDevices: %v", err)
	}
	devices, err = st.ListActivePushDevices(ctx)
	if err != nil {
		t.Fatalf("ListActivePushDevices after disable: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("disabled device still active: %+v", devices)
	}

	// An empty slice is a no-op, not an error.
	if err := st.DisablePushDevices(ctx, nil); err != nil {
		t.Fatalf("DisablePushDevices nil: %v", err)
	}

	// Upserting the disabled token re-enables it.
	reenabled, err := st.UpsertPushDevice(ctx, PushDevice{Token: "tok-1"})
	if err != nil {
		t.Fatalf("UpsertPushDevice re-enable: %v", err)
	}
	if !reenabled.NotificationsEnabled || reenabled.DisabledAt != nil {
		t.Fatalf("re-enabled device = %+v", reenabled)
	}

	if err := st.DeletePushDevice(ctx, "tok-1"); err != nil {
		t.Fatalf("DeletePushDevice: %v", err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM push_devices`).Scan(&count); err != nil {
		t.Fatalf("count devices after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("device rows after delete = %d, want 0", count)
	}
}

func TestPushDeviceRequiresToken(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.UpsertPushDevice(context.Background(), PushDevice{Token: "   "}); err == nil {
		t.Fatal("empty token accepted")
	}
}

func TestWebPushSubscriptionLifecycle(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	admin, err := st.CreateAdmin(ctx, "pushadmin", "Push Admin", "hash", RoleOwner)
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	sub, err := st.UpsertWebPushSubscription(ctx, WebPushSubscription{
		Endpoint:  " https://push.example.com/one ",
		P256DH:    " p256 ",
		Auth:      " auth ",
		UserAgent: "test-agent",
		AdminID:   &admin.ID,
	})
	if err != nil {
		t.Fatalf("UpsertWebPushSubscription: %v", err)
	}
	if sub.Endpoint != "https://push.example.com/one" || sub.P256DH != "p256" || sub.Auth != "auth" {
		t.Fatalf("subscription = %+v", sub)
	}
	if sub.AdminID == nil || *sub.AdminID != admin.ID {
		t.Fatalf("admin_id = %v, want %d", sub.AdminID, admin.ID)
	}
	if !sub.NotificationsEnabled || sub.DisabledAt != nil {
		t.Fatalf("new subscription = %+v", sub)
	}

	// A refresh without an admin keeps the adopted admin, and the endpoint is
	// unique so only one row remains.
	refreshed, err := st.UpsertWebPushSubscription(ctx, WebPushSubscription{
		Endpoint: "https://push.example.com/one",
		P256DH:   "p256-new",
		Auth:     "auth-new",
	})
	if err != nil {
		t.Fatalf("UpsertWebPushSubscription refresh: %v", err)
	}
	if refreshed.ID != sub.ID {
		t.Fatalf("id changed: %d -> %d", sub.ID, refreshed.ID)
	}
	if refreshed.P256DH != "p256-new" || refreshed.Auth != "auth-new" {
		t.Fatalf("refresh did not update keys: %+v", refreshed)
	}
	if refreshed.AdminID == nil || *refreshed.AdminID != admin.ID {
		t.Fatalf("refresh lost admin_id: %v", refreshed.AdminID)
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM web_push_subscriptions`).Scan(&count); err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if count != 1 {
		t.Fatalf("subscription rows = %d, want 1", count)
	}

	subs, err := st.ListActiveWebPushSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListActiveWebPushSubscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("active subscriptions = %+v", subs)
	}

	if err := st.DisableWebPushSubscriptions(ctx, []string{"https://push.example.com/one", "https://nope.example.com"}); err != nil {
		t.Fatalf("DisableWebPushSubscriptions: %v", err)
	}
	subs, err = st.ListActiveWebPushSubscriptions(ctx)
	if err != nil {
		t.Fatalf("list after disable: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("disabled subscription still active: %+v", subs)
	}
	if err := st.DisableWebPushSubscriptions(ctx, nil); err != nil {
		t.Fatalf("DisableWebPushSubscriptions nil: %v", err)
	}

	if err := st.DeleteWebPushSubscription(ctx, "https://push.example.com/one"); err != nil {
		t.Fatalf("DeleteWebPushSubscription: %v", err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM web_push_subscriptions`).Scan(&count); err != nil {
		t.Fatalf("count subscriptions after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("subscription rows after delete = %d, want 0", count)
	}
}

func TestAdminNotificationPrefsDefaultsAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	admin, err := st.CreateAdmin(ctx, "prefsadmin", "Prefs Admin", "hash", RoleOwner)
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	// A missing row yields all-true defaults with the requested admin id.
	prefs, err := st.GetAdminNotificationPrefs(ctx, admin.ID)
	if err != nil {
		t.Fatalf("GetAdminNotificationPrefs missing row: %v", err)
	}
	want := AdminNotificationPrefs{AdminID: admin.ID, Realtime: true, WebPush: true, EntryCreated: true}
	if prefs != want {
		t.Fatalf("defaults = %+v, want %+v", prefs, want)
	}

	saved := AdminNotificationPrefs{AdminID: admin.ID, Realtime: false, WebPush: true, EntryCreated: false}
	if err := st.SaveAdminNotificationPrefs(ctx, saved); err != nil {
		t.Fatalf("SaveAdminNotificationPrefs: %v", err)
	}
	got, err := st.GetAdminNotificationPrefs(ctx, admin.ID)
	if err != nil {
		t.Fatalf("GetAdminNotificationPrefs: %v", err)
	}
	if got != saved {
		t.Fatalf("round trip = %+v, want %+v", got, saved)
	}

	// Saving again replaces rather than duplicating the row.
	updated := AdminNotificationPrefs{AdminID: admin.ID, Realtime: true, WebPush: false, EntryCreated: true}
	if err := st.SaveAdminNotificationPrefs(ctx, updated); err != nil {
		t.Fatalf("SaveAdminNotificationPrefs update: %v", err)
	}
	got, err = st.GetAdminNotificationPrefs(ctx, admin.ID)
	if err != nil {
		t.Fatalf("GetAdminNotificationPrefs update: %v", err)
	}
	if got != updated {
		t.Fatalf("updated prefs = %+v, want %+v", got, updated)
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM admin_notification_prefs`).Scan(&count); err != nil {
		t.Fatalf("count prefs: %v", err)
	}
	if count != 1 {
		t.Fatalf("pref rows = %d, want 1", count)
	}
}

func TestPushNotificationMigrationCreatesTables(t *testing.T) {
	st := newTestStore(t)
	for _, table := range []string{"push_devices", "web_push_subscriptions", "admin_notification_prefs"} {
		var name string
		if err := st.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("table %s: %v", table, err)
		}
	}
	var applied int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied != 15 {
		t.Fatalf("applied migrations = %d, want 15", applied)
	}
}
