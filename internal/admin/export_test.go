package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

func clearAdminEntries(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	for {
		page, err := st.ListEntries(ctx, store.ListParams{PerPage: store.MaxPerPage})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if len(page.Entries) == 0 {
			return
		}
		ids := make([]int64, 0, len(page.Entries))
		for _, entry := range page.Entries {
			ids = append(ids, entry.ID)
		}
		if _, err := st.SoftDeleteEntries(ctx, ids); err != nil {
			t.Fatalf("SoftDeleteEntries: %v", err)
		}
		if _, err := st.PurgeEntries(ctx, ids); err != nil {
			t.Fatalf("PurgeEntries: %v", err)
		}
	}
}

func testZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write(body); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func importEnvelopeJSON(t *testing.T, entries []store.ExportRow) []byte {
	t.Helper()
	body, err := json.Marshal(exportEnvelope{
		FormatVersion: exportFormatVersion,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		EntryCount:    len(entries),
		Entries:       entries,
	})
	if err != nil {
		t.Fatalf("marshal import envelope: %v", err)
	}
	return body
}

func uploadImport(t *testing.T, client *http.Client, url, csrf, name string,
	data []byte, mode, conflict string, withCSRF bool) (*http.Response, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if withCSRF {
		_ = w.WriteField("csrf", csrf)
	}
	_ = w.WriteField("mode", mode)
	_ = w.WriteField("conflict", conflict)
	file, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST import: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read import response: %v", err)
	}
	return resp, string(body)
}

func TestAdminExportJSON(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	setupAdmin(t, srv, client)
	clearAdminEntries(t, st)

	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	if _, err := st.CreateEntry(t.Context(), "alpha", "first", "Alice", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.CreateEntry(t.Context(), "beta", "second", "", photo); err != nil {
		t.Fatalf("CreateEntry photo: %v", err)
	}

	resp, body := get(t, client, srv.URL+"/admin/export.json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "vandyke-ledger-") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if cache := resp.Header.Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("Cache-Control = %q", cache)
	}

	var envelope exportEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("parse export: %v", err)
	}
	if envelope.FormatVersion != exportFormatVersion || envelope.EntryCount != 2 || envelope.PhotoCount != 1 {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.ExportedAt == "" {
		t.Fatal("exported_at missing")
	}
	if len(envelope.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(envelope.Entries))
	}
	first, second := envelope.Entries[0], envelope.Entries[1]
	if first.Word != "alpha" || first.Person != "Alice" || first.HasPhoto {
		t.Errorf("first entry = %+v", first)
	}
	if !second.HasPhoto || second.PhotoContentType != "image/png" || second.PhotoSHA256 == "" {
		t.Errorf("second entry photo = %+v", second)
	}
}

func TestAdminExportCSV(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	setupAdmin(t, srv, client)
	clearAdminEntries(t, st)

	if _, err := st.CreateEntry(t.Context(), "alpha", `a, "b"`+"\nnext", "", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.CreateEntry(t.Context(), "beta", "second", "", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	resp, body := get(t, client, srv.URL+"/admin/export.csv")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("Content-Type = %q", ct)
	}
	records, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("csv rows = %d, want header plus 2", len(records))
	}
	wantHeader := []string{"id", "word", "meaning", "person", "created_at",
		"has_photo", "photo_content_type", "photo_sha256"}
	if strings.Join(records[0], ",") != strings.Join(wantHeader, ",") {
		t.Fatalf("header = %v", records[0])
	}
	if records[1][2] != "a, \"b\"\nnext" {
		t.Fatalf("quoted meaning = %q", records[1][2])
	}
}

func TestAdminExportZIP(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	setupAdmin(t, srv, client)
	clearAdminEntries(t, st)

	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	if _, err := st.CreateEntry(t.Context(), "with", "photo", "", photo); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := st.CreateEntry(t.Context(), "without", "photo", "", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	resp, body := get(t, client, srv.URL+"/admin/export.zip")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type = %q", ct)
	}

	reader, err := zip.NewReader(strings.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	contents := make(map[string][]byte)
	order := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", file.Name, err)
		}
		contents[file.Name] = data
		order = append(order, file.Name)
	}
	if len(order) < 3 || order[0] != "manifest.json" || order[1] != "entries.json" {
		t.Fatalf("zip order = %v", order)
	}

	var manifest exportManifest
	if err := json.Unmarshal(contents["manifest.json"], &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.FormatVersion != exportFormatVersion || manifest.EntryCount != 2 || manifest.PhotoCount != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}

	var envelope exportEnvelope
	if err := json.Unmarshal(contents["entries.json"], &envelope); err != nil {
		t.Fatalf("parse entries: %v", err)
	}
	if envelope.EntryCount != 2 || envelope.PhotoCount != 1 {
		t.Fatalf("entries envelope = %+v", envelope)
	}
	if len(contents) != 3 {
		t.Fatalf("zip files = %v, want manifest, entries and one photo", order)
	}
	var photoBytes []byte
	for name, data := range contents {
		if strings.HasPrefix(name, "photos/") {
			photoBytes = data
		}
	}
	if !bytes.Equal(photoBytes, photo.Data) {
		t.Fatal("zip photo does not contain the exported photo bytes")
	}
}

