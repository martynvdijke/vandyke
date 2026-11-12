package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// MonthCount is one calendar month and how many live entries it holds.
type MonthCount struct {
	Month int `json:"month"`
	Count int `json:"count"`
}

// YearReview is the computed recap of one calendar year. Every figure is read
// from the entries table on demand; nothing is stored.
type YearReview struct {
	Year              int           `json:"year"`
	Total             int           `json:"total"`
	FirstEntry        *Entry        `json:"first_entry,omitempty"`
	LastEntry         *Entry        `json:"last_entry,omitempty"`
	Months            []MonthCount  `json:"months"`
	BusiestMonth      int           `json:"busiest_month"`
	BusiestMonthCount int           `json:"busiest_month_count"`
	TopPersons        []PersonCount `json:"top_persons"`
	Longest           *Entry        `json:"longest,omitempty"`
	WithPhoto         int           `json:"with_photo"`
	PhotoPct          int           `json:"photo_pct"`
}

// YearReview aggregates one calendar year of live entries. It returns
// ErrNotFound when the year holds no live entries, so the handler can answer
// with the ordinary not-found page instead of a recap of zeroes. Ties are
// broken deterministically: the earliest month wins, persons sort by count
// then name, and the longest misspelling sorts by rune length, then the
// earliest created_at, then the lowest id.
func (s *Store) YearReview(ctx context.Context, year int) (YearReview, error) {
	if year < 1000 || year > 9999 {
		return YearReview{}, ErrNotFound
	}
	start := fmt.Sprintf("%04d-01-01T00:00:00Z", year)
	end := fmt.Sprintf("%04d-01-01T00:00:00Z", year+1)

	review := YearReview{Year: year, Months: make([]MonthCount, 12)}
	for i := range review.Months {
		review.Months[i].Month = i + 1
	}

	var (
		first string
		last  string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MIN(created_at), ''), COALESCE(MAX(created_at), '')
		FROM entries
		WHERE deleted_at IS NULL AND created_at >= ? AND created_at < ?`,
		start, end,
	).Scan(&review.Total, &first, &last)
	if err != nil {
		return YearReview{}, fmt.Errorf("query year totals: %w", err)
	}
	if review.Total == 0 {
		return YearReview{}, ErrNotFound
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entry_photos p JOIN entries e ON e.id = p.entry_id
		WHERE e.deleted_at IS NULL AND e.created_at >= ? AND e.created_at < ?`,
		start, end,
	).Scan(&review.WithPhoto); err != nil {
		return YearReview{}, fmt.Errorf("count year photos: %w", err)
	}
	review.PhotoPct = (review.WithPhoto*100 + review.Total/2) / review.Total

	monthRows, err := s.db.QueryContext(ctx,
		`SELECT CAST(strftime('%m', created_at) AS INTEGER) AS month, COUNT(*)
		FROM entries
		WHERE deleted_at IS NULL AND created_at >= ? AND created_at < ?
		GROUP BY month`,
		start, end,
	)
	if err != nil {
		return YearReview{}, fmt.Errorf("query year months: %w", err)
	}
	for monthRows.Next() {
		var (
			month int
			count int
		)
		if err := monthRows.Scan(&month, &count); err != nil {
			monthRows.Close()
			return YearReview{}, fmt.Errorf("scan year month: %w", err)
		}
		if month >= 1 && month <= 12 {
			review.Months[month-1].Count = count
		}
	}
	if err := monthRows.Err(); err != nil {
		monthRows.Close()
		return YearReview{}, fmt.Errorf("iterate year months: %w", err)
	}
	monthRows.Close()
	for _, month := range review.Months {
		if month.Count > review.BusiestMonthCount {
			review.BusiestMonth = month.Month
			review.BusiestMonthCount = month.Count
		}
	}

	personRows, err := s.db.QueryContext(ctx,
		`SELECT person, COUNT(*)
		FROM entries
		WHERE person <> '' AND deleted_at IS NULL AND created_at >= ? AND created_at < ?
		GROUP BY person COLLATE NOCASE
		ORDER BY COUNT(*) DESC, person COLLATE NOCASE ASC
		LIMIT 5`,
		start, end,
	)
	if err != nil {
		return YearReview{}, fmt.Errorf("query year persons: %w", err)
	}
	for personRows.Next() {
		var pc PersonCount
		if err := personRows.Scan(&pc.Person, &pc.Count); err != nil {
			personRows.Close()
			return YearReview{}, fmt.Errorf("scan year person: %w", err)
		}
		review.TopPersons = append(review.TopPersons, pc)
	}
	if err := personRows.Err(); err != nil {
		personRows.Close()
		return YearReview{}, fmt.Errorf("iterate year persons: %w", err)
	}
	personRows.Close()

	// Reuse ListEntries so visibility and ordering stay in one place.
	ascending, err := s.ListEntries(ctx, ListParams{
		Year: year, Sort: "created_at", Order: "asc", PerPage: 1,
	})
	if err != nil {
		return YearReview{}, err
	}
	if len(ascending.Entries) > 0 {
		firstEntry := ascending.Entries[0]
		review.FirstEntry = &firstEntry
	}
	descending, err := s.ListEntries(ctx, ListParams{
		Year: year, Sort: "created_at", Order: "desc", PerPage: 1,
	})
	if err != nil {
		return YearReview{}, err
	}
	if len(descending.Entries) > 0 {
		lastEntry := descending.Entries[0]
		review.LastEntry = &lastEntry
	}

	// No list sort orders by length, so the longest misspelling needs its own
	// query. SQLite length() counts characters for text.
	row := s.db.QueryRowContext(ctx,
		`SELECT e.id, e.word, e.meaning, e.person, e.created_at, e.deleted_at,
			EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)
		FROM entries e
		WHERE e.deleted_at IS NULL AND e.word <> '' AND e.created_at >= ? AND e.created_at < ?
		ORDER BY length(e.word) DESC, e.created_at ASC, e.id ASC
		LIMIT 1`,
		start, end,
	)
	longest, err := scanEntry(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// A photo-only or empty-word year has no misspelling to name.
	case err != nil:
		return YearReview{}, fmt.Errorf("query longest misspelling: %w", err)
	default:
		review.Longest = &longest
	}

	return review, nil
}
