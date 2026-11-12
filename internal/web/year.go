package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"vandyke.thor.edu/vandyke/internal/store"
)

// yearMonthBar is one column of the recap's twelve-month chart. Height is a
// percentage of the busiest month, computed here so the template only places
// numbers.
type yearMonthBar struct {
	Month  int
	Name   string
	Count  int
	Height int
}

// yearReviewData is the view model for /y/{yyyy}. It embeds pageData so the
// shared masthead, add dialog, info dialog and footer resolve their fields;
// RecapYear is the year being reviewed while Year stays the current year.
type yearReviewData struct {
	pageData
	RecapYear int
	Total     int

	First *store.Entry
	Last  *store.Entry

	BusiestMonth      string
	BusiestMonthCount int
	Months            []yearMonthBar
	Persons           []personBar
	Longest           *store.Entry

	WithPhoto int
	PhotoPct  int

	LedgerHref string
}

// handleYearReview renders a server-computed recap of one calendar year. The
// ETag hashes the figures because, unlike the append-only feed, an edit inside
// the year changes the page without changing any id.
func (w *Web) handleYearReview(rw http.ResponseWriter, r *http.Request) {
	year, ok := parseYearSegment(r.PathValue("year"))
	if !ok {
		w.renderErrorPage(rw,
			"Year not found",
			"That year is not in the ledger. Pick a year that has at least one slip.")
		return
	}
	review, err := w.store.YearReview(r.Context(), year)
	if errors.Is(err, store.ErrNotFound) {
		w.renderErrorPage(rw,
			"Year not found",
			"That year has no live entries. Pick a year that has at least one slip.")
		return
	}
	if err != nil {
		w.renderInternalError(rw, err)
		return
	}

	etag := yearReviewETag(review)
	rw.Header().Set("ETag", etag)
	rw.Header().Set("Cache-Control", "public, max-age=300")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		rw.WriteHeader(http.StatusNotModified)
		return
	}
	w.render(rw, http.StatusOK, "year", buildYearReviewPage(w, r, review))
}

// parseYearSegment accepts exactly four digits, so /y/24 and /y/abcd are
// malformed rather than silently coerced.
func parseYearSegment(raw string) (int, bool) {
	if len(raw) != 4 {
		return 0, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	year, err := strconv.Atoi(raw)
	if err != nil || year < 1000 || year > 9999 {
		return 0, false
	}
	return year, true
}

func buildYearReviewPage(w *Web, r *http.Request, review store.YearReview) yearReviewData {
	peak := 0
	for _, month := range review.Months {
		if month.Count > peak {
			peak = month.Count
		}
	}
	months := make([]yearMonthBar, 0, len(review.Months))
	for _, month := range review.Months {
		height := 0
		if peak > 0 && month.Count > 0 {
			height = (month.Count*100 + peak/2) / peak
			if height < 5 {
				height = 5
			}
		}
		months = append(months, yearMonthBar{
			Month:  month.Month,
			Name:   time.Month(month.Month).String(),
			Count:  month.Count,
			Height: height,
		})
	}

	busiest := "—"
	if review.BusiestMonth >= 1 && review.BusiestMonth <= 12 {
		busiest = time.Month(review.BusiestMonth).String()
	}

	title := fmt.Sprintf("%d in review · vanDyke", review.Year)
	description := fmt.Sprintf(
		"The %d ledger in review: %d %s, the busiest month, the five most frequent names and the longest misspelling.",
		review.Year, review.Total, pluralWord(review.Total, "slip", "slips"))

	return yearReviewData{
		pageData: pageData{
			Umami:        w.umami(),
			Year:         time.Now().Year(),
			Title:        title,
			Description:  description,
			CanonicalURL: w.absoluteURL(r, fmt.Sprintf("/y/%d", review.Year)),
		},
		RecapYear:         review.Year,
		Total:             review.Total,
		First:             review.FirstEntry,
		Last:              review.LastEntry,
		BusiestMonth:      busiest,
		BusiestMonthCount: review.BusiestMonthCount,
		Months:            months,
		Persons:           buildPersonBars(review.TopPersons),
		Longest:           review.Longest,
		WithPhoto:         review.WithPhoto,
		PhotoPct:          review.PhotoPct,
		LedgerHref:        fmt.Sprintf("/entries?year=%d", review.Year),
	}
}

// yearReviewETag hashes the computed figures. JSON marshalling is
// deterministic for this struct (no maps), so the same data always yields the
// same tag.
func yearReviewETag(review store.YearReview) string {
	body, err := json.Marshal(review)
	if err != nil {
		body = []byte(fmt.Sprintf("%d-%d", review.Year, review.Total))
	}
	sum := sha256.Sum256(body)
	return fmt.Sprintf(`"year-%04d-%s"`, review.Year, hex.EncodeToString(sum[:])[:16])
}

// pluralWord picks a singular or plural noun for generated copy.
func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
