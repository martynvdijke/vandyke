package web

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/slip"
	"vandyke.thor.edu/vandyke/internal/store"
)

func purgeAllEntries(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	ids, err := st.AllEntryIDs(ctx)
	if err != nil {
		t.Fatalf("AllEntryIDs: %v", err)
	}
	if len(ids) == 0 {
		return
	}
	if _, err := st.SoftDeleteEntries(ctx, ids); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.PurgeEntries(ctx, ids); err != nil {
		t.Fatalf("PurgeEntries: %v", err)
	}
}

func slipSection(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `<section class="slip"`)
	if start < 0 {
		return ""
	}
	end := strings.Index(body[start:], "</section>")
	if end < 0 {
		return body[start:]
	}
	return body[start : start+end]
}

func TestHomeSlipSection(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	today, err := slip.New(st).Today(context.Background())
	if err != nil {
		t.Fatalf("slip.Today: %v", err)
	}

	status, _, body := fetch(t, srv, "/", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	section := slipSection(t, body)
	if section == "" {
		t.Fatal("home page has no slip section")
	}
	for _, want := range []string{
		`aria-labelledby="slip-title"`,
		`data-slip`,
		`id="slip-title"`,
		`href="/e/` + strconv.FormatInt(today.Entry.ID, 10) + `"`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("slip section is missing %q", want)
		}
	}
	if today.Entry.Word != "" && !strings.Contains(section, today.Entry.Word) {
		t.Errorf("slip section does not show the word %q", today.Entry.Word)
	}
	if today.Entry.Meaning != "" && !strings.Contains(section, today.Entry.Meaning) {
		t.Errorf("slip section does not show the correction %q", today.Entry.Meaning)
	}
}

func TestHomeSlipAbsentWithListingParameters(t *testing.T) {
	srv, _ := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	paths := []string{
		"/?q=boeke",
		"/?q=",
		"/?year=2026",
		"/?person=Marieke",
		"/?from=2020-01-01",
		"/?to=2026-01-01",
		"/?photo=1",
		"/?page=2",
		"/?per_page=30",
		"/?sort=word&order=asc",
	}
	for _, path := range paths {
		status, _, body := fetch(t, srv, path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, status)
		}
		if strings.Contains(body, `data-slip`) {
			t.Errorf("%s rendered the slip section", path)
		}
	}
}

