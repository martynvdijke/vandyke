package web

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
)

func makePhotoEntryAt(t *testing.T, st *store.Store, word, meaning, person string, at time.Time) store.Entry {
	t.Helper()
	ctx := context.Background()
	photo, err := store.NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(ctx, word, meaning, person, photo)
	if err != nil {
		t.Fatalf("CreateEntry photo %q: %v", word, err)
	}
	_, updated, err := st.UpdateEntry(ctx, store.EntryUpdate{
		ID: entry.ID, Word: word, Meaning: meaning, Person: person, CreatedAt: at,
	})
	if err != nil {
		t.Fatalf("UpdateEntry photo %q: %v", word, err)
	}
	return updated
}

var (
	gallerySentinelRe = regexp.MustCompile(`<a class="gallery-sentinel" href="([^"]+)"`)
	galleryImageRe    = regexp.MustCompile(`<img class="gallery-img" src="([^"]+)"`)
	galleryDisplayRe  = regexp.MustCompile(`data-lightbox-src="/api/v1/entries/(\d+)/photo\?size=display"`)
	galleryPrevRe     = regexp.MustCompile(`href="([^"]+)" rel="prev"`)
)

func photoIDs(t *testing.T, body string) []int64 {
	t.Helper()
	var ids []int64
	for _, match := range galleryDisplayRe.FindAllStringSubmatch(body, -1) {
		var id int64
		if _, err := fmt.Sscanf(match[1], "%d", &id); err != nil {
			t.Fatalf("parse photo id %q: %v", match[1], err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestPhotosGalleryDocumentAndFragment(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		makePhotoEntryAt(t, st, fmt.Sprintf("foto%02d", i), "photo", "", base.Add(time.Duration(i)*time.Minute))
	}
	// One photographic slip with no text and one trashed photo.
	textless := makePhotoEntryAt(t, st, "", "", "", base.Add(40*time.Minute))
	trashed := makePhotoEntryAt(t, st, "verdwenen", "gone", "", base.Add(41*time.Minute))
	if _, err := st.SoftDeleteEntries(context.Background(), []int64{trashed.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	// Photo-less live entries are not photographs.
	makeEntry(t, st, "zonder", "photo", "", "")

	status, headers, body := fetch(t, srv, "/photos", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got := headers.Get("Vary"); !strings.Contains(got, "HX-Request") {
		t.Errorf("Vary = %q, want HX-Request", got)
	}
	if got := strings.Count(body, `class="gallery-tile"`); got != photosPerPage {
		t.Errorf("first batch tiles = %d, want %d", got, photosPerPage)
	}
	if !strings.Contains(body, "31 photographs are in the ledger") {
		t.Errorf("gallery total statement missing: %s", body)
	}
	if strings.Contains(body, fmt.Sprintf("/api/v1/entries/%d/photo", trashed.ID)) {
		t.Error("trashed photo appears in the gallery")
	}
	if !strings.Contains(body, `alt="foto29 → photo"`) {
		t.Error("worded tile alt text is not word → meaning")
	}
	if !strings.Contains(body, fmt.Sprintf(`alt="Photograph attached to entry #%04d"`, textless.ID)) {
		t.Error("textless tile alt text is not the entry number")
	}
	if !strings.Contains(body, "no text recorded") {
		t.Error("textless tile caption does not say the slip has no text")
	}

	// Every grid image uses the thumb variant only; display URLs live on the
	// anchors and in the lightbox attributes.
	images := galleryImageRe.FindAllStringSubmatch(body, -1)
	if len(images) != photosPerPage {
		t.Fatalf("grid images = %d, want %d", len(images), photosPerPage)
	}
	for _, match := range images {
		if !strings.HasSuffix(match[1], "?size=thumb") {
			t.Errorf("grid image source = %q, want the thumb variant", match[1])
		}
		if strings.Contains(match[1], "size=display") {
			t.Errorf("grid image source uses the display variant: %q", match[1])
		}
	}

	sentinel := gallerySentinelRe.FindStringSubmatch(body)
	if sentinel == nil {
		t.Fatalf("first batch has no sentinel: %s", body)
	}
	nextURL := sentinel[1]
	if !strings.HasPrefix(nextURL, "/photos?cursor=") {
		t.Errorf("sentinel href = %q, want a cursor URL", nextURL)
	}
	if !strings.Contains(body, `hx-get="`+nextURL+`"`) || !strings.Contains(body, `hx-trigger="revealed"`) {
		t.Error("sentinel is not an HTMX reveal target")
	}
	if !strings.Contains(body, `data-lightbox-src=`) || !strings.Contains(body, `id="photo-lightbox"`) {
		t.Error("the document has no lightbox wiring")
	}
	if !strings.Contains(body, `href="`+nextURL+`" rel="next"`) {
		t.Error("first document has no rel=next link to the following batch")
	}

	// An HTMX request gets only the batch fragment.
	status, fragmentHeaders, fragment := fetch(t, srv, nextURL, map[string]string{"HX-Request": "true"})
	if status != http.StatusOK {
		t.Fatalf("fragment status = %d", status)
	}
	if got := fragmentHeaders.Get("Vary"); !strings.Contains(got, "HX-Request") {
		t.Errorf("fragment Vary = %q", got)
	}
	if strings.Contains(fragment, "<!doctype html>") || strings.Contains(fragment, "<html") || strings.Contains(fragment, `class="masthead"`) || strings.Contains(fragment, `class="footer"`) {
		t.Error("HTMX response contains a document shell")
	}
	if !strings.Contains(fragment, `class="gallery-tile"`) {
		t.Error("HTMX response has no tiles")
	}
	if !strings.Contains(fragment, "You have reached the end of the gallery") {
		t.Error("last HTMX batch has no closing line")
	}
	if strings.Contains(fragment, `id="photo-lightbox"`) {
		t.Error("HTMX fragment duplicated the lightbox dialog")
	}

	// A plain navigation to the same cursor URL is a complete document.
	status, documentHeaders, document := fetch(t, srv, nextURL, nil)
	if status != http.StatusOK {
		t.Fatalf("document status = %d", status)
	}
	if got := documentHeaders.Get("Vary"); !strings.Contains(got, "HX-Request") {
		t.Errorf("document Vary = %q", got)
	}
	if !strings.HasPrefix(document, "<!doctype html>") || !strings.Contains(document, `class="masthead"`) || !strings.Contains(document, `class="footer"`) {
		t.Error("cursor navigation is not a complete document")
	}
	if !strings.Contains(document, `rel="prev"`) {
		t.Error("cursor document has no prev link")
	}
	if !strings.Contains(document, `href="/photos"`) {
		t.Error("cursor document does not link back to the start")
	}
	if !strings.Contains(document, `<link rel="canonical" href="`+srv.URL+`/photos">`) {
		t.Error("cursor document canonical is not the bare gallery")
	}
	if !strings.Contains(document, `<meta name="robots" content="noindex,follow">`) {
		t.Error("cursor document is indexable")
	}
	if got := len(photoIDs(t, document)); got != 7 {
		t.Errorf("last batch tiles = %d, want 7", got)
	}
	if !strings.Contains(document, "You have reached the end of the gallery") {
		t.Error("last document does not state that the gallery ended")
	}
	if strings.Contains(document, `class="gallery-sentinel"`) {
		t.Error("last document still offers a sentinel")
	}

	status, _, malformed := fetch(t, srv, "/photos?cursor=not-a-cursor", nil)
	if status != http.StatusBadRequest {
		t.Errorf("malformed cursor status = %d, want 400", status)
	}
	if strings.Contains(malformed, `class="gallery-tile"`) {
		t.Error("malformed cursor rendered tiles")
	}
}

func TestPhotosGalleryEmpty(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)
	makeEntry(t, st, "zonder", "photo", "", "")

	status, headers, body := fetch(t, srv, "/photos", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got := headers.Get("Vary"); !strings.Contains(got, "HX-Request") {
		t.Errorf("Vary = %q", got)
	}
	if !strings.Contains(body, "No photographs yet") {
		t.Error("empty gallery message missing")
	}
	if strings.Contains(body, `class="gallery-grid"`) {
		t.Error("empty gallery rendered a grid")
	}
	if strings.Contains(body, `<meta name="robots" content="noindex,follow">`) {
		t.Error("the bare gallery is noindex")
	}
	if !strings.Contains(body, `<link rel="canonical" href="`+srv.URL+`/photos">`) {
		t.Error("bare gallery has no canonical")
	}
}

func TestPhotosCursorWalksEveryTileOnceAndLinksBatches(t *testing.T) {
	srv, st := newTestWebWithStore(t, config.Umami{}, ratelimit.New(1000, 100))
	clearLedger(t, st)

	base := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	// 23 newer rows, two sharing a timestamp, then six older rows: the shared
	// pair straddles the first cursor boundary.
	for i := 0; i < 23; i++ {
		makePhotoEntryAt(t, st, fmt.Sprintf("nieuw%02d", i), "new", "", base.Add(time.Duration(i+1)*time.Minute))
	}
	shared := base.Add(30 * time.Minute)
	makePhotoEntryAt(t, st, "gelijk-laag", "same", "", shared)
	makePhotoEntryAt(t, st, "gelijk-hoog", "same", "", shared)
	for i := 0; i < 6; i++ {
		makePhotoEntryAt(t, st, fmt.Sprintf("oud%02d", i), "old", "", base.Add(-time.Duration(i+1)*time.Minute))
	}

	var (
		seen  = make(map[int64]int)
		pages [][]int64
		urls  []string
		next  = "/photos"
	)
	for len(urls) < 5 && next != "" {
		status, _, body := fetch(t, srv, next, nil)
		if status != http.StatusOK {
			t.Fatalf("batch %d status = %d", len(urls), status)
		}
		urls = append(urls, next)
		ids := photoIDs(t, body)
		pages = append(pages, ids)
		for _, id := range ids {
			seen[id]++
		}
		if match := gallerySentinelRe.FindStringSubmatch(body); match != nil {
			next = match[1]
		} else {
			next = ""
		}
	}

	if len(pages) != 2 {
		t.Fatalf("walked %d batches, want 2", len(pages))
	}
	if len(seen) != 31 {
		t.Errorf("distinct tiles = %d, want 31", len(seen))
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("tile %d appeared %d times", id, count)
		}
	}

	// The second document's prev link is the start of the gallery, because the
	// batch above it is the first one.
	_, _, second := fetch(t, srv, urls[1], nil)
	match := galleryPrevRe.FindStringSubmatch(second)
	if match == nil {
		t.Fatal("second batch has no prev link")
	}
	if match[1] != "/photos" {
		t.Errorf("second batch prev = %q, want /photos", match[1])
	}

	// A third batch links back to the second, not to the start.
	for i := 0; i < 24; i++ {
		makePhotoEntryAt(t, st, fmt.Sprintf("extra%02d", i), "extra", "", base.Add(time.Duration(100+i)*time.Minute))
	}
	_, _, first := fetch(t, srv, "/photos", nil)
	firstSentinel := gallerySentinelRe.FindStringSubmatch(first)
	if firstSentinel == nil {
		t.Fatal("extended gallery has no first sentinel")
	}
	_, _, secondDoc := fetch(t, srv, firstSentinel[1], nil)
	secondSentinel := gallerySentinelRe.FindStringSubmatch(secondDoc)
	if secondSentinel == nil {
		t.Fatal("extended gallery has no second sentinel")
	}
	_, _, thirdDoc := fetch(t, srv, secondSentinel[1], nil)
	prev := galleryPrevRe.FindStringSubmatch(thirdDoc)
	if prev == nil {
		t.Fatal("third batch has no prev link")
	}
	_, _, prevDoc := fetch(t, srv, prev[1], nil)
	prevIDs := photoIDs(t, prevDoc)
	wantIDs := photoIDs(t, secondDoc)
	if len(prevIDs) != len(wantIDs) {
		t.Fatalf("prev batch tiles = %d, want %d", len(prevIDs), len(wantIDs))
	}
	for i := range prevIDs {
		if prevIDs[i] != wantIDs[i] {
			t.Fatalf("prev batch ids = %v, want %v", prevIDs, wantIDs)
		}
	}
}
