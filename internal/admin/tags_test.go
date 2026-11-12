package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"vandyke.thor.edu/vandyke/internal/store"
)

func auditAction(t *testing.T, st *store.Store, action string) []store.AuditEntry {
	t.Helper()
	page, err := st.ListAudit(context.Background(), store.AuditFilter{Action: action, PerPage: 50})
	if err != nil {
		t.Fatalf("ListAudit %q: %v", action, err)
	}
	return page.Entries
}

func TestAdminTagManagementRolesCSRFAndAudit(t *testing.T) {
	srv, owner, st := newAdminTestServer(t, testConfig())
	ownerCSRF := setupAdmin(t, srv, owner)
	ctx := context.Background()

	anonymous := newAdminClient()
	if resp, _ := get(t, anonymous, srv.URL+"/admin/tags"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("anonymous GET = %d, want a login redirect", resp.StatusCode)
	}

	resp, body := get(t, owner, srv.URL+"/admin/tags")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner GET = %d", resp.StatusCode)
	}
	for _, want := range []string{"The vocabulary", "typo", "misheard", "autocorrect"} {
		if !strings.Contains(body, want) {
			t.Errorf("tag page missing %q", want)
		}
	}

	createForm := url.Values{
		"csrf":  {ownerCSRF},
		"name":  {"Shouting"},
		"color": {"#123ABC"},
	}
	if status, body := postFormRead(t, owner, srv.URL+"/admin/tags", createForm); status != http.StatusSeeOther {
		t.Fatalf("create = %d: %.300s", status, body)
	}
	tag, err := st.GetTagBySlug(ctx, "shouting")
	if err != nil {
		t.Fatalf("GetTagBySlug: %v", err)
	}
	if tag.Name != "Shouting" || tag.Color != "#123abc" {
		t.Fatalf("created tag = %+v", tag)
	}

	duplicate := url.Values{"csrf": {ownerCSRF}, "name": {"shouting"}, "color": {"#123abc"}}
	if status, body := postFormRead(t, owner, srv.URL+"/admin/tags", duplicate); status != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "already exists") {
		t.Fatalf("duplicate = %d: %.300s", status, body)
	}

	invalidColor := url.Values{"csrf": {ownerCSRF}, "name": {"Broken"}, "color": {"red"}}
	if status, body := postFormRead(t, owner, srv.URL+"/admin/tags", invalidColor); status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid colour = %d, want 422", status)
	} else if !strings.Contains(body, "Broken") || !strings.Contains(body, "colour") {
		t.Fatalf("422 did not preserve the name or explain the colour: %.400s", body)
	}
	if _, err := st.GetTagBySlug(ctx, "broken"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("invalid colour wrote a tag: %v", err)
	}

	punctuation := url.Values{"csrf": {ownerCSRF}, "name": {"!!!"}, "color": {"#123abc"}}
	if status, _ := postFormRead(t, owner, srv.URL+"/admin/tags", punctuation); status != http.StatusUnprocessableEntity {
		t.Fatalf("punctuation-only name = %d, want 422", status)
	}

	tagURL := srv.URL + "/admin/tags/" + strconv.FormatInt(tag.ID, 10)
	rename := url.Values{"csrf": {ownerCSRF}, "name": {"Shouted"}, "color": {"#123abc"}}
	if status, _ := postFormRead(t, owner, tagURL, rename); status != http.StatusSeeOther {
		t.Fatalf("rename = %d", status)
	}
	renamed, err := st.GetTag(ctx, tag.ID)
	if err != nil || renamed.Name != "Shouted" || renamed.Slug != "shouting" {
		t.Fatalf("renamed tag = %+v (%v), want the original slug", renamed, err)
	}

	recolour := url.Values{"csrf": {ownerCSRF}, "name": {"Shouted"}, "color": {"#abc123"}}
	if status, _ := postFormRead(t, owner, tagURL, recolour); status != http.StatusSeeOther {
		t.Fatalf("recolour = %d", status)
	}
	updated, err := st.GetTag(ctx, tag.ID)
	if err != nil || updated.Color != "#abc123" || updated.Slug != "shouting" {
		t.Fatalf("recoloured tag = %+v (%v)", updated, err)
	}

	created := auditAction(t, st, "tag.create")
	if len(created) != 1 || created[0].Detail != "name=Shouting" {
		t.Fatalf("tag.create audit = %+v", created)
	}
	if created[0].Subject != strconv.FormatInt(tag.ID, 10) {
		t.Fatalf("tag.create subject = %q", created[0].Subject)
	}
	renames := auditAction(t, st, "tag.rename")
	if len(renames) != 1 || renames[0].Detail != "old=Shouting new=Shouted" {
		t.Fatalf("tag.rename audit = %+v", renames)
	}
	colours := auditAction(t, st, "tag.color")
	if len(colours) != 1 || colours[0].Detail != "old=#123abc new=#abc123" {
		t.Fatalf("tag.color audit = %+v", colours)
	}

	// Live and total counts disagree once one tagged entry is trashed.
	entry, err := st.CreateEntry(ctx, "telling", "count", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if err := st.SetEntryTags(ctx, entry.ID, []int64{tag.ID}); err != nil {
		t.Fatalf("SetEntryTags: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	_, page := get(t, owner, srv.URL+"/admin/tags")
	if !strings.Contains(page, "1 entry loses the tag") {
		t.Fatalf("delete confirmation does not name the damage: %.600s", page)
	}

	// A missing CSRF token changes nothing.
	noCSRF := url.Values{"name": {"NoCsrf"}, "color": {"#123abc"}}
	if status, _ := postFormRead(t, owner, srv.URL+"/admin/tags", noCSRF); status != http.StatusForbidden {
		t.Fatalf("missing CSRF = %d, want 403", status)
	}
	if _, err := st.GetTagBySlug(ctx, "nocsrf"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("refused request wrote a tag: %v", err)
	}

	// Deletion is owner-only.
	createLocalAdmin(t, st, "editor", "editor password 1", store.RoleEditor)
	editorClient := newAdminClient()
	editorCSRF := loginAs(t, srv, editorClient, "editor", "editor password 1")
	deleteURL := tagURL + "/delete"
	if status, _ := postFormRead(t, editorClient, deleteURL, url.Values{"csrf": {editorCSRF}}); status != http.StatusForbidden {
		t.Fatalf("editor delete = %d, want 403", status)
	}
	if _, err := st.GetTag(ctx, tag.ID); err != nil {
		t.Fatalf("refused editor delete removed the tag: %v", err)
	}
	if rows := auditAction(t, st, "tag.delete"); len(rows) != 0 {
		t.Fatalf("refused editor delete wrote an audit row: %+v", rows)
	}

	if status, _ := postFormRead(t, owner, deleteURL, url.Values{"csrf": {ownerCSRF}}); status != http.StatusSeeOther {
		t.Fatalf("owner delete = %d", status)
	}
	if _, err := st.GetTag(ctx, tag.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted tag still exists: %v", err)
	}
	if tags, err := st.EntryTags(ctx, entry.ID); err != nil || len(tags) != 0 {
		t.Fatalf("deleted tag left assignments: %+v (%v)", tags, err)
	}
	deleted := auditAction(t, st, "tag.delete")
	if len(deleted) != 1 || !strings.Contains(deleted[0].Detail, "name=Shouted") {
		t.Fatalf("tag.delete audit = %+v", deleted)
	}
}

func TestAdminEntryTagAssignment(t *testing.T) {
	srv, client, st := newAdminTestServer(t, testConfig())
	csrf := setupAdmin(t, srv, client)
	ctx := context.Background()

	entry, err := st.CreateEntry(ctx, "tagproef", "tag test", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	typo, err := st.GetTagBySlug(ctx, "typo")
	if err != nil {
		t.Fatalf("GetTagBySlug typo: %v", err)
	}
	misheard, err := st.GetTagBySlug(ctx, "misheard")
	if err != nil {
		t.Fatalf("GetTagBySlug misheard: %v", err)
	}
	editURL := srv.URL + "/admin/entries/" + strconv.FormatInt(entry.ID, 10)
	resp, body := get(t, client, editURL+"/edit")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit GET = %d", resp.StatusCode)
	}
	if !strings.Contains(body, `name="tags" value="`+strconv.FormatInt(typo.ID, 10)+`"`) {
		t.Fatal("edit form has no tag checkbox")
	}
	if !strings.Contains(body, "Manage the vocabulary") {
		t.Fatal("edit form does not link to tag management")
	}

	form := func(word, meaning string, tags ...int64) url.Values {
		values := url.Values{
			"csrf":    {csrf},
			"word":    {word},
			"meaning": {meaning},
		}
		for _, id := range tags {
			values.Add("tags", strconv.FormatInt(id, 10))
		}
		return values
	}

	if status, body := postFormRead(t, client, editURL, form("tagproef", "tag test", typo.ID, misheard.ID)); status != http.StatusSeeOther {
		t.Fatalf("assign = %d: %.300s", status, body)
	}
	tags, err := st.EntryTags(ctx, entry.ID)
	if err != nil || len(tags) != 2 {
		t.Fatalf("entry tags = %+v (%v), want two", tags, err)
	}
	assignment := auditAction(t, st, "entry.tags")
	if len(assignment) != 1 || assignment[0].Detail != "tags=misheard,typo" {
		t.Fatalf("entry.tags audit = %+v", assignment)
	}

	// The ledger row renders one chip per tag, each linking to the filter.
	_, ledger := get(t, client, srv.URL+"/entries?tag=typo")
	for _, slug := range []string{"typo", "misheard"} {
		if !strings.Contains(ledger, `href="/entries?tag=`+slug+`"`) {
			t.Errorf("ledger row has no %s chip", slug)
		}
	}

	// An empty submission clears every tag.
	if status, _ := postFormRead(t, client, editURL, form("tagproef", "tag test")); status != http.StatusSeeOther {
		t.Fatalf("clear = %d", status)
	}
	tags, err = st.EntryTags(ctx, entry.ID)
	if err != nil || len(tags) != 0 {
		t.Fatalf("entry tags after clear = %+v (%v)", tags, err)
	}
	cleared := auditAction(t, st, "entry.tags")
	if len(cleared) != 2 || cleared[0].Detail != "tags=" {
		t.Fatalf("clear audit = %+v", cleared)
	}

	// An unknown id is ignored without failing the edit.
	if status, _ := postFormRead(t, client, editURL, form("tagproef", "tag test", 999999)); status != http.StatusSeeOther {
		t.Fatalf("unknown tag id = %d", status)
	}
	tags, err = st.EntryTags(ctx, entry.ID)
	if err != nil || len(tags) != 0 {
		t.Fatalf("unknown tag id assigned something: %+v (%v)", tags, err)
	}

	// Assignments survive the trash and return with the entry.
	if status, _ := postFormRead(t, client, editURL, form("tagproef", "tag test", typo.ID)); status != http.StatusSeeOther {
		t.Fatalf("reassign = %d", status)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.RestoreEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("RestoreEntries: %v", err)
	}
	tags, err = st.EntryTags(ctx, entry.ID)
	if err != nil || len(tags) != 1 || tags[0].ID != typo.ID {
		t.Fatalf("tags after trash/restore = %+v (%v)", tags, err)
	}

	// A trashed entry's edit page still shows its tags.
	if _, err := st.SoftDeleteEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries second: %v", err)
	}
	_, trashed := get(t, client, editURL+"/edit")
	if !strings.Contains(trashed, `name="tags" value="`+strconv.FormatInt(typo.ID, 10)+`" checked`) {
		t.Fatal("trashed entry edit page lost its checked tag")
	}
}
