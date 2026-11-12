package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/store"
)

// handleTagsIndex lists the whole curated vocabulary with live counts. A tag
// with no live entry is still listed: this is the taxonomy index, and a freshly
// created tag must be reachable.
func (w *Web) handleTagsIndex(rw http.ResponseWriter, r *http.Request) {
	tags, err := w.store.ListTags(r.Context())
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	w.render(rw, http.StatusOK, "tags-page", pageData{
		Umami:        w.umami(),
		Tags:         tags,
		Title:        "Tags · vanDyke",
		Description:  "The curated vocabulary of the vanDyke misspelling ledger: every kind of slip, with the number of entries that carry it.",
		CanonicalURL: w.absoluteURL(r, "/tags"),
		Year:         time.Now().Year(),
	})
}

// handleTagPage renders the ledger filtered to one tag. An unknown slug is a
// 404 because this is a page for a named resource; an unknown tag on the filter
// views is a 200 with an empty result instead.
func (w *Web) handleTagPage(rw http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.PathValue("slug"))
	tag, err := w.store.GetTagBySlug(r.Context(), slug)
	if errors.Is(err, store.ErrNotFound) {
		w.renderErrorPage(rw,
			"Tag not found",
			"That tag is not in the vocabulary. It may have been renamed or deleted.")
		return
	}
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	ledger, err := w.buildLedgerWithTag(r, tag.Slug)
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}
	description := fmt.Sprintf("Every slip tagged %s in the LANCo misspelling ledger.", tag.Name)
	if ledger.Total == 0 {
		description = fmt.Sprintf("No live entry currently carries the %s tag in the LANCo misspelling ledger.", tag.Name)
	}
	w.render(rw, http.StatusOK, "tag-page", pageData{
		Umami:         w.umami(),
		Ledger:        ledger,
		Tag:           tag,
		ToolbarAction: "/entries",
		Title:         tag.Name + " · vanDyke",
		Description:   description,
		CanonicalURL:  w.absoluteURL(r, "/tags/"+tag.Slug),
		Year:          time.Now().Year(),
	})
}
