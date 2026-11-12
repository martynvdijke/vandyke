package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"vandyke.thor.edu/vandyke/internal/store"
)

// handleRandom draws one live slip and hands the client to its canonical
// permalink with 303 See Other, so a reload does not reroll and the permalink
// stays the one document crawlers and readers see. photo and person narrow the
// draw; an empty or unmatched ledger is the shared not-found page.
func (w *Web) handleRandom(rw http.ResponseWriter, r *http.Request) {
	hasPhoto := formOptionalBool(r, "photo")
	person := strings.TrimSpace(r.FormValue("person"))

	entry, err := w.store.RandomEntry(r.Context(), person, hasPhoto)
	if errors.Is(err, store.ErrNotFound) {
		w.renderErrorPage(rw,
			"No slip to draw",
			"The ledger is empty, or nothing matches that filter. Add a slip or widen the draw.")
		return
	}
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("X-Robots-Tag", "noindex")
	http.Redirect(rw, r, fmt.Sprintf("/e/%d", entry.ID), http.StatusSeeOther)
}
