package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ExportRow is one ledger entry plus its photo metadata as written to an
// export file.
type ExportRow struct {
	ID               int64     `json:"id"`
	Word             string    `json:"word"`
	Meaning          string    `json:"meaning"`
	Person           string    `json:"person"`
	CreatedAt        time.Time `json:"created_at"`
	HasPhoto         bool      `json:"has_photo"`
	PhotoContentType string    `json:"photo_content_type"`
	PhotoSHA256      string    `json:"photo_sha256"`
}

// ExportPhoto is the display image payload written into an export archive.
// ContentType is the derivative's stored type, or the original's type when the
// derivative columns are null.
type ExportPhoto struct {
	ContentType string
	Data        []byte
}

// ImportRow is one row read from an import file. Photo is nil when the file
// carries no bytes for the row.
type ImportRow struct {
	ID        int64
	Word      string
	Meaning   string
	Person    string
	CreatedAt time.Time
	Photo     *Photo
}

// ImportPreview counts what an import would do without writing anything.
// Skip counts rows repeated within the file; Conflict counts rows matching an
// existing case-insensitive (word, meaning).
type ImportPreview struct {
	Create   int
	Skip     int
	Conflict int
}

// ImportResult reports what an applied import changed.
type ImportResult struct {
	Created  int
	Skipped  int
	Replaced int
}

// ImportRowError reports the row that stopped an import. Index is zero-based.
type ImportRowError struct {
	Index int
	Word  string
	Err   error
}

func (e *ImportRowError) Error() string {
	return fmt.Sprintf("row %d (%q): %v", e.Index+1, e.Word, e.Err)
}

// Unwrap returns the underlying row error.
func (e *ImportRowError) Unwrap() error { return e.Err }

// ExportEntries returns every entry in ascending id order, joined to its photo
// metadata. There is no pagination.
func (s *Store) ExportEntries(ctx context.Context) ([]ExportRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.word, e.meaning, e.person, e.created_at,
			p.content_type, p.sha256
		FROM entries e
		LEFT JOIN entry_photos p ON p.entry_id = e.id
		ORDER BY e.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query export entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]ExportRow, 0)
	for rows.Next() {
		var (
			row         ExportRow
			createdAt   string
			contentType sql.NullString
			sha256      sql.NullString
		)
		if err := rows.Scan(&row.ID, &row.Word, &row.Meaning, &row.Person, &createdAt, &contentType, &sha256); err != nil {
			return nil, fmt.Errorf("scan export entry: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse created_at %q: %w", createdAt, err)
		}
		row.CreatedAt = parsed.UTC()
		if contentType.Valid {
			row.HasPhoto = true
			row.PhotoContentType = contentType.String
			row.PhotoSHA256 = sha256.String
		}
		entries = append(entries, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate export entries: %w", err)
	}
	return entries, nil
}

// GetEntryExportPhoto returns the display derivative for one entry, falling
// back to the original bytes and content type when the derivative columns are
// null.
func (s *Store) GetEntryExportPhoto(ctx context.Context, id int64) (ExportPhoto, error) {
	var photo ExportPhoto
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(display_content_type, content_type), COALESCE(display_data, data)
		FROM entry_photos WHERE entry_id = ?`, id,
	).Scan(&photo.ContentType, &photo.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return ExportPhoto{}, ErrNotFound
	}
	if err != nil {
		return ExportPhoto{}, fmt.Errorf("query export photo: %w", err)
	}
	return photo, nil
}

func importKey(word, meaning string) string {
	return strings.ToLower(strings.TrimSpace(word)) + "\x00" + strings.ToLower(strings.TrimSpace(meaning))
}

func findEntryIDDB(ctx context.Context, db *sql.DB, word, meaning string) (int64, bool, error) {
	var id int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM entries WHERE word = ? COLLATE NOCASE AND meaning = ? COLLATE NOCASE`,
		word, meaning,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("find entry: %w", err)
	}
	return id, true, nil
}

