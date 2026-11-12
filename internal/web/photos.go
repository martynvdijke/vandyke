package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vandyke.thor.edu/vandyke/internal/store"
)

// photosPerPage is the gallery batch size. It is deliberately smaller than a
// ledger page because every tile costs a thumbnail fetch.
const photosPerPage = 24

// photoTile is one gallery tile. The image source is always the thumb variant;
// the display variant lives only on the anchor and in the lightbox attributes.
type photoTile struct {
	Alt        string
	Caption    string
	ThumbURL   string
	DisplayURL string
}

// photosPageData is the view model for /photos and for the HTMX batch fragment.
type photosPageData struct {
	pageData
	Tiles   []photoTile
	Total   int
	NextURL string
	PrevURL string
	Cursor  bool
}

// handlePhotos serves the cursor-paged gallery of entries that have a photo.
// A document request for a cursor URL is a complete, navigable page; an
// HX-Request gets only the next batch, and Vary keeps the two apart in caches.
func (w *Web) handlePhotos(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Vary", "HX-Request")

	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	batch, err := w.store.ListPhotoEntries(r.Context(), cursor, photosPerPage)
	if errors.Is(err, store.ErrInvalidCursor) {
		w.renderStatusErrorPage(rw, http.StatusBadRequest,
			"Bad gallery cursor",
			"That gallery cursor cannot be read. Start again from the beginning of the gallery.")
		return
	}
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	tiles := make([]photoTile, 0, len(batch.Entries))
	for _, entry := range batch.Entries {
		tiles = append(tiles, photoTile{
			Alt:        photoAlt(entry),
			Caption:    photoCaption(entry),
			ThumbURL:   entry.PhotoURL + "?size=thumb",
			DisplayURL: entry.PhotoURL + "?size=display",
		})
	}

	data := photosPageData{
		pageData: pageData{
			Umami:        w.umami(),
			Year:         time.Now().Year(),
			Title:        "Photographs · vanDyke",
			Description:  "The photographic side of the LANCo ledger: every slip that was captured with a camera instead of a pen, newest first.",
			CanonicalURL: w.absoluteURL(r, "/photos"),
		},
		Tiles:  tiles,
		Total:  batch.Total,
		Cursor: cursor != "",
	}
	if batch.Next != nil {
		data.NextURL = photosURL(batch.Next.Token())
	}
	if batch.Prev != nil {
		data.PrevURL = photosURL(batch.Prev.Token())
	} else if data.Cursor {
		data.PrevURL = "/photos"
	}
	// A cursor variant is a filtered view; the bare gallery is canonical and
	// the only document worth indexing.
	if data.Cursor {
		data.Robots = "noindex,follow"
	}

	if r.Header.Get("HX-Request") != "" {
		w.render(rw, http.StatusOK, "photos-batch", data)
		return
	}
	w.render(rw, http.StatusOK, "photos-page", data)
}

func photosURL(token string) string {
	return "/photos?cursor=" + url.QueryEscape(token)
}

// photoAlt derives a tile's alt text from the entry: the correction line when
// both texts exist, the word alone otherwise, and the entry number when the
// slip is only a photograph.
func photoAlt(entry store.Entry) string {
	switch {
	case entry.Word != "" && entry.Meaning != "":
		return entry.Word + " → " + entry.Meaning
	case entry.Word != "":
		return entry.Word
	default:
		return "Photograph attached to entry " + photoEntryNo(entry.ID)
	}
}

// photoCaption names the entry, its date and its person, and says plainly when
// a photographic slip carries no text.
func photoCaption(entry store.Entry) string {
	parts := []string{"Entry " + photoEntryNo(entry.ID), entry.CreatedAt.Format("02 Jan 2006")}
	if entry.Person != "" {
		parts = append(parts, entry.Person)
	}
	caption := strings.Join(parts, " · ")
	if entry.Word == "" && entry.Meaning == "" {
		caption += " · no text recorded"
	}
	return caption
}

func photoEntryNo(id int64) string {
	return fmt.Sprintf("#%04d", id)
}
