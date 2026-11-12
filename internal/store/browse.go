package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidCursor reports a photo-gallery cursor that cannot be decoded.
var ErrInvalidCursor = errors.New("invalid cursor")

// WordSummary is one case-insensitive spelling group in the word index: the
// spelling of its newest occurrence, how many live entries carry it, the first
// and last occurrence dates and the permalink id of the newest occurrence.
type WordSummary struct {
	Word     string
	Count    int
	FirstAt  time.Time
	LastAt   time.Time
	NewestID int64
}

// WordIndex is the browse index: one summary per live non-empty spelling group
// plus the number of live entries whose word is empty (the photo-only slips).
type WordIndex struct {
	Words     []WordSummary
	PhotoOnly int
}

// WordIndex groups every live non-empty word case-insensitively, exactly the
// folding the unique (word, meaning) index enforces. The newest occurrence
// supplies the displayed spelling and the permalink target.
func (s *Store) WordIndex(ctx context.Context) (WordIndex, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH grouped AS (
			SELECT word, id, created_at,
				COUNT(*) OVER (PARTITION BY word COLLATE NOCASE) AS n,
				MIN(created_at) OVER (PARTITION BY word COLLATE NOCASE) AS first_at,
				MAX(created_at) OVER (PARTITION BY word COLLATE NOCASE) AS last_at,
				ROW_NUMBER() OVER (
					PARTITION BY word COLLATE NOCASE
					ORDER BY created_at DESC, id DESC) AS rank
			FROM entries
			WHERE deleted_at IS NULL AND word <> ''
		)
		SELECT word, n, first_at, last_at, id
		FROM grouped
		WHERE rank = 1
		ORDER BY word COLLATE NOCASE`)
	if err != nil {
		return WordIndex{}, fmt.Errorf("query word index: %w", err)
	}
	defer func() { _ = rows.Close() }()

	index := WordIndex{Words: make([]WordSummary, 0)}
	for rows.Next() {
		var (
			summary         WordSummary
			firstAt, lastAt string
		)
		if err := rows.Scan(&summary.Word, &summary.Count, &firstAt, &lastAt, &summary.NewestID); err != nil {
			return WordIndex{}, fmt.Errorf("scan word summary: %w", err)
		}
		if summary.FirstAt, err = parseStoredTime(firstAt); err != nil {
			return WordIndex{}, err
		}
		if summary.LastAt, err = parseStoredTime(lastAt); err != nil {
			return WordIndex{}, err
		}
		index.Words = append(index.Words, summary)
	}
	if err := rows.Err(); err != nil {
		return WordIndex{}, fmt.Errorf("iterate word index: %w", err)
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entries WHERE deleted_at IS NULL AND word = ''`,
	).Scan(&index.PhotoOnly); err != nil {
		return WordIndex{}, fmt.Errorf("count photo-only entries: %w", err)
	}
	return index, nil
}

// PersonProfile is the canonical view of one case-insensitive person group: the
// spelling of the newest occurrence, the entry tally and the first and last slip.
type PersonProfile struct {
	Person  string
	Count   int
	FirstAt time.Time
	LastAt  time.Time
}

