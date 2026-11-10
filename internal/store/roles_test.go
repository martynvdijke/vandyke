package store

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// applyMigrationsBefore creates a pre-011 database and inserts one admin row so
// the role migration can be observed upgrading it.
func applyMigrationsBefore(t *testing.T, path, stopAfter string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
	)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(names)
	for _, name := range names {
		version := filepath.Base(name)
		if version > stopAfter {
			break
		}
		body, err := migrationsFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", version, err)
		}
		for _, stmt := range splitStatements(string(body)) {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("apply %s: %v", version, err)
			}
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			t.Fatalf("record %s: %v", version, err)
		}
	}
}

func TestOIDCFirstLoginProvisionsOwner(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	first, err := st.UpsertOIDCAdmin(ctx, "https://id.example.com", "sub-1", "a@example.com", "Alice")
	if err != nil {
		t.Fatalf("UpsertOIDCAdmin: %v", err)
	}
	if first.Role != RoleOwner {
		t.Fatalf("first OIDC admin role = %q, want owner", first.Role)
	}
	second, err := st.UpsertOIDCAdmin(ctx, "https://id.example.com", "sub-2", "b@example.com", "Bob")
	if err != nil {
		t.Fatalf("second UpsertOIDCAdmin: %v", err)
	}
	if second.Role != RoleEditor {
		t.Fatalf("later OIDC admin role = %q, want editor", second.Role)
	}
}

func TestAdminRolesMigrationPromotesExistingRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "migration.db")
	applyMigrationsBefore(t, path, "010_search_person.sql")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := raw.ExecContext(ctx,
		`INSERT INTO admins (username, password_hash) VALUES ('legacy', 'hash')`); err != nil {
		t.Fatalf("insert legacy admin: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open with migrations: %v", err)
	}
	defer st.Close()

	legacy, err := st.GetAdminByUsername(ctx, "legacy")
	if err != nil {
		t.Fatalf("GetAdminByUsername: %v", err)
	}
	if legacy.Role != RoleOwner {
		t.Fatalf("pre-existing admin role = %q, want owner", legacy.Role)
	}
	if legacy.IsDisabled() {
		t.Fatal("pre-existing admin should be enabled")
	}

	// New local admins default to the least-privileged role.
	created, err := st.CreateAdmin(ctx, "fresh", "", "hash", RoleEditor)
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if created.Role != RoleEditor {
		t.Fatalf("new admin role = %q, want editor", created.Role)
	}
}

func TestAdminRoleAndLastOwnerInvariant(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.CreateFirstAdmin(ctx, "owner", "hash")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	if owner.Role != RoleOwner {
		t.Fatalf("first admin role = %q, want owner", owner.Role)
	}
	editor, err := st.CreateAdmin(ctx, "editor", "Editor", "hash", RoleEditor)
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	if _, err := st.SetAdminRole(ctx, owner.ID, RoleEditor); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("demote only owner = %v, want ErrLastOwner", err)
	}
	if err := st.DisableAdmin(ctx, owner.ID, time.Now()); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("disable only owner = %v, want ErrLastOwner", err)
	}
	if err := st.DeleteAdmin(ctx, owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("delete only owner = %v, want ErrLastOwner", err)
	}
	if count, err := st.CountEnabledOwners(ctx); err != nil || count != 1 {
		t.Fatalf("CountEnabledOwners = %d, %v; want 1", count, err)
	}

	promoted, err := st.SetAdminRole(ctx, editor.ID, RoleOwner)
	if err != nil {
		t.Fatalf("promote editor: %v", err)
	}
	if promoted.Role != RoleOwner {
		t.Fatalf("promoted role = %q", promoted.Role)
	}
	// With two enabled owners, removing one succeeds.
	demoted, err := st.SetAdminRole(ctx, owner.ID, RoleEditor)
	if err != nil {
		t.Fatalf("demote with second owner: %v", err)
	}
	if demoted.Role != RoleEditor {
		t.Fatalf("demoted role = %q", demoted.Role)
	}
	if count, err := st.CountEnabledOwners(ctx); err != nil || count != 1 {
		t.Fatalf("owner count after demotion = %d, %v; want 1", count, err)
	}
}