func TestAdminImportParsingErrors(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, srv, client)
	clearAdminEntries(t, st)

	entry := store.ExportRow{ID: 1, Word: "word", Meaning: "meaning"}
	validEntries, err := json.Marshal(exportEnvelope{
		FormatVersion: exportFormatVersion,
		EntryCount:    1,
		Entries:       []store.ExportRow{entry},
	})
	if err != nil {
		t.Fatalf("marshal entries: %v", err)
	}

	cases := []struct {
		name     string
		filename string
		data     []byte
		status   int
		contains string
	}{
		{"malformed json", "export.json", []byte("{"), http.StatusBadRequest, "could not parse"},
		{"unknown version", "export.json", []byte(`{"format_version":2,"entries":[]}`), http.StatusBadRequest, "supports 1"},
		{"missing entries", "export.zip", testZip(t, map[string][]byte{
			"manifest.json": []byte(`{"format_version":1}`),
		}), http.StatusBadRequest, "entries.json"},
		{"orphan photo", "export.zip", testZip(t, map[string][]byte{
			"entries.json": validEntries,
			"photos/2.jpg": tinyPNG(t),
		}), http.StatusBadRequest, "no matching entry"},
		{"unknown zip manifest", "export.zip", testZip(t, map[string][]byte{
			"manifest.json": []byte(`{"format_version":9}`),
			"entries.json":  validEntries,
		}), http.StatusBadRequest, "supports 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := uploadImport(t, client, srv.URL+"/admin/import", csrf, tc.filename, tc.data, "preview", "skip", true)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d (%s)", resp.StatusCode, tc.status, body)
			}
			if !strings.Contains(body, tc.contains) {
				t.Fatalf("body = %q, want %q", body, tc.contains)
			}
		})
	}
}

