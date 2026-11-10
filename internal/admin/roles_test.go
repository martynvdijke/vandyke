package admin

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/auth"
	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

var sessionRefPattern = regexp.MustCompile(`<code>([0-9a-f]{32})</code>`)

func testConfig() config.Config {
	return config.Config{AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100}
}

func timeNow() time.Time { return time.Now().UTC() }

func newAdminClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func loginAs(t *testing.T, srv *httptest.Server, client *http.Client, username, password string) string {
	t.Helper()
	resp := postForm(t, client, srv.URL+"/admin/login", url.Values{
		"username": {username},
		"password": {password},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login %s = %d, want 303", username, resp.StatusCode)
	}
	return csrfFrom(t, mustGet(t, client, srv.URL+"/admin"))
}

func createLocalAdmin(t *testing.T, st *store.Store, username, password, role string) store.Admin {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	admin, err := st.CreateAdmin(t.Context(), username, "", hash, role)
	if err != nil {
		t.Fatalf("CreateAdmin %s: %v", username, err)
	}
	return admin
}

func TestSetupProvisionsOwner(t *testing.T) {
	srv, client, st := newAdminTestServer(t, testConfig())
	setupAdmin(t, srv, client)
	owner, err := st.GetAdminByUsername(t.Context(), "owner")
	if err != nil {
		t.Fatalf("GetAdminByUsername: %v", err)
	}
	if owner.Role != store.RoleOwner {
		t.Fatalf("setup admin role = %q, want owner", owner.Role)
	}
}

func TestOwnerOnlyRoutesRejectEditor(t *testing.T) {
	srv, ownerClient, st := newAdminTestServer(t, testConfig())
	ownerCSRF := setupAdmin(t, srv, ownerClient)
	owner, err := st.GetAdminByUsername(t.Context(), "owner")
	if err != nil {
		t.Fatalf("GetAdminByUsername: %v", err)
	}
	editor := createLocalAdmin(t, st, "editor", "editor password 1", store.RoleEditor)
	editorClient := newAdminClient()
	editorCSRF := loginAs(t, srv, editorClient, "editor", "editor password 1")
	entry, err := st.CreateEntry(t.Context(), "editable", "correct", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	for _, path := range []string{"/admin/admins", "/admin/audit"} {
		resp, _ := get(t, editorClient, srv.URL+path)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("editor GET %s = %d, want 403", path, resp.StatusCode)
		}
	}

	posts := []struct {
		path string
		form url.Values
	}{
		{"/admin/admins", url.Values{"username": {"x"}, "password": {"x"}}},
		{fmt.Sprintf("/admin/admins/%d/role", owner.ID), url.Values{"role": {"editor"}}},
		{fmt.Sprintf("/admin/admins/%d/disable", owner.ID), url.Values{}},
		{fmt.Sprintf("/admin/admins/%d/enable", editor.ID), url.Values{}},
		{fmt.Sprintf("/admin/admins/%d/delete", editor.ID), url.Values{}},
		{fmt.Sprintf("/admin/admins/%d/password", editor.ID), url.Values{"password": {"long enough password"}}},
		{"/admin/entries/delete", url.Values{"ids": {"1"}}},
		{"/admin/trash/purge", url.Values{"ids": {"1"}}},
	}
	for _, tc := range posts {
		form := tc.form
		form.Set("csrf", editorCSRF)
		resp := postForm(t, editorClient, srv.URL+tc.path, form)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("editor POST %s = %d, want 403", tc.path, resp.StatusCode)
		}
	}

	// Editors keep entry editing.
	resp := postForm(t, editorClient, srv.URL+fmt.Sprintf("/admin/entries/%d", entry.ID), url.Values{
		"csrf": {editorCSRF}, "word": {"edited"}, "meaning": {"correct"}, "date": {"2020-01-01"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("editor entry update = %d, want 303", resp.StatusCode)
	}

	// The refused requests changed nothing.
	all, err := st.ListAdmins(t.Context())
	if err != nil {
		t.Fatalf("ListAdmins: %v", err)
	}
	for _, admin := range all {
		if admin.ID == editor.ID && admin.Role != store.RoleEditor {
			t.Fatalf("editor role changed to %q", admin.Role)
		}
		if admin.ID == owner.ID && admin.IsDisabled() {
			t.Fatal("owner was disabled by an editor request")
		}
	}

	// The same routes work for an owner.
	for _, path := range []string{"/admin/admins", "/admin/audit"} {
		resp, _ := get(t, ownerClient, srv.URL+path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("owner GET %s = %d, want 200", path, resp.StatusCode)
		}
	}
	resp = postForm(t, ownerClient, srv.URL+fmt.Sprintf("/admin/admins/%d/disable", editor.ID),
		url.Values{"csrf": {ownerCSRF}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("owner disable = %d, want 303", resp.StatusCode)
	}
}

func TestLastOwnerProtectionHTTP(t *testing.T) {
	srv, client, st := newAdminTestServer(t, testConfig())
	csrf := setupAdmin(t, srv, client)
	owner, err := st.GetAdminByUsername(t.Context(), "owner")
	if err != nil {
		t.Fatalf("GetAdminByUsername: %v", err)
	}
	cases := []string{"role", "disable", "delete"}
	for _, action := range cases {
		form := url.Values{"csrf": {csrf}}
		if action == "role" {
			form.Set("role", "editor")
		}
		resp := postForm(t, client, srv.URL+fmt.Sprintf("/admin/admins/%d/%s", owner.ID, action), form)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("only owner %s = %d, want 409", action, resp.StatusCode)
		}
	}
	updated, err := st.GetAdmin(t.Context(), owner.ID)
	if err != nil {
		t.Fatalf("GetAdmin: %v", err)
	}
	if updated.Role != store.RoleOwner || updated.IsDisabled() {
		t.Fatalf("owner mutated: %+v", updated)
	}

	// With a second enabled owner the same operations on that other account
	// succeed, leaving one enabled owner.
	second := createLocalAdmin(t, st, "second", "second password 1", store.RoleOwner)
	secondPath := fmt.Sprintf("/admin/admins/%d", second.ID)
	if resp := postForm(t, client, srv.URL+secondPath+"/role",
		url.Values{"csrf": {csrf}, "role": {"editor"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("demote second owner = %d, want 303", resp.StatusCode)
	}
	if resp := postForm(t, client, srv.URL+secondPath+"/role",
		url.Values{"csrf": {csrf}, "role": {"owner"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("re-promote second = %d, want 303", resp.StatusCode)
	}
	if resp := postForm(t, client, srv.URL+secondPath+"/disable",
		url.Values{"csrf": {csrf}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("disable second owner = %d, want 303", resp.StatusCode)
	}
	if resp := postForm(t, client, srv.URL+secondPath+"/enable",
		url.Values{"csrf": {csrf}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("enable second owner = %d, want 303", resp.StatusCode)
	}
	if resp := postForm(t, client, srv.URL+secondPath+"/delete",
		url.Values{"csrf": {csrf}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete second owner = %d, want 303", resp.StatusCode)
	}
	if count, err := st.CountEnabledOwners(t.Context()); err != nil || count != 1 {
		t.Fatalf("enabled owners after removing second = %d, %v; want 1", count, err)
	}
}

func TestAdminCreateDuplicateUsername(t *testing.T) {
	srv, client, _ := newAdminTestServer(t, testConfig())
	csrf := setupAdmin(t, srv, client)
	form := url.Values{
		"csrf": {csrf}, "username": {"duplicate"}, "display_name": {""},
		"password": {"duplicate password 1"}, "role": {"editor"},
	}
	if resp := postForm(t, client, srv.URL+"/admin/admins", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("first create = %d, want 303", resp.StatusCode)
	}
	if resp := postForm(t, client, srv.URL+"/admin/admins", form); resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate create = %d, want 409", resp.StatusCode)
	}
}

func TestDisabledAdminRejectedAtLoginAndLiveSession(t *testing.T) {
	srv, _, st := newAdminTestServer(t, testConfig())
	ownerClient := newAdminClient()
	setupAdmin(t, srv, ownerClient)
	editor := createLocalAdmin(t, st, "editor", "editor password 1", store.RoleEditor)
	editorClient := newAdminClient()
	loginAs(t, srv, editorClient, "editor", "editor password 1")

	if resp, _ := get(t, editorClient, srv.URL+"/admin"); resp.StatusCode != http.StatusOK {
		t.Fatalf("editor session before disable = %d", resp.StatusCode)
	}
	if err := st.DisableAdmin(t.Context(), editor.ID, timeNow()); err != nil {
		t.Fatalf("DisableAdmin: %v", err)
	}
	resp, _ := get(t, editorClient, srv.URL+"/admin")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/login") {
		t.Fatalf("disabled live session = %d %q, want login redirect", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp = postForm(t, editorClient, srv.URL+"/admin/login", url.Values{
		"username": {"editor"}, "password": {"editor password 1"},
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled login = %d, want 401", resp.StatusCode)
	}
}

func TestPasswordChangeRevokesSiblingSessions(t *testing.T) {
	srv, client, st := newAdminTestServer(t, testConfig())
	csrf := setupAdmin(t, srv, client)
	owner, err := st.GetAdminByUsername(t.Context(), "owner")
	if err != nil {
		t.Fatalf("GetAdminByUsername: %v", err)
	}
	siblingHash := auth.HashToken("sibling-token-value")
	if err := st.CreateSession(t.Context(), store.Session{
		TokenHash: siblingHash, AdminID: owner.ID, CSRFToken: "csrf",
		CreatedAt: timeNow(), ExpiresAt: timeNow().Add(3600 * 1e9),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	change := func(current, next string) *http.Response {
		return postForm(t, client, srv.URL+"/admin/account/password", url.Values{
			"csrf": {csrf}, "current_password": {current},
			"new_password": {next}, "confirm_password": {next},
		})
	}
	if resp := change("wrong password", "a brand new password"); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("wrong current = %d, want 422", resp.StatusCode)
	}
	if resp := change("correct horse battery", "short"); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("short password = %d, want 422", resp.StatusCode)
	}
	if resp := change("correct horse battery", "a brand new password"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("successful change = %d, want 303", resp.StatusCode)
	}
	if _, err := st.GetSession(t.Context(), siblingHash); err == nil {
		t.Fatal("sibling session survived the password change")
	}
	if resp, _ := get(t, client, srv.URL+"/admin"); resp.StatusCode != http.StatusOK {
		t.Fatalf("current session after change = %d", resp.StatusCode)
	}
	updated, err := st.GetAdmin(t.Context(), owner.ID)
	if err != nil || !auth.VerifyPassword(updated.PasswordHash, "a brand new password") {
		t.Fatalf("new hash not stored: %v", err)
	}

	// Owner reset revokes every target session and needs no old password.
	target := createLocalAdmin(t, st, "target", "target password 1", store.RoleEditor)
	targetHash := auth.HashToken("target-session")
	if err := st.CreateSession(t.Context(), store.Session{
		TokenHash: targetHash, AdminID: target.ID, CSRFToken: "csrf",
		CreatedAt: timeNow(), ExpiresAt: timeNow().Add(3600 * 1e9),
	}); err != nil {
		t.Fatalf("CreateSession target: %v", err)
	}
	resp := postForm(t, client, srv.URL+fmt.Sprintf("/admin/admins/%d/password", target.ID), url.Values{
		"csrf": {csrf}, "password": {"reset password 1"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("owner reset = %d, want 303", resp.StatusCode)
	}
	if _, err := st.GetSession(t.Context(), targetHash); err == nil {
		t.Fatal("target session survived the owner reset")
	}
	reset, err := st.GetAdmin(t.Context(), target.ID)
	if err != nil || !auth.VerifyPassword(reset.PasswordHash, "reset password 1") {
		t.Fatalf("reset hash not stored: %v", err)
	}
}

func TestSessionRevokeScoping(t *testing.T) {
	srv, client, st := newAdminTestServer(t, testConfig())
	csrf := setupAdmin(t, srv, client)
	owner, err := st.GetAdminByUsername(t.Context(), "owner")
	if err != nil {
		t.Fatalf("GetAdminByUsername: %v", err)
	}
	editor := createLocalAdmin(t, st, "editor", "editor password 1", store.RoleEditor)
	editorClient := newAdminClient()
	editorCSRF := loginAs(t, srv, editorClient, "editor", "editor password 1")

	extraHash := auth.HashToken("extra-owner-session")
	if err := st.CreateSession(t.Context(), store.Session{
		TokenHash: extraHash, AdminID: owner.ID, CSRFToken: "csrf",
		CreatedAt: timeNow(), ExpiresAt: timeNow().Add(3600 * 1e9),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	extra, err := st.GetSession(t.Context(), extraHash)
	if err != nil {
		t.Fatalf("GetSession extra: %v", err)
	}
	ref := extra.Session.SessionRef
	if ref == "" {
		t.Fatal("extra session has no reference")
	}

	// An editor may not revoke a reference that belongs to another admin.
	resp := postForm(t, editorClient, srv.URL+"/admin/sessions/revoke", url.Values{
		"csrf": {editorCSRF}, "ref": {ref},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign revoke = %d, want 403", resp.StatusCode)
	}
	if _, err := st.GetSession(t.Context(), extraHash); err != nil {
		t.Fatalf("foreign revoke deleted the session: %v", err)
	}

	// The owner may revoke any reference.
	resp = postForm(t, client, srv.URL+"/admin/sessions/revoke", url.Values{
		"csrf": {csrf}, "ref": {ref},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("owner revoke = %d, want 303", resp.StatusCode)
	}
	if _, err := st.GetSession(t.Context(), extraHash); err == nil {
		t.Fatal("owner revoke did not delete the session")
	}

	// Revoke-all keeps the session making the request.
	anotherHash := auth.HashToken("another-owner-session")
	if err := st.CreateSession(t.Context(), store.Session{
		TokenHash: anotherHash, AdminID: owner.ID, CSRFToken: "csrf",
		CreatedAt: timeNow(), ExpiresAt: timeNow().Add(3600 * 1e9),
	}); err != nil {
		t.Fatalf("CreateSession another: %v", err)
	}
	resp = postForm(t, client, srv.URL+"/admin/sessions/revoke-all", url.Values{"csrf": {csrf}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("revoke-all = %d, want 303", resp.StatusCode)
	}
	if _, err := st.GetSession(t.Context(), anotherHash); err == nil {
		t.Fatal("revoke-all left another session alive")
	}
	if resp, _ := get(t, client, srv.URL+"/admin"); resp.StatusCode != http.StatusOK {
		t.Fatalf("current session after revoke-all = %d", resp.StatusCode)
	}
	_ = editor
}

func TestAuditRowsRecordedForMutations(t *testing.T) {
	srv, client, st := newAdminTestServer(t, testConfig())
	csrf := setupAdmin(t, srv, client)

	// A logout/login cycle records both a logout and a login.success row.
	if resp := postForm(t, client, srv.URL+"/admin/logout", url.Values{"csrf": {csrf}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout = %d, want 303", resp.StatusCode)
	}
	resp := postForm(t, client, srv.URL+"/admin/login", url.Values{
		"username": {"owner"}, "password": {"correct horse battery"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login = %d, want 303", resp.StatusCode)
	}
	csrf = csrfFrom(t, mustGet(t, client, srv.URL+"/admin"))

	entry, err := st.CreateEntry(t.Context(), "wrong", "right", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	resp = postForm(t, client, srv.URL+fmt.Sprintf("/admin/entries/%d", entry.ID), url.Values{
		"csrf": {csrf}, "word": {"fixed"}, "meaning": {"right"}, "date": {"2020-01-02"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("entry update = %d, want 303", resp.StatusCode)
	}

	anon := newAdminClient()
	resp = postForm(t, anon, srv.URL+"/admin/login", url.Values{
		"username": {"owner"}, "password": {"wrong password"},
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("failed login = %d, want 401", resp.StatusCode)
	}

	resp = postForm(t, client, srv.URL+"/admin/admins", url.Values{
		"csrf": {csrf}, "username": {"audituser"}, "display_name": {"Audit"},
		"password": {"audit password 1"}, "role": {"editor"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin create = %d, want 303", resp.StatusCode)
	}
	created, err := st.GetAdminByUsername(t.Context(), "audituser")
	if err != nil {
		t.Fatalf("GetAdminByUsername: %v", err)
	}
	resp = postForm(t, client, srv.URL+fmt.Sprintf("/admin/admins/%d/disable", created.ID),
		url.Values{"csrf": {csrf}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin disable = %d, want 303", resp.StatusCode)
	}

	resp = postForm(t, client, srv.URL+"/admin/settings", url.Values{
		"csrf": {csrf}, "umami_url": {"https://umami.example.com/script.js"},
		"umami_website_id": {"site"}, "umami_domains": {""},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("settings save = %d, want 303", resp.StatusCode)
	}

	page, err := st.ListAudit(t.Context(), store.AuditFilter{PerPage: 100})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	want := map[string]bool{
		"login.success": false, "login.failure": false, "entry.update": false,
		"admin.create": false, "admin.disable": false, "settings.update": false,
	}
	for _, row := range page.Entries {
		if _, ok := want[row.Action]; ok {
			want[row.Action] = true
		}
		if row.IP == "" {
			t.Fatalf("audit row %s has no IP", row.Action)
		}
		if strings.Contains(row.Detail, "audit password 1") || strings.Contains(row.Detail, "correct horse") {
			t.Fatalf("audit row %s leaked a password: %q", row.Action, row.Detail)
		}
	}
	for action, seen := range want {
		if !seen {
			t.Errorf("audit action %q was not recorded", action)
		}
	}
}

func TestAuditCoversFullVocabulary(t *testing.T) {
	srv, client, st := newAdminTestServer(t, testConfig())
	csrf := setupAdmin(t, srv, client)

	entry, err := st.CreateEntry(t.Context(), "full", "vocab", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	post := func(path string, form url.Values) *http.Response {
		form.Set("csrf", csrf)
		return postForm(t, client, srv.URL+path, form)
	}
	id := fmt.Sprintf("%d", entry.ID)
	if resp := post("/admin/entries/delete", url.Values{"ids": {id}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("entry delete = %d", resp.StatusCode)
	}
	if resp := post("/admin/trash/restore", url.Values{"ids": {id}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("entry restore = %d", resp.StatusCode)
	}
	if resp := post("/admin/entries/delete", url.Values{"ids": {id}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("entry re-delete = %d", resp.StatusCode)
	}
	if resp := post("/admin/trash/purge", url.Values{"ids": {id}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("entry purge = %d", resp.StatusCode)
	}

	if resp := post("/admin/admins", url.Values{
		"username": {"victim"}, "display_name": {"Victim"}, "password": {"victim password 1"}, "role": {"editor"},
	}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin create = %d", resp.StatusCode)
	}
	victim, err := st.GetAdminByUsername(t.Context(), "victim")
	if err != nil {
		t.Fatalf("GetAdminByUsername: %v", err)
	}
	vpath := fmt.Sprintf("/admin/admins/%d", victim.ID)
	if resp := post(vpath+"/role", url.Values{"role": {"owner"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("promote = %d", resp.StatusCode)
	}
	if resp := post(vpath+"/role", url.Values{"role": {"editor"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("demote = %d", resp.StatusCode)
	}
	if resp := post(vpath+"/disable", url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("disable = %d", resp.StatusCode)
	}
	if resp := post(vpath+"/enable", url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("enable = %d", resp.StatusCode)
	}
	if resp := post(vpath+"/password", url.Values{"password": {"new victim password"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reset = %d", resp.StatusCode)
	}

	owner, err := st.GetAdminByUsername(t.Context(), "owner")
	if err != nil {
		t.Fatalf("GetAdminByUsername owner: %v", err)
	}
	for _, token := range []string{"vocab-session-one", "vocab-session-two"} {
		if err := st.CreateSession(t.Context(), store.Session{
			TokenHash: auth.HashToken(token), AdminID: owner.ID, CSRFToken: "csrf",
			CreatedAt: timeNow(), ExpiresAt: timeNow().Add(3600 * 1e9),
		}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	first, err := st.GetSession(t.Context(), auth.HashToken("vocab-session-one"))
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if resp := post("/admin/sessions/revoke", url.Values{"ref": {first.Session.SessionRef}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("session revoke = %d", resp.StatusCode)
	}
	if resp := post("/admin/sessions/revoke-all", url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("session revoke-all = %d", resp.StatusCode)
	}

	if resp := post(vpath+"/delete", url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin delete = %d", resp.StatusCode)
	}
	if resp := post("/admin/logout", url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout = %d", resp.StatusCode)
	}

	page, err := st.ListAudit(t.Context(), store.AuditFilter{PerPage: 200})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	seen := map[string]bool{}
	for _, row := range page.Entries {
		seen[row.Action] = true
	}
	for _, action := range []string{
		"entry.delete", "entry.restore", "entry.purge", "admin.create", "admin.role",
		"admin.disable", "admin.enable", "admin.password_reset", "admin.delete",
		"session.revoke", "session.revoke_all", "logout",
	} {
		if !seen[action] {
			t.Errorf("audit action %q was not recorded", action)
		}
	}
}
