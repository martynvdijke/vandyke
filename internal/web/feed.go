package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/store"
)

const (
	feedDefaultLimit = 50
	feedTitle        = "The vanDyke Corrections Ledger"
	atomNamespace    = "http://www.w3.org/2005/Atom"
)

// atomFeed is the Atom 1.0 document. encoding/xml escapes every text field.
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Xmlns   string      `xml:"xmlns,attr"`
	ID      string      `xml:"id"`
	Title   string      `xml:"title"`
	Updated string      `xml:"updated"`
	Author  atomAuthor  `xml:"author"`
	Link    atomLink    `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}

type atomEntry struct {
	ID      string      `xml:"id"`
	Title   string      `xml:"title"`
	Summary string      `xml:"summary"`
	Updated string      `xml:"updated"`
	Link    atomLink    `xml:"link"`
	Author  *atomAuthor `xml:"author,omitempty"`
}

// handleFeed serves the newest live entries as Atom 1.0 with a strong ETag and
// a short public cache lifetime. One or more tag slugs filter the items, and
// the tag is folded into the feed id, the self link and the ETag so the tagged
// feeds never share a cache entry.
func (w *Web) handleFeed(rw http.ResponseWriter, r *http.Request) {
	limit := feedLimit(r.URL.Query().Get("limit"))
	tags := feedTags(r.URL.Query()["tag"])
	page, err := w.store.ListEntries(r.Context(), store.ListParams{
		Sort:    "created_at",
		Order:   "desc",
		PerPage: limit,
		Tags:    tags,
	})
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	base := w.absoluteURL(r, "")
	feedPath := feedPathFor(tags)
	updated := time.Now().UTC()
	newestID := int64(0)
	if len(page.Entries) > 0 {
		newestID = page.Entries[0].ID
		updated = page.Entries[0].CreatedAt.UTC()
	}
	tag := feedETag(tags, newestID, len(page.Entries))

	rw.Header().Set("ETag", tag)
	rw.Header().Set("Cache-Control", "public, max-age=60")
	if etagMatches(r.Header.Get("If-None-Match"), tag) {
		rw.WriteHeader(http.StatusNotModified)
		return
	}

	doc := buildAtomFeed(base, atomFeedSpec{Path: feedPath}, updated, page.Entries)
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	rw.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	rw.WriteHeader(http.StatusOK)
	_, _ = rw.Write([]byte(xml.Header))
	_, _ = rw.Write(body)
	_, _ = rw.Write([]byte("\n"))
}

// atomFeedSpec carries the feed-level fields the site feed and a scoped feed do
// not share. Path is the request path appended to the base for the self link;
// ID overrides the feed id when a scoped feed needs its own identity; Title and
// Author fall back to the ledger-wide wording.
type atomFeedSpec struct {
	Path   string
	ID     string
	Title  string
	Author string
}

// buildAtomFeed maps ledger entries onto the Atom document. The feed-level
// author keeps the document valid when an entry has no attributed person.
func buildAtomFeed(base string, spec atomFeedSpec, updated time.Time, entries []store.Entry) atomFeed {
	self := base + spec.Path
	id := spec.ID
	if id == "" {
		id = self
	}
	title := spec.Title
	if title == "" {
		title = feedTitle
	}
	author := spec.Author
	if author == "" {
		author = feedTitle
	}
	feed := atomFeed{
		Xmlns:   atomNamespace,
		ID:      id,
		Title:   title,
		Updated: updated.UTC().Format(time.RFC3339),
		Author:  atomAuthor{Name: author},
		Link:    atomLink{Href: self, Rel: "self", Type: "application/atom+xml"},
	}
	for _, entry := range entries {
		permalink := fmt.Sprintf("%s/e/%d", base, entry.ID)
		item := atomEntry{
			ID:      permalink,
			Title:   entry.Word,
			Summary: entry.Meaning,
			Updated: entry.CreatedAt.UTC().Format(time.RFC3339),
			Link:    atomLink{Href: permalink},
		}
		if entry.Person != "" {
			item.Author = &atomAuthor{Name: entry.Person}
		}
		feed.Entries = append(feed.Entries, item)
	}
	return feed
}

// feedPathFor builds the feed's own path, query included, so the id and self
// link name exactly the requested view.
func feedPathFor(tags []string) string {
	if len(tags) == 0 {
		return "/feed.xml"
	}
	values := url.Values{}
	for _, tag := range tags {
		values.Add("tag", tag)
	}
	return "/feed.xml?" + values.Encode()
}

// feedTags trims, drops empties and de-duplicates the tag filter.
func feedTags(values []string) []string {
	var tags []string
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags
}

// feedETag keeps the historical untagged format and adds a hash of the tag
// list otherwise. The hash keeps arbitrary query text out of the header while
// still making each tag view cache separately.
func feedETag(tags []string, newestID int64, count int) string {
	if len(tags) == 0 {
		return fmt.Sprintf(`"feed-%d-%d"`, newestID, count)
	}
	sum := sha256.Sum256([]byte(strings.Join(tags, "\x00")))
	return fmt.Sprintf(`"feed-%s-%d-%d"`, hex.EncodeToString(sum[:])[:8], newestID, count)
}

// personFeedETag derives a strong tag from the person, the newest entry id and
// the returned count, so two persons never share a cache entry even when their
// feeds happen to hold identical rows.
func personFeedETag(person string, newestID int64, count int) string {
	sum := sha256.Sum256([]byte(strings.ToLower(person)))
	return fmt.Sprintf(`"feed-p-%s-%d-%d"`, hex.EncodeToString(sum[:])[:8], newestID, count)
}

// feedLimit clamps the query parameter: default 50 for a non-numeric or sub-1
// value, at most store.MaxPerPage.
func feedLimit(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return feedDefaultLimit
	}
	if n > store.MaxPerPage {
		return store.MaxPerPage
	}
	return n
}

// etagMatches reports whether an If-None-Match header names tag. Weak prefixes
// and "*" are accepted.
func etagMatches(header, tag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == tag || candidate == "*" {
			return true
		}
	}
	return false
}