// PersonProfile groups a person's live entries case-insensitively, the way
// PersonCounts does, and reports ErrNotFound when no live entry matches.
func (s *Store) PersonProfile(ctx context.Context, name string) (PersonProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return PersonProfile{}, ErrNotFound
	}
	var (
		profile         PersonProfile
		firstAt, lastAt sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		WITH matched AS (
			SELECT id, person, created_at
			FROM entries
			WHERE deleted_at IS NULL AND person <> '' AND person = ? COLLATE NOCASE
		)
		SELECT COALESCE(
				(SELECT person FROM matched ORDER BY created_at DESC, id DESC LIMIT 1),
				''),
			COUNT(*), MIN(created_at), MAX(created_at)
		FROM matched`, name,
	).Scan(&profile.Person, &profile.Count, &firstAt, &lastAt)
	if err != nil {
		return PersonProfile{}, fmt.Errorf("query person profile: %w", err)
	}
	if profile.Count == 0 {
		return PersonProfile{}, ErrNotFound
	}
	if profile.FirstAt, err = parseStoredTime(firstAt.String); err != nil {
		return PersonProfile{}, err
	}
	if profile.LastAt, err = parseStoredTime(lastAt.String); err != nil {
		return PersonProfile{}, err
	}
	return profile, nil
}

// RandomEntry draws one live entry, optionally restricted to a person (matched
// case-insensitively) and to entries with or without a photo. It returns
// ErrNotFound when nothing matches.
func (s *Store) RandomEntry(ctx context.Context, person string, hasPhoto *bool) (Entry, error) {
	params := NormalizeListParams(ListParams{Person: person, HasPhoto: hasPhoto})
	where, args := ledgerFilter(params)
	query := `SELECT e.id, e.word, e.meaning, e.person, e.created_at, e.deleted_at,
		EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)
		FROM entries e` + where + ` ORDER BY RANDOM() LIMIT 1`
	entry, err := scanEntry(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("draw random entry: %w", err)
	}
	return entry, nil
}

// PhotoCursor identifies the last rendered gallery tile by its stored
// created_at and id, so a batch can continue below it without repeating or
// skipping a row when entries are added concurrently.
type PhotoCursor struct {
	CreatedAt time.Time
	ID        int64
}

// Token encodes the cursor as URL-safe base64 of "created_at_id".
func (c PhotoCursor) Token() string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "_" + strconv.FormatInt(c.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// ParsePhotoCursor decodes a gallery cursor token. Anything that is not an
// encoded (created_at, id) pair is ErrInvalidCursor rather than a silent
// restart of the gallery.
func ParsePhotoCursor(token string) (PhotoCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(token)
		if err != nil {
			return PhotoCursor{}, ErrInvalidCursor
		}
	}
	left, right, ok := strings.Cut(string(raw), "_")
	if !ok {
		return PhotoCursor{}, ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, left)
	if err != nil {
		return PhotoCursor{}, ErrInvalidCursor
	}
	id, err := strconv.ParseInt(right, 10, 64)
	if err != nil || id < 1 {
		return PhotoCursor{}, ErrInvalidCursor
	}
	return PhotoCursor{CreatedAt: createdAt.UTC(), ID: id}, nil
}

// PhotoBatch is one cursor page of the photo gallery. Next is the cursor for the
// following batch, nil on the last one. Prev is the cursor that renders the
// batch above this one, nil when this is the first batch or its start.
type PhotoBatch struct {
	Entries []Entry
	Total   int
	Next    *PhotoCursor
	Prev    *PhotoCursor
}

// ListPhotoEntries returns live entries that have a photo, newest first, in
// batches of at most limit rows. An empty cursor starts at the newest tile; a
// malformed one is ErrInvalidCursor.
func (s *Store) ListPhotoEntries(ctx context.Context, cursor string, limit int) (PhotoBatch, error) {
	if limit < 1 {
		limit = 1
	}
	var after *PhotoCursor
	if cursor != "" {
		decoded, err := ParsePhotoCursor(cursor)
		if err != nil {
			return PhotoBatch{}, err
		}
		after = &decoded
	}

	where := `e.deleted_at IS NULL AND EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)`
	args := make([]any, 0, 5)
	if after != nil {
		where += ` AND (e.created_at < ? OR (e.created_at = ? AND e.id < ?))`
		at := after.CreatedAt.UTC().Format(time.RFC3339)
		args = append(args, at, at, after.ID)
	}
	query := `SELECT e.id, e.word, e.meaning, e.person, e.created_at, e.deleted_at,
		EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)
		FROM entries e WHERE ` + where + `
		ORDER BY e.created_at DESC, e.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return PhotoBatch{}, fmt.Errorf("query photo entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]Entry, 0, limit+1)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return PhotoBatch{}, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return PhotoBatch{}, fmt.Errorf("iterate photo entries: %w", err)
	}

	batch := PhotoBatch{Entries: entries}
	if len(batch.Entries) > limit {
		batch.Entries = batch.Entries[:limit]
		last := batch.Entries[len(batch.Entries)-1]
		batch.Next = &PhotoCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	total, err := s.countPhotoEntries(ctx)
	if err != nil {
		return PhotoBatch{}, err
	}
	batch.Total = total

	// The previous document's cursor is the item just above the batch above this
	// one. The batch above is the limit tiles directly newer than this batch's
	// first tile; ordering those oldest first and skipping limit rows lands on
	// the cursor that renders them. No row means this is the first batch.
	if after != nil && len(batch.Entries) > 0 {
		first := batch.Entries[0]
		at := first.CreatedAt.UTC().Format(time.RFC3339)
		var (
			prevAt string
			prevID int64
		)
		err := s.db.QueryRowContext(ctx, `SELECT e.created_at, e.id FROM entries e
			WHERE e.deleted_at IS NULL
			  AND EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)
			  AND (e.created_at > ? OR (e.created_at = ? AND e.id > ?))
			ORDER BY e.created_at ASC, e.id ASC LIMIT 1 OFFSET ?`,
			at, at, first.ID, limit,
		).Scan(&prevAt, &prevID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// No batch above this one: the caller links back to /photos.
		case err != nil:
			return PhotoBatch{}, fmt.Errorf("query previous photo cursor: %w", err)
		default:
			parsed, parseErr := parseStoredTime(prevAt)
			if parseErr != nil {
				return PhotoBatch{}, parseErr
			}
			batch.Prev = &PhotoCursor{CreatedAt: parsed, ID: prevID}
		}
	}
	return batch, nil
}

func (s *Store) countPhotoEntries(ctx context.Context) (int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entries e
		 WHERE e.deleted_at IS NULL
		   AND EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)`,
	).Scan(&total); err != nil {
		return 0, fmt.Errorf("count photo entries: %w", err)
	}
	return total, nil
}

// parseStoredTime parses the RFC3339 text SQLite stores for created_at.
func parseStoredTime(raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time %q: %w", raw, err)
	}
	return parsed.UTC(), nil
}