func TestHomeSlipAbsentOnEmptyLedger(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	purgeAllEntries(t, st)

	status, _, body := fetch(t, srv, "/", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if strings.Contains(body, `data-slip`) || strings.Contains(body, `class="slip"`) {
		t.Fatal("empty ledger rendered a slip section")
	}
}

func TestLedgerFragmentNeverContainsSlip(t *testing.T) {
	srv, _ := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	status, _, frag := fetch(t, srv, "/entries", map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if strings.Contains(frag, `data-slip`) {
		t.Fatal("the ledger fragment contains the slip section")
	}
	status, _, doc := fetch(t, srv, "/entries", nil)
	if status != http.StatusOK {
		t.Fatalf("document status = %d, want 200", status)
	}
	if strings.Contains(doc, `data-slip`) {
		t.Fatal("/entries document contains the slip section")
	}
}

func TestPermalinkSlipBadge(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	today, err := slip.New(st).Today(context.Background())
	if err != nil {
		t.Fatalf("slip.Today: %v", err)
	}
	pinned := strconv.FormatInt(today.Entry.ID, 10)

	status, _, body := fetch(t, srv, "/e/"+pinned, nil)
	if status != http.StatusOK {
		t.Fatalf("pinned permalink status = %d", status)
	}
	if !strings.Contains(body, `class="slip-badge"`) || !strings.Contains(body, "Slip of the day") {
		t.Fatal("pinned permalink does not carry the slip badge")
	}
	if !strings.Contains(body, `rel="canonical" href="`+srv.URL+`/e/`+pinned+`"`) {
		t.Fatal("pinned permalink lost its self-canonical")
	}

	ids, err := st.AllEntryIDs(context.Background())
	if err != nil {
		t.Fatalf("AllEntryIDs: %v", err)
	}
	var other int64
	for _, id := range ids {
		if id != today.Entry.ID {
			other = id
			break
		}
	}
	if other == 0 {
		t.Skip("ledger has only one live entry")
	}
	status, _, body = fetch(t, srv, "/e/"+strconv.FormatInt(other, 10), nil)
	if status != http.StatusOK {
		t.Fatalf("other permalink status = %d", status)
	}
	if strings.Contains(body, "Slip of the day") {
		t.Fatal("a non-pinned permalink carries the slip badge")
	}
}

func TestCanonicalAndRobotsMatrix(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	entry, err := st.CreateEntry(t.Context(), "matrix", "canonical", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	permalink := "/e/" + strconv.FormatInt(entry.ID, 10)

	cases := []struct {
		path      string
		robots    string
		canonical string // empty means no canonical link at all
	}{
		{path: "/", robots: "index,follow", canonical: srv.URL + "/"},
		{path: "/?add=1", robots: "index,follow", canonical: srv.URL + "/"},
		{path: "/?info=1", robots: "index,follow", canonical: srv.URL + "/"},
		{path: "/?q=boeke", robots: "noindex,follow"},
		{path: "/?q=", robots: "noindex,follow"},
		{path: "/?year=2026", robots: "noindex,follow"},
		{path: "/?person=Marieke", robots: "noindex,follow"},
		{path: "/?from=2020-01-01", robots: "noindex,follow"},
		{path: "/?to=2026-01-01", robots: "noindex,follow"},
		{path: "/?photo=1", robots: "noindex,follow"},
		{path: "/?page=2", robots: "noindex,follow"},
		{path: "/?per_page=30", robots: "noindex,follow"},
		{path: "/?sort=word&order=asc", robots: "noindex,follow"},
		{path: "/entries", robots: "noindex,follow"},
		{path: "/entries?q=boeke", robots: "noindex,follow"},
		{path: "/entries?year=2026", robots: "noindex,follow"},
		{path: "/entries?photo=1", robots: "noindex,follow"},
		{path: "/entries?page=2", robots: "noindex,follow"},
		{path: "/stats", robots: "index,follow", canonical: srv.URL + "/stats"},
		{path: permalink, robots: "index,follow", canonical: srv.URL + permalink},
	}

	for _, tc := range cases {
		status, _, body := fetch(t, srv, tc.path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.path, status)
		}
		if !strings.Contains(body, `<meta name="robots" content="`+tc.robots+`">`) {
			t.Errorf("%s does not carry robots %q", tc.path, tc.robots)
		}
		if tc.canonical == "" {
			if strings.Contains(body, `rel="canonical"`) {
				t.Errorf("%s carries a canonical link next to noindex", tc.path)
			}
			continue
		}
		if !strings.Contains(body, `<link rel="canonical" href="`+tc.canonical+`">`) {
			t.Errorf("%s canonical is not %q", tc.path, tc.canonical)
		}
	}
}

var jsonLDBlock = regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`)

func decodeJSONLD(t *testing.T, body string) map[string]any {
	t.Helper()
	matches := jsonLDBlock.FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		t.Fatalf("found %d JSON-LD blocks, want 1", len(matches))
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(matches[0][1]), &doc); err != nil {
		t.Fatalf("decode JSON-LD: %v", err)
	}
	return doc
}

func TestPermalinkJSONLD(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	entry, err := st.CreateEntry(t.Context(), "definitie", "definition", "Marieke", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	permalink := "/e/" + strconv.FormatInt(entry.ID, 10)

	status, _, body := fetch(t, srv, permalink, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	doc := decodeJSONLD(t, body)
	if doc["@context"] != "https://schema.org" {
		t.Errorf("@context = %v", doc["@context"])
	}
	if doc["@type"] != "DefinedTerm" {
		t.Errorf("@type = %v, want DefinedTerm", doc["@type"])
	}
	if doc["name"] != "definitie" {
		t.Errorf("name = %v", doc["name"])
	}
	if doc["description"] != "definition" {
		t.Errorf("description = %v", doc["description"])
	}
	if doc["url"] != srv.URL+permalink {
		t.Errorf("url = %v, want the canonical permalink", doc["url"])
	}
	if doc["identifier"] != strconv.FormatInt(entry.ID, 10) {
		t.Errorf("identifier = %v, want the entry id", doc["identifier"])
	}
	if doc["dateCreated"] == "" || doc["dateCreated"] == nil {
		t.Error("dateCreated is missing")
	}
	set, ok := doc["inDefinedTermSet"].(map[string]any)
	if !ok {
		t.Fatalf("inDefinedTermSet = %T, want an object", doc["inDefinedTermSet"])
	}
	if set["@type"] != "DefinedTermSet" || set["name"] != "The vanDyke Corrections Ledger" {
		t.Errorf("inDefinedTermSet = %+v", set)
	}
	if _, ok := doc["image"]; ok {
		t.Error("a text-only entry should not declare an image")
	}
}

func TestPermalinkJSONLDPhotoOnlyEntry(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(t.Context(), "", "", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	permalink := "/e/" + strconv.FormatInt(entry.ID, 10)

	_, _, body := fetch(t, srv, permalink, nil)
	doc := decodeJSONLD(t, body)
	if got := doc["name"]; got != "Photo entry #"+leftPad4(entry.ID) {
		t.Errorf("name = %v, want the photo entry wording", got)
	}
	if _, ok := doc["description"]; ok {
		t.Error("photo-only entry should omit description")
	}
	wantImage := srv.URL + entry.PhotoURL + "?size=display"
	if doc["image"] != wantImage {
		t.Errorf("image = %v, want %q", doc["image"], wantImage)
	}
}

func leftPad4(id int64) string {
	s := strconv.FormatInt(id, 10)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

func TestPermalinkJSONLDCannotBreakThePage(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	entry, err := st.CreateEntry(t.Context(), `</script><b>boos</b>`, "a & b < c", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	_, _, body := fetch(t, srv, "/e/"+strconv.FormatInt(entry.ID, 10), nil)
	if strings.Contains(body, `</script><b>boos</b>`) {
		t.Fatal("entry text reached the response unescaped")
	}
	doc := decodeJSONLD(t, body)
	if doc["name"] != `</script><b>boos</b>` {
		t.Errorf("decoded name = %v", doc["name"])
	}
	if doc["description"] != "a & b < c" {
		t.Errorf("decoded description = %v", doc["description"])
	}
}

func TestPermalinkFallbackPreviewImage(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	entry, err := st.CreateEntry(t.Context(), "tekst", "text only", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	_, _, body := fetch(t, srv, "/e/"+strconv.FormatInt(entry.ID, 10), nil)
	image := srv.URL + "/static/images/og-default.png"
	for _, want := range []string{
		`property="og:image" content="` + image + `"`,
		`property="og:image:width" content="1200"`,
		`property="og:image:height" content="630"`,
		`property="og:image:alt"`,
		`name="twitter:image" content="` + image + `"`,
		`name="twitter:card" content="summary_large_image"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("text-only permalink is missing %q", want)
		}
	}

	status, headers, data := fetch(t, srv, "/static/images/og-default.png", nil)
	if status != http.StatusOK {
		t.Fatalf("preview asset status = %d", status)
	}
	if got := headers.Get("Content-Type"); got != "image/png" {
		t.Errorf("preview content type = %q", got)
	}
	if len(data) < 1000 {
		t.Errorf("preview asset is suspiciously small: %d bytes", len(data))
	}
}