func TestAdminCreateDisableLoginLookupAndDeleteCascades(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if _, err := st.CreateFirstAdmin(ctx, "owner", "hash"); err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	admin, err := st.CreateAdmin(ctx, "Editor", "Ed", "hash", RoleEditor)
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if admin.Username != "editor" || admin.DisplayName != "Ed" {
		t.Fatalf("created admin = %+v", admin)
	}
	if _, err := st.CreateAdmin(ctx, "editor", "", "hash", RoleEditor); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("duplicate CreateAdmin = %v, want ErrAdminExists", err)
	}

	now := time.Now().UTC()
	if err := st.CreateSession(ctx, Session{
		TokenHash: "editor-token", AdminID: admin.ID, CSRFToken: "csrf",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := st.DisableAdmin(ctx, admin.ID, now); err != nil {
		t.Fatalf("DisableAdmin: %v", err)
	}
	if _, err := st.GetAdminByUsername(ctx, "editor"); !errors.Is(err, ErrAdminNotFound) {
		t.Fatalf("disabled login lookup = %v, want ErrAdminNotFound", err)
	}
	if _, err := st.GetSession(ctx, "editor-token"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("disabled session = %v, want ErrSessionNotFound", err)
	}
	disabled, err := st.GetAdmin(ctx, admin.ID)
	if err != nil || !disabled.IsDisabled() {
		t.Fatalf("GetAdmin disabled = %+v, %v", disabled, err)
	}

	if err := st.EnableAdmin(ctx, admin.ID); err != nil {
		t.Fatalf("EnableAdmin: %v", err)
	}
	if _, err := st.GetAdminByUsername(ctx, "editor"); err != nil {
		t.Fatalf("enabled login lookup: %v", err)
	}

	if err := st.DeleteAdmin(ctx, admin.ID); err != nil {
		t.Fatalf("DeleteAdmin: %v", err)
	}
	if _, err := st.GetAdmin(ctx, admin.ID); !errors.Is(err, ErrAdminNotFound) {
		t.Fatalf("GetAdmin after delete = %v", err)
	}
	if _, err := st.GetSession(ctx, "editor-token"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session after admin delete = %v, want cascade", err)
	}
}

func TestSetAdminPasswordRevokesSessions(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	admin, err := st.CreateFirstAdmin(ctx, "owner", "old-hash")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	now := time.Now().UTC()
	for _, token := range []string{"keep", "drop"} {
		if err := st.CreateSession(ctx, Session{
			TokenHash: token, AdminID: admin.ID, CSRFToken: "csrf",
			CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("CreateSession %s: %v", token, err)
		}
	}
	if err := st.SetAdminPassword(ctx, admin.ID, "new-hash", "keep"); err != nil {
		t.Fatalf("SetAdminPassword: %v", err)
	}
	if _, err := st.GetSession(ctx, "keep"); err != nil {
		t.Fatalf("kept session: %v", err)
	}
	if _, err := st.GetSession(ctx, "drop"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("sibling session = %v, want revoked", err)
	}
	updated, err := st.GetAdmin(ctx, admin.ID)
	if err != nil || updated.PasswordHash != "new-hash" {
		t.Fatalf("hash not replaced: %+v, %v", updated, err)
	}

	// An owner reset with no exception revokes everything.
	if err := st.SetAdminPassword(ctx, admin.ID, "reset-hash", ""); err != nil {
		t.Fatalf("SetAdminPassword all: %v", err)
	}
	if _, err := st.GetSession(ctx, "keep"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("all sessions should be revoked, got %v", err)
	}
}

func TestSessionRefsListingAndScopedRevocation(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	first, err := st.CreateFirstAdmin(ctx, "owner", "hash")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	second, err := st.CreateAdmin(ctx, "editor", "", "hash", RoleEditor)
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	now := time.Now().UTC()
	create := func(token string, adminID int64) {
		t.Helper()
		if err := st.CreateSession(ctx, Session{
			TokenHash: token, AdminID: adminID, CSRFToken: "csrf",
			CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("CreateSession %s: %v", token, err)
		}
	}
	create("mine-a", first.ID)
	create("mine-b", first.ID)
	create("theirs", second.ID)

	sessions, err := st.ListSessionsForAdmin(ctx, first.ID, "mine-a")
	if err != nil {
		t.Fatalf("ListSessionsForAdmin: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("session count = %d, want 2", len(sessions))
	}
	var otherRef string
	for _, s := range sessions {
		if s.Ref == "" {
			t.Fatal("session reference is empty")
		}
	}
	currentCount := 0
	for _, s := range sessions {
		if s.Current {
			currentCount++
		}
	}
	if currentCount != 1 {
		t.Fatalf("current session count = %d, want 1", currentCount)
	}

	info, err := st.GetSession(ctx, "theirs")
	if err != nil {
		t.Fatalf("foreign session: %v", err)
	}
	otherRef = info.Session.SessionRef
	if n, err := st.DeleteSessionByRef(ctx, otherRef, first.ID); err != nil || n != 0 {
		t.Fatalf("scoped foreign delete = %d, %v; want 0", n, err)
	}
	if n, err := st.DeleteSessionByRef(ctx, otherRef, 0); err != nil || n != 1 {
		t.Fatalf("owner delete = %d, %v; want 1", n, err)
	}

	deleted, err := st.DeleteOtherSessionsForAdmin(ctx, first.ID, "mine-a")
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteOtherSessionsForAdmin = %d, %v; want 1", deleted, err)
	}
	if _, err := st.GetSession(ctx, "mine-a"); err != nil {
		t.Fatalf("current session should survive: %v", err)
	}
}

func TestAuditRecordListFiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	actor := int64(7)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i, action := range []string{"login.failure", "login.success", "entry.update"} {
		if err := st.RecordAudit(ctx, AuditRecord{
			ActorID: &actor, ActorName: "owner", Action: action,
			Subject: "subject", Detail: "detail", IP: "127.0.0.1",
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatalf("RecordAudit: %v", err)
		}
	}
	page, err := st.ListAudit(ctx, AuditFilter{PerPage: 2})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if page.Total != 3 || page.Pages != 2 || len(page.Entries) != 2 {
		t.Fatalf("page = %+v", page)
	}
	if page.Entries[0].Action != "entry.update" {
		t.Fatalf("newest first failed: %q", page.Entries[0].Action)
	}
	filtered, err := st.ListAudit(ctx, AuditFilter{Action: "login", Actor: "own", From: "2026-11-01", To: "2026-11-01"})
	if err != nil {
		t.Fatalf("filtered ListAudit: %v", err)
	}
	if filtered.Total != 2 {
		t.Fatalf("filtered total = %d, want 2", filtered.Total)
	}
	for _, e := range filtered.Entries {
		if !strings.HasPrefix(e.Action, "login.") {
			t.Fatalf("filter leaked action %q", e.Action)
		}
	}
}
