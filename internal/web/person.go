package web

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/store"
)

// personPageData is the view model for /p/{person}. It embeds pageData so the
// shared masthead, dialogs, footer and the ledger fragment resolve their fields.
type personPageData struct {
	pageData
	Person  string
	Count   int
	FirstAt *time.Time
	LastAt  *time.Time
	FeedURL string
}

// handlePerson serves the ledger scoped to one attributed person. The name is
// matched case-insensitively and canonicalised to the newest spelling, so two
// case variants resolve to one document. An unknown or empty name is 404.
func (w *Web) handlePerson(rw http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("person"))
	if name == "" {
		w.renderPersonNotFound(rw)
		return
	}
	profile, err := w.store.PersonProfile(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		w.renderPersonNotFound(rw)
		return
	}
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	path := "/p/" + url.PathEscape(profile.Person)
	ledger, err := w.buildLedgerWith(r, "", profile.Person, path)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	canonical := w.absoluteURL(r, path)
	first, last := profile.FirstAt, profile.LastAt
	data := personPageData{
		pageData: pageData{
			Umami:         w.umami(),
			Year:          time.Now().Year(),
			Ledger:        ledger,
			ToolbarAction: path,
			Title:         profile.Person + " · vanDyke",
			Description: fmt.Sprintf(
				"%d %s attributed to %s in the LANCo misspelling ledger, from the first on %s to the last on %s.",
				profile.Count, pluralWord(profile.Count, "slip", "slips"), profile.Person,
				first.Format("02 Jan 2006"), last.Format("02 Jan 2006")),
			CanonicalURL: canonical,
			// One near-identical document per attributed name would multiply the
			// index; every row already has an indexed permalink.
			Robots: "noindex,follow",
		},
		Person:  profile.Person,
		Count:   profile.Count,
		FirstAt: &first,
		LastAt:  &last,
		FeedURL: canonical + "/feed.xml",
	}

	// The toolbar and every ledger control target this path, so an HTMX request
	// asks for the same scoped page and gets only the fragment back.
	if r.Header.Get("HX-Request") != "" {
		w.render(rw, http.StatusOK, "ledger", data)
		return
	}
	w.render(rw, http.StatusOK, "person-page", data)
}

func (w *Web) renderPersonNotFound(rw http.ResponseWriter) {
	w.renderErrorPage(rw,
		"Person not found",
		"Nobody by that name has a live slip in the ledger.")
}

// handlePersonFeed serves one person's slips as Atom 1.0. The person is part of
// the feed id, its ETag and its self link, so two persons never share a cache
// entry and a case variant resolves to the same canonical feed.
func (w *Web) handlePersonFeed(rw http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("person"))
	if name == "" {
		w.renderPersonNotFound(rw)
		return
	}
	profile, err := w.store.PersonProfile(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		w.renderPersonNotFound(rw)
		return
	}
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	page, err := w.store.ListEntries(r.Context(), store.ListParams{
		Sort:    "created_at",
		Order:   "desc",
		PerPage: feedLimit(r.URL.Query().Get("limit")),
		Person:  profile.Person,
	})
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	base := w.absoluteURL(r, "")
	feedPath := "/p/" + url.PathEscape(profile.Person) + "/feed.xml"
	updated := time.Now().UTC()
	newestID := int64(0)
	if len(page.Entries) > 0 {
		newestID = page.Entries[0].ID
		updated = page.Entries[0].CreatedAt.UTC()
	}
	tag := personFeedETag(profile.Person, newestID, len(page.Entries))

	rw.Header().Set("ETag", tag)
	rw.Header().Set("Cache-Control", "public, max-age=60")
	if etagMatches(r.Header.Get("If-None-Match"), tag) {
		rw.WriteHeader(http.StatusNotModified)
		return
	}

	doc := buildAtomFeed(base, atomFeedSpec{
		Path:   feedPath,
		ID:     base + feedPath,
		Title:  profile.Person + " · " + feedTitle,
		Author: profile.Person,
	}, updated, page.Entries)
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