func TestPermalinkPhotoPreviewImage(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(t.Context(), "foto", "photo", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	_, _, body := fetch(t, srv, "/e/"+strconv.FormatInt(entry.ID, 10), nil)
	image := srv.URL + entry.PhotoURL + "?size=display"
	for _, want := range []string{
		`property="og:image" content="` + image + `"`,
		`name="twitter:image" content="` + image + `"`,
		`name="twitter:card" content="summary_large_image"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("photo permalink is missing %q", want)
		}
	}
}

func TestStatsJSONLD(t *testing.T) {
	srv, _ := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	_, _, body := fetch(t, srv, "/stats", nil)
	doc := decodeJSONLD(t, body)
	if doc["@type"] != "Dataset" {
		t.Errorf("@type = %v, want Dataset", doc["@type"])
	}
	if doc["url"] != srv.URL+"/stats" {
		t.Errorf("url = %v", doc["url"])
	}
	if doc["isAccessibleForFree"] != true {
		t.Errorf("isAccessibleForFree = %v", doc["isAccessibleForFree"])
	}
	if coverage, _ := doc["temporalCoverage"].(string); !strings.Contains(coverage, "/") {
		t.Errorf("temporalCoverage = %v, want a start/end interval", doc["temporalCoverage"])
	}
	measured, ok := doc["variableMeasured"].([]any)
	if !ok || len(measured) == 0 {
		t.Fatalf("variableMeasured = %T %v", doc["variableMeasured"], doc["variableMeasured"])
	}
	distribution, ok := doc["distribution"].(map[string]any)
	if !ok {
		t.Fatalf("distribution = %T", doc["distribution"])
	}
	if content, _ := distribution["contentUrl"].(string); !strings.Contains(content, "/api/v1/stats") {
		t.Errorf("distribution.contentUrl = %v", distribution["contentUrl"])
	}
}

func TestStatsJSONLDEmptyLedger(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	purgeAllEntries(t, st)

	_, _, body := fetch(t, srv, "/stats", nil)
	doc := decodeJSONLD(t, body)
	if _, ok := doc["temporalCoverage"]; ok {
		t.Error("empty ledger should omit temporalCoverage")
	}
	for _, key := range []string{"name", "description", "url"} {
		if doc[key] == nil || doc[key] == "" {
			t.Errorf("empty ledger JSON-LD omits %s", key)
		}
	}
}

func TestHomeSocialPreview(t *testing.T) {
	srv, _ := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))

	_, _, body := fetch(t, srv, "/", nil)
	image := srv.URL + "/static/images/og-default.png"
	for _, want := range []string{
		`property="og:title"`,
		`property="og:description"`,
		`property="og:url" content="` + srv.URL + `/"`,
		`property="og:image" content="` + image + `"`,
		`name="twitter:card" content="summary_large_image"`,
		`name="twitter:image" content="` + image + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home page is missing %q", want)
		}
	}
}