func TestAdminImportPreviewAndApply(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, srv, client)
	clearAdminEntries(t, st)

	existing, err := st.CreateEntry(t.Context(), "Existing", "row", "Alice", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	conflictRow := store.ExportRow{
		ID: existing.ID, Word: "existing", Meaning: "ROW", Person: "Bob",
		CreatedAt: time.Date(2018, 3, 2, 1, 2, 3, 0, time.UTC),
	}
	newRow := store.ExportRow{ID: 99, Word: "brand", Meaning: "new", Person: "Carol"}
	payload := importEnvelopeJSON(t, []store.ExportRow{newRow, conflictRow})

	// Preview writes nothing and reports the conflict mode.
	resp, body := uploadImport(t, client, srv.URL+"/admin/import", csrf, "export.json", payload, "preview", "replace", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d (%s)", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Would create</dt><dd>1</dd>") ||
		!strings.Contains(body, "Would conflict</dt><dd>1</dd>") ||
		!strings.Contains(body, "replace") {
		t.Fatalf("preview body missing counts:\n%s", body)
	}
	if count, err := st.CountEntries(t.Context()); err != nil || count != 1 {
		t.Fatalf("count after preview = %d, %v; want 1", count, err)
	}

	// Apply inserts the new row and replaces person/date on the conflict.
	resp, body = uploadImport(t, client, srv.URL+"/admin/import", csrf, "export.json", payload, "apply", "replace", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply status = %d (%s)", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Created</dt><dd>1</dd>") || !strings.Contains(body, "Replaced</dt><dd>1</dd>") {
		t.Fatalf("apply body missing counts:\n%s", body)
	}
	stored, err := st.GetEntry(t.Context(), existing.ID)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if stored.Word != "Existing" || stored.Meaning != "row" || stored.Person != "Bob" ||
		!stored.CreatedAt.Equal(conflictRow.CreatedAt) {
		t.Fatalf("replaced row = %+v", stored)
	}
	if count, err := st.CountEntries(t.Context()); err != nil || count != 2 {
		t.Fatalf("count after apply = %d, %v; want 2", count, err)
	}

	// The same file with skip leaves the row untouched.
	skipPayload := importEnvelopeJSON(t, []store.ExportRow{conflictRow})
	resp, body = uploadImport(t, client, srv.URL+"/admin/import", csrf, "export.json", skipPayload, "apply", "skip", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("skip status = %d (%s)", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Skipped</dt><dd>1</dd>") {
		t.Fatalf("skip body missing count:\n%s", body)
	}

	// A mid-file error rolls back the whole apply and names the row.
	badRow := store.ExportRow{ID: 100, Word: strings.Repeat("x", store.MaxWordLen+1), Meaning: "too long"}
	failing := importEnvelopeJSON(t, []store.ExportRow{
		{ID: 101, Word: "should", Meaning: "roll back"},
		badRow,
	})
	resp, body = uploadImport(t, client, srv.URL+"/admin/import", csrf, "export.json", failing, "apply", "skip", true)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("failing status = %d (%s)", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Row 2") || !strings.Contains(body, "nothing was imported") {
		t.Fatalf("failing body = %s", body)
	}
	if count, err := st.CountEntries(t.Context()); err != nil || count != 2 {
		t.Fatalf("count after rollback = %d, %v; want 2", count, err)
	}
}

func TestAdminExportAuthAndImportCSRF(t *testing.T) {
	srv, client, st := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	setupAdmin(t, srv, client)
	clearAdminEntries(t, st)

	anon := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, path := range []string{"/admin/export.json", "/admin/export.csv", "/admin/export.zip", "/admin/import"} {
		resp, err := anon.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("anonymous GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/login") {
			t.Fatalf("anonymous %s = %d %q, want login redirect", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}

	payload := importEnvelopeJSON(t, []store.ExportRow{{ID: 1, Word: "a", Meaning: "b"}})
	resp, _ := uploadImport(t, client, srv.URL+"/admin/import", "", "export.json", payload, "preview", "skip", false)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("import without CSRF = %d, want 403", resp.StatusCode)
	}
}

func TestAdminExportImportRoundTrip(t *testing.T) {
	source, sourceClient, sourceStore := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	setupAdmin(t, source, sourceClient)
	clearAdminEntries(t, sourceStore)

	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	if _, err := sourceStore.CreateEntry(t.Context(), "round", "trip", "Alice", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if _, err := sourceStore.CreateEntry(t.Context(), "with", "photo", "", photo); err != nil {
		t.Fatalf("CreateEntry photo: %v", err)
	}
	resp, archive := get(t, sourceClient, source.URL+"/admin/export.zip")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}

	target, targetClient, targetStore := newAdminTestServer(t, config.Config{
		AdminLoginRatePerMinute: 1000, AdminLoginRateBurst: 100,
	})
	csrf := setupAdmin(t, target, targetClient)
	clearAdminEntries(t, targetStore)

	resp, body := uploadImport(t, targetClient, target.URL+"/admin/import", csrf,
		"export.zip", []byte(archive), "apply", "skip", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import status = %d (%s)", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Created</dt><dd>2</dd>") {
		t.Fatalf("import result = %s", body)
	}
	sourceCount, err := sourceStore.CountEntries(t.Context())
	if err != nil {
		t.Fatalf("source CountEntries: %v", err)
	}
	targetCount, err := targetStore.CountEntries(t.Context())
	if err != nil {
		t.Fatalf("target CountEntries: %v", err)
	}
	if targetCount != sourceCount {
		t.Fatalf("round-trip count = %d, want %d", targetCount, sourceCount)
	}
	page, err := targetStore.ListEntries(t.Context(), store.ListParams{Query: "photo"})
	if err != nil {
		t.Fatalf("target ListEntries: %v", err)
	}
	if page.Total == 0 {
		t.Fatal("round-trip lost the photo entry")
	}
	found := false
	for _, entry := range page.Entries {
		if entry.PhotoURL != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("round-trip lost the photo")
	}

	// Re-importing the same archive skips every row.
	resp, body = uploadImport(t, targetClient, target.URL+"/admin/import", csrf,
		"export.zip", []byte(archive), "apply", "skip", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second import status = %d (%s)", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Skipped</dt><dd>2</dd>") {
		t.Fatalf("second import result = %s", body)
	}
	if count, err := targetStore.CountEntries(t.Context()); err != nil || count != sourceCount {
		t.Fatalf("count after second import = %d, %v; want %d", count, err, sourceCount)
	}
}