func findEntryIDTx(ctx context.Context, tx *sql.Tx, word, meaning string) (int64, bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM entries WHERE word = ? COLLATE NOCASE AND meaning = ? COLLATE NOCASE`,
		word, meaning,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("find entry: %w", err)
	}
	return id, true, nil
}

// PreviewImport classifies rows against the unique (word, meaning) index
// without writing anything.
func (s *Store) PreviewImport(ctx context.Context, rows []ImportRow) (ImportPreview, error) {
	var preview ImportPreview
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		word := strings.TrimSpace(row.Word)
		meaning := strings.TrimSpace(row.Meaning)
		key := importKey(word, meaning)
		if _, ok := seen[key]; ok {
			preview.Skip++
			continue
		}
		_, found, err := findEntryIDDB(ctx, s.db, word, meaning)
		if err != nil {
			return ImportPreview{}, err
		}
		if found {
			preview.Conflict++
		} else {
			preview.Create++
		}
		seen[key] = struct{}{}
	}
	return preview, nil
}

// ApplyImport inserts new rows and resolves duplicates in one transaction.
// Rows repeated within the file are skipped. A row matching an existing
// (word, meaning) is skipped unless replaceExisting is set, in which case only
// person, created_at and the photo are replaced. Any row error rolls the whole
// import back.
func (s *Store) ApplyImport(ctx context.Context, rows []ImportRow, replaceExisting bool) (ImportResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, fmt.Errorf("begin import: %w", err)
	}
	var result ImportResult
	seen := make(map[string]struct{}, len(rows))

	fail := func(index int, word string, err error) (ImportResult, error) {
		_ = tx.Rollback()
		return ImportResult{}, &ImportRowError{Index: index, Word: word, Err: err}
	}

	for i, row := range rows {
		word := strings.TrimSpace(row.Word)
		meaning := strings.TrimSpace(row.Meaning)
		person := strings.TrimSpace(row.Person)
		key := importKey(word, meaning)

		if _, ok := seen[key]; ok {
			result.Skipped++
			continue
		}
		seen[key] = struct{}{}

		existingID, found, err := findEntryIDTx(ctx, tx, word, meaning)
		if err != nil {
			return fail(i, word, err)
		}
		switch {
		case found && !replaceExisting:
			result.Skipped++
		case found:
			if err := ValidatePerson(person); err != nil {
				return fail(i, word, err)
			}
			if err := replaceEntryTx(ctx, tx, existingID, person, row.CreatedAt, row.Photo); err != nil {
				return fail(i, word, err)
			}
			result.Replaced++
		default:
			if err := ValidateEntry(word, meaning, row.Photo != nil); err != nil {
				return fail(i, word, err)
			}
			if err := ValidatePerson(person); err != nil {
				return fail(i, word, err)
			}
			if _, err := insertEntryTx(ctx, tx, word, meaning, person, row.CreatedAt, row.Photo); err != nil {
				if isUniqueViolation(err) {
					return fail(i, word, ErrDuplicate)
				}
				return fail(i, word, err)
			}
			result.Created++
		}
	}
	if err := tx.Commit(); err != nil {
		return ImportResult{}, fmt.Errorf("commit import: %w", err)
	}
	return result, nil
}

// replaceEntryTx updates only person, created_at and the photo of an existing
// row, leaving word and meaning untouched. The search index already holds the
// stored word and meaning; it is rewritten from those values.
func replaceEntryTx(ctx context.Context, tx *sql.Tx, id int64, person string, createdAt time.Time, photo *Photo) error {
	if createdAt.IsZero() {
		if _, err := tx.ExecContext(ctx, `UPDATE entries SET person = ? WHERE id = ?`, person, id); err != nil {
			return fmt.Errorf("update entry: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE entries SET person = ?, created_at = ? WHERE id = ?`,
			person, createdAt.UTC().Format(time.RFC3339), id); err != nil {
			return fmt.Errorf("update entry: %w", err)
		}
	}
	if photo != nil {
		if err := upsertEntryPhotoTx(ctx, tx, id, photo); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO entries_fts(rowid, word, meaning, person)
		 SELECT id, word, meaning, person FROM entries WHERE id = ?`, id,
	); err != nil {
		return fmt.Errorf("update search index: %w", err)
	}
	return nil
}