func TestShareControlAnnouncesThroughToasts(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	entry, err := st.CreateEntry(t.Context(), "deel", "share", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	permalink := "/e/" + strconv.FormatInt(entry.ID, 10)
	canonical := srv.URL + permalink

	_, _, body := fetch(t, srv, permalink, nil)
	for _, want := range []string{
		`<a class="btn btn-primary" href="` + canonical + `"`,
		`data-share-url="` + canonical + `"`,
		`data-share-title="deel → share"`,
		`data-share-text="Entry #` + leftPad4(entry.ID) + ` in the LANCo misspelling ledger."`,
		`<div id="toasts" class="toasts" role="status" aria-live="polite">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("permalink is missing %q", want)
		}
	}

	_, _, js := fetch(t, srv, "/static/js/app.js", nil)
	for _, want := range []string{
		`navigator.share`,
		`AbortError`,
		`"Link copied."`,
		`data-share-url`,
		`document.execCommand("copy")`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js is missing %q", want)
		}
	}
}

func TestOpenSearchDocument(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	entry, err := st.CreateEntry(t.Context(), "zoek", "search", "", nil)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	status, headers, body := fetch(t, srv, "/opensearch.xml", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := headers.Get("Content-Type"); !strings.HasPrefix(got, "application/opensearchdescription+xml") {
		t.Errorf("content type = %q", got)
	}
	if got := headers.Get("Cache-Control"); !strings.Contains(got, "max-age=3600") {
		t.Errorf("Cache-Control = %q", got)
	}
	for _, want := range []string{
		"<OpenSearchDescription",
		"<ShortName>vanDyke</ShortName>",
		"<InputEncoding>UTF-8</InputEncoding>",
		`<Url type="text/html" template="` + srv.URL + `/?q={searchTerms}"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("OpenSearch document is missing %q", want)
		}
	}
	if strings.Contains(body, "Suggestions") {
		t.Error("OpenSearch document advertises a suggestions URL")
	}

	discovery := `<link rel="search" type="application/opensearchdescription+xml" title="vanDyke ledger search" href="/opensearch.xml">`
	for _, path := range []string{"/", "/stats", "/e/" + strconv.FormatInt(entry.ID, 10)} {
		_, _, page := fetch(t, srv, path, nil)
		if !strings.Contains(page, discovery) {
			t.Errorf("%s does not advertise the OpenSearch document", path)
		}
	}
}

func TestHomeSlipAgreesWithPermalinkBadge(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	today, err := slip.New(st).Today(context.Background())
	if err != nil {
		t.Fatalf("slip.Today: %v", err)
	}

	_, _, home := fetch(t, srv, "/", nil)
	section := slipSection(t, home)
	link := `/e/` + strconv.FormatInt(today.Entry.ID, 10)
	if !strings.Contains(section, `href="`+link+`"`) {
		t.Fatalf("home slip does not link to %s", link)
	}
	_, _, permalink := fetch(t, srv, link, nil)
	if !strings.Contains(permalink, "Slip of the day") {
		t.Fatal("the entry the home page calls the slip is not badged")
	}
}
