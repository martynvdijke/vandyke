package web

import (
	"encoding/xml"
	"fmt"
	"net/http"
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
// a short public cache lifetime.
func (w *Web) handleFeed(rw http.ResponseWriter, r *http.Request) {
	limit := feedLimit(r.URL.Query().Get("limit"))
	page, err := w.store.ListEntries(r.Context(), store.ListParams{
		Sort:    "created_at",
		Order:   "desc",
		PerPage: limit,
	})
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	base := w.absoluteURL(r, "")
	updated := time.Now().UTC()
	newestID := int64(0)
	if len(page.Entries) > 0 {
		newestID = page.Entries[0].ID
		updated = page.Entries[0].CreatedAt.UTC()
	}
	tag := fmt.Sprintf(`"feed-%d-%d"`, newestID, len(page.Entries))

	rw.Header().Set("ETag", tag)
	rw.Header().Set("Cache-Control", "public, max-age=60")
	if etagMatches(r.Header.Get("If-None-Match"), tag) {
		rw.WriteHeader(http.StatusNotModified)
		return
	}

	doc := buildAtomFeed(base, updated, page.Entries)
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

// buildAtomFeed maps ledger entries onto the Atom document. The feed-level
// author keeps the document valid when an entry has no attributed person.
func buildAtomFeed(base string, updated time.Time, entries []store.Entry) atomFeed {
	feed := atomFeed{
		Xmlns:   atomNamespace,
		ID:      base + "/feed.xml",
		Title:   feedTitle,
		Updated: updated.UTC().Format(time.RFC3339),
		Author:  atomAuthor{Name: feedTitle},
		Link:    atomLink{Href: base + "/feed.xml", Rel: "self", Type: "application/atom+xml"},
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
