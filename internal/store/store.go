// Package store provides the SQLite persistence layer for the vanDyke ledger.
package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // CGO-free SQLite driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed seed_entries.json
var seedFS embed.FS

// ErrNotFound is returned when an entry does not exist.
var ErrNotFound = errors.New("entry not found")

const (
	// DefaultPerPage is the page size used when a request does not specify one.
	DefaultPerPage = 30
	// MaxPerPage caps the page size.
	MaxPerPage = 100
	// MaxWordLen is the maximum accepted length of a misspelling.
	MaxWordLen = 200
	// MaxMeaningLen is the maximum accepted length of a correction.
	MaxMeaningLen = 500
	// MaxPhotoBytes caps the size of an uploaded photo.
	MaxPhotoBytes = 10 << 20
)

// Entry is one row of the ledger: a misspelling and its correction, optionally
// accompanied by a photo of the offending text.
type Entry struct {
	ID        int64     `json:"id"`
	Word      string    `json:"word"`
	Meaning   string    `json:"meaning"`
	PhotoURL  string    `json:"photo_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Photo is image evidence attached to an entry.
type Photo struct {
	ContentType string
	Data        []byte
	SHA256      string
	Size        int
}

// NewPhoto validates an uploaded image and returns it ready for storage.
func NewPhoto(data []byte) (*Photo, error) {
	switch {
	case len(data) == 0:
		return nil, errors.New("photo is empty")
	case len(data) > MaxPhotoBytes:
		return nil, fmt.Errorf("photo must be at most %d MB", MaxPhotoBytes/(1<<20))
	}

	contentType := photoContentType(data)
	if contentType == "" {
		return nil, errors.New("photo must be a JPEG, PNG, GIF or WebP image")
	}

	sum := sha256.Sum256(data)
	return &Photo{
		ContentType: contentType,
		Data:        data,
		SHA256:      hex.EncodeToString(sum[:]),
		Size:        len(data),
	}, nil
}

func photoContentType(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 8 && bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

func photoURL(id int64) string {
	return fmt.Sprintf("/api/v1/entries/%d/photo", id)
}

// ListParams describes a ledger query.
type ListParams struct {
	Query   string
	Sort    string
	Order   string
	Page    int
	PerPage int
}

// Page is a paginated slice of entries.
type Page struct {
	Entries []Entry `json:"items"`
	Total   int     `json:"total"`
	Page    int     `json:"page"`
	PerPage int     `json:"per_page"`
	Pages   int     `json:"pages"`
}

// Stats summarises the ledger.
type Stats struct {
	Total          int        `json:"total_entries"`
	UniqueWords    int        `json:"unique_words"`
	FirstEntryAt   *time.Time `json:"first_entry_at"`
	LastEntryAt    *time.Time `json:"last_entry_at"`
	EntriesLast30d int        `json:"entries_last_30_days"`
}

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

var sortColumns = map[string]string{
	"id":         "id",
	"word":       "word COLLATE NOCASE",
	"meaning":    "meaning COLLATE NOCASE",
	"created_at": "created_at",
}

// ValidateEntry checks user-supplied word and meaning values. The text fields
// are optional when a photo is attached; otherwise both are required.
func ValidateEntry(word, meaning string, hasPhoto bool) error {
	switch {
	case !hasPhoto && strings.TrimSpace(word) == "":
		return errors.New("word is required")
	case len([]rune(word)) > MaxWordLen:
		return fmt.Errorf("word must be at most %d characters", MaxWordLen)
	case !hasPhoto && strings.TrimSpace(meaning) == "":
		return errors.New("meaning is required")
	case len([]rune(meaning)) > MaxMeaningLen:
		return fmt.Errorf("meaning must be at most %d characters", MaxMeaningLen)
	}
	return nil
}

// NormalizeListParams clamps list parameters to supported values.
func NormalizeListParams(p ListParams) ListParams {
	p.Query = strings.TrimSpace(p.Query)
	if _, ok := sortColumns[p.Sort]; !ok {
		p.Sort = "created_at"
	}
	if p.Order != "asc" && p.Order != "desc" {
		if p.Sort == "word" || p.Sort == "meaning" {
			p.Order = "asc"
		} else {
			p.Order = "desc"
		}
	}
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage < 1 {
		p.PerPage = DefaultPerPage
	}
	if p.PerPage > MaxPerPage {
		p.PerPage = MaxPerPage
	}
	return p
}

// Open opens (creating if needed) the SQLite database, applies migrations and
// seeds the ledger from the embedded dataset when the table is empty.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
	}
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.seedIfEmpty(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

type seedEntry struct {
	Word      string `json:"word"`
	Meaning   string `json:"meaning"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(names)

	for _, name := range names {
		version := filepath.Base(name)
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", version, err)
		}
		for _, stmt := range splitStatements(string(body)) {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply migration %s: %w", version, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}
	return nil
}

// splitStatements splits a migration script into individual statements,
// dropping comment-only lines. Migrations intentionally contain plain DDL.
func splitStatements(script string) []string {
	var cleaned []string
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	var out []string
	for _, stmt := range strings.Split(strings.Join(cleaned, "\n"), ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// seedIfEmpty imports the embedded snapshot of the original site when the
// ledger has no entries yet.
func (s *Store) seedIfEmpty(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`).Scan(&count); err != nil {
		return fmt.Errorf("count entries: %w", err)
	}
	if count > 0 {
		return nil
	}

	body, err := fs.ReadFile(seedFS, "seed_entries.json")
	if err != nil {
		return fmt.Errorf("read seed data: %w", err)
	}
	var rows []seedEntry
	if err := json.Unmarshal(body, &rows); err != nil {
		return fmt.Errorf("parse seed data: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin seed: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO entries (word, meaning, created_at) VALUES (?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare seed insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, row.Word, row.Meaning, row.CreatedAt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("seed entry %q: %w", row.Word, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}
	return nil
}

// ListEntries returns a filtered, sorted, paginated view of the ledger.
func (s *Store) ListEntries(ctx context.Context, params ListParams) (Page, error) {
	p := NormalizeListParams(params)

	where := ""
	args := make([]any, 0, 2)
	if p.Query != "" {
		pattern := "%" + escapeLike(p.Query) + "%"
		where = ` WHERE (e.word LIKE ? ESCAPE '\' OR e.meaning LIKE ? ESCAPE '\')`
		args = append(args, pattern, pattern)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries e`+where, args...).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count entries: %w", err)
	}

	pages := 0
	if total > 0 {
		pages = (total + p.PerPage - 1) / p.PerPage
		if p.Page > pages {
			p.Page = pages
		}
	} else {
		p.Page = 1
	}

	query := `SELECT e.id, e.word, e.meaning, e.created_at,
		EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)
		FROM entries e` + where +
		` ORDER BY ` + sortColumns[p.Sort] + ` ` + strings.ToUpper(p.Order) + `, id ` + strings.ToUpper(p.Order) +
		` LIMIT ? OFFSET ?`
	args = append(args, p.PerPage, (p.Page-1)*p.PerPage)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, fmt.Errorf("query entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]Entry, 0, p.PerPage)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return Page{}, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate entries: %w", err)
	}

	return Page{Entries: entries, Total: total, Page: p.Page, PerPage: p.PerPage, Pages: pages}, nil
}

// GetEntry returns a single entry by ID.
func (s *Store) GetEntry(ctx context.Context, id int64) (Entry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT e.id, e.word, e.meaning, e.created_at,
		EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)
		FROM entries e WHERE e.id = ?`, id)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return entry, err
}

// GetEntryPhoto returns the photo attached to an entry.
func (s *Store) GetEntryPhoto(ctx context.Context, id int64) (Photo, error) {
	var photo Photo
	err := s.db.QueryRowContext(ctx,
		`SELECT content_type, data, sha256, size FROM entry_photos WHERE entry_id = ?`, id,
	).Scan(&photo.ContentType, &photo.Data, &photo.SHA256, &photo.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return Photo{}, ErrNotFound
	}
	if err != nil {
		return Photo{}, fmt.Errorf("query entry photo: %w", err)
	}
	return photo, nil
}

// CreateEntry validates and inserts a new ledger entry, optionally with a photo.
func (s *Store) CreateEntry(ctx context.Context, word, meaning string, photo *Photo) (Entry, error) {
	word = strings.TrimSpace(word)
	meaning = strings.TrimSpace(meaning)
	if err := ValidateEntry(word, meaning, photo != nil); err != nil {
		return Entry{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("begin entry insert: %w", err)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO entries (word, meaning) VALUES (?, ?)`, word, meaning)
	if err != nil {
		_ = tx.Rollback()
		return Entry{}, fmt.Errorf("insert entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return Entry{}, fmt.Errorf("last insert id: %w", err)
	}
	if photo != nil {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO entry_photos (entry_id, content_type, data, sha256, size) VALUES (?, ?, ?, ?, ?)`,
			id, photo.ContentType, photo.Data, photo.SHA256, photo.Size,
		); err != nil {
			_ = tx.Rollback()
			return Entry{}, fmt.Errorf("insert entry photo: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("commit entry insert: %w", err)
	}
	return s.GetEntry(ctx, id)
}

// CountEntries returns the total number of ledger entries.
func (s *Store) CountEntries(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count entries: %w", err)
	}
	return count, nil
}

// Stats summarises the ledger for the UI and API.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var (
		stats      Stats
		firstEntry sql.NullString
		lastEntry  sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(DISTINCT CASE WHEN word <> '' THEN word COLLATE NOCASE END),
			MIN(created_at),
			MAX(created_at),
			COUNT(CASE WHEN created_at >= strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-30 day') THEN 1 END)
		FROM entries`).Scan(&stats.Total, &stats.UniqueWords, &firstEntry, &lastEntry, &stats.EntriesLast30d)
	if err != nil {
		return Stats{}, fmt.Errorf("query stats: %w", err)
	}
	if firstEntry.Valid {
		if t, err := time.Parse(time.RFC3339, firstEntry.String); err == nil {
			t = t.UTC()
			stats.FirstEntryAt = &t
		}
	}
	if lastEntry.Valid {
		if t, err := time.Parse(time.RFC3339, lastEntry.String); err == nil {
			t = t.UTC()
			stats.LastEntryAt = &t
		}
	}
	return stats, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(row rowScanner) (Entry, error) {
	var (
		entry     Entry
		createdAt string
		hasPhoto  bool
	)
	if err := row.Scan(&entry.ID, &entry.Word, &entry.Meaning, &createdAt, &hasPhoto); err != nil {
		return Entry{}, err
	}
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return Entry{}, fmt.Errorf("parse created_at %q: %w", createdAt, err)
	}
	entry.CreatedAt = parsed.UTC()
	if hasPhoto {
		entry.PhotoURL = photoURL(entry.ID)
	}
	return entry, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
