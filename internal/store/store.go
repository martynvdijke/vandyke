// Package store provides the SQLite persistence layer for the vanDyke ledger.
package store

import (
	"bytes"
	"context"
	"crypto/rand"
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

	sqlite "modernc.org/sqlite" // CGO-free SQLite driver

	"vandyke.thor.edu/vandyke/internal/searchquery"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed seed_entries.json
var seedFS embed.FS

// ErrNotFound is returned when an entry does not exist.
var ErrNotFound = errors.New("entry not found")

// ErrDuplicate is returned when an entry with the same misspelling and
// correction already exists, compared case-insensitively.
var ErrDuplicate = errors.New("entry already exists")

// ErrAdminExists is returned when first-run setup is attempted after an admin
// account has already been created.
var ErrAdminExists = errors.New("an admin account already exists")

// ErrAdminNotFound is returned when an admin account does not exist.
var ErrAdminNotFound = errors.New("admin not found")

// ErrSessionNotFound is returned when a session is missing or expired.
var ErrSessionNotFound = errors.New("session not found")

// ErrLastOwner is returned when an operation would leave no enabled owner.
var ErrLastOwner = errors.New("cannot remove the last enabled owner")

// Admin roles. Every admin is exactly one of these.
const (
	RoleOwner  = "owner"
	RoleEditor = "editor"
)

// ValidRole reports whether role is an accepted admin role.
func ValidRole(role string) bool { return role == RoleOwner || role == RoleEditor }

// sqliteConstraintUnique is SQLITE_CONSTRAINT_UNIQUE.
const sqliteConstraintUnique = 2067

// isUniqueViolation reports whether err is a SQLite unique-constraint error.
func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqliteConstraintUnique
}

const (
	// DefaultPerPage is the page size used when a request does not specify one.
	DefaultPerPage = 30
	// MaxPerPage caps the page size.
	MaxPerPage = 100
	// MaxWordLen is the maximum accepted length of a misspelling.
	MaxWordLen = 200
	// MaxMeaningLen is the maximum accepted length of a correction.
	MaxMeaningLen = 500
	// MaxPersonLen is the maximum accepted length of an entry's person.
	MaxPersonLen = 100
	// MaxPhotoBytes caps the size of an uploaded photo.
	MaxPhotoBytes = 10 << 20
)

// Entry is one row of the ledger: a misspelling and its correction, optionally
// accompanied by a photo of the offending text.
type Entry struct {
	ID        int64      `json:"id"`
	Word      string     `json:"word"`
	Meaning   string     `json:"meaning"`
	Person    string     `json:"person,omitempty"`
	PhotoURL  string     `json:"photo_url,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`

	// PhotoSHA256 is the original photo digest when a caller loaded it for a
	// lifecycle snapshot (UpdateEntry). It is not part of the public JSON
	// contract; the hash never leaves the server except in a webhook diff,
	// truncated to 12 characters.
	PhotoSHA256 string `json:"-"`
}

// Photo is image evidence attached to an entry. The original bytes are kept
// for administrators; Display and Thumb are metadata-free derivatives served
// to the public.
type Photo struct {
	ContentType string
	Data        []byte
	SHA256      string
	Size        int
	Width       int
	Height      int
	Display     *PhotoVariant
	Thumb       *PhotoVariant
}

// PhotoVariant is one processed version of an entry photo.
type PhotoVariant struct {
	ContentType string
	Data        []byte
	SHA256      string
	Size        int
	Width       int
	Height      int
}

// PhotoBackfill describes an existing photo that still needs derivatives.
type PhotoBackfill struct {
	EntryID     int64
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
	Query    string
	Sort     string
	Order    string
	Person   string
	Page     int
	PerPage  int
	HasPhoto *bool
	Year     int
	// From and To bound created_at inclusively. The zero value leaves that
	// side of the range open.
	From time.Time
	To   time.Time
	// Tags names tag slugs. Every requested tag must be carried by an entry,
	// so repeated values intersect, and tags intersect with every other
	// filter because each becomes its own AND clause.
	Tags []string
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
	Total          int           `json:"total_entries"`
	UniqueWords    int           `json:"unique_words"`
	FirstEntryAt   *time.Time    `json:"first_entry_at"`
	LastEntryAt    *time.Time    `json:"last_entry_at"`
	EntriesLast30d int           `json:"entries_last_30_days"`
	WithPhoto      int           `json:"with_photo"`
	ByYear         []YearCount   `json:"by_year"`
	TopPersons     []PersonCount `json:"top_persons"`
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
	p.Person = strings.TrimSpace(p.Person)
	p.Tags = normalizeTagSlugs(p.Tags)
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
	if p.Year < 1000 || p.Year > 9999 {
		p.Year = 0
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
		res, err := stmt.ExecContext(ctx, row.Word, row.Meaning, row.CreatedAt)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("seed entry %q: %w", row.Word, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("seed entry id %q: %w", row.Word, err)
		}
		if err := indexEntry(ctx, tx, id, row.Word, row.Meaning, ""); err != nil {
			_ = tx.Rollback()
			return err
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

	where, args := ledgerFilter(p)

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

	query := `SELECT e.id, e.word, e.meaning, e.person, e.created_at, e.deleted_at,
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

// ledgerFilter builds the shared WHERE clause for listing and counting entries.
// Trashed rows are always excluded.
func ledgerFilter(p ListParams) (string, []any) {
	clauses := []string{`e.deleted_at IS NULL`}
	args := make([]any, 0, 12)

	if query := searchquery.Parse(p.Query); !query.Empty() {
		if match := query.Match(); match != "" {
			clauses = append(clauses, `e.id IN (SELECT rowid FROM entries_fts WHERE entries_fts MATCH ?)`)
			args = append(args, match)
		} else {
			include, exclude := query.LikePatterns()
			for _, pattern := range include {
				clauses = append(clauses,
					`(e.word LIKE ? ESCAPE '\' OR e.meaning LIKE ? ESCAPE '\' OR e.person LIKE ? ESCAPE '\')`)
				args = append(args, pattern, pattern, pattern)
			}
			for _, pattern := range exclude {
				clauses = append(clauses,
					`NOT (e.word LIKE ? ESCAPE '\' OR e.meaning LIKE ? ESCAPE '\' OR e.person LIKE ? ESCAPE '\')`)
				args = append(args, pattern, pattern, pattern)
			}
		}
	}
	if p.HasPhoto != nil {
		if *p.HasPhoto {
			clauses = append(clauses, `EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)`)
		} else {
			clauses = append(clauses, `NOT EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)`)
		}
	}
	if p.Year > 0 {
		clauses = append(clauses, `e.created_at >= ? AND e.created_at < ?`)
		args = append(args,
			fmt.Sprintf("%04d-01-01T00:00:00Z", p.Year),
			fmt.Sprintf("%04d-01-01T00:00:00Z", p.Year+1),
		)
	}
	if !p.From.IsZero() {
		clauses = append(clauses, `e.created_at >= ?`)
		args = append(args, p.From.UTC().Format("2006-01-02T00:00:00Z"))
	}
	if !p.To.IsZero() {
		// Advances To by one day so the upper boundary date is inclusive.
		clauses = append(clauses, `e.created_at < ?`)
		args = append(args, p.To.UTC().AddDate(0, 0, 1).Format("2006-01-02T00:00:00Z"))
	}
	if person := strings.TrimSpace(p.Person); person != "" {
		clauses = append(clauses, `e.person = ? COLLATE NOCASE`)
		args = append(args, person)
	}
	for _, slug := range p.Tags {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM entry_tags et JOIN tags t ON t.id = et.tag_id
			WHERE et.entry_id = e.id AND t.slug = ? COLLATE NOCASE)`)
		args = append(args, slug)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// EntryYears returns the distinct years that contain ledger entries, newest first.
func (s *Store) EntryYears(ctx context.Context) ([]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT CAST(strftime('%Y', created_at) AS INTEGER) AS year
		FROM entries WHERE deleted_at IS NULL ORDER BY year DESC`)
	if err != nil {
		return nil, fmt.Errorf("query entry years: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var years []int
	for rows.Next() {
		var year int
		if err := rows.Scan(&year); err != nil {
			return nil, fmt.Errorf("scan entry year: %w", err)
		}
		years = append(years, year)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entry years: %w", err)
	}
	return years, nil
}

// Persons returns the distinct non-empty people recorded on ledger entries,
// ordered case-insensitively.
func (s *Store) Persons(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT person FROM entries WHERE person <> '' AND deleted_at IS NULL ORDER BY person COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("query persons: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var persons []string
	for rows.Next() {
		var person string
		if err := rows.Scan(&person); err != nil {
			return nil, fmt.Errorf("scan person: %w", err)
		}
		persons = append(persons, person)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate persons: %w", err)
	}
	return persons, nil
}

// YearCount is one calendar year and how many entries were logged in it.
type YearCount struct {
	Year  int `json:"year"`
	Count int `json:"count"`
}

// PersonCount is one attributed name and how many entries it has.
type PersonCount struct {
	Person string `json:"person"`
	Count  int    `json:"count"`
}

// PersonCounts lists every attributed person with their entry tally,
// ordered case-insensitively. It backs the filter pickers in the web UI and
// the Android app.
func (s *Store) PersonCounts(ctx context.Context) ([]PersonCount, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT person, COUNT(*) FROM entries
		 WHERE person <> '' AND deleted_at IS NULL
		 GROUP BY person COLLATE NOCASE
		 ORDER BY person COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("query person counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var counts []PersonCount
	for rows.Next() {
		var pc PersonCount
		if err := rows.Scan(&pc.Person, &pc.Count); err != nil {
			return nil, fmt.Errorf("scan person count: %w", err)
		}
		counts = append(counts, pc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate person counts: %w", err)
	}
	return counts, nil
}

// indexEntry keeps the trigram search index in sync with an entry.
func indexEntry(ctx context.Context, tx *sql.Tx, id int64, word, meaning, person string) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO entries_fts(rowid, word, meaning, person) VALUES (?, ?, ?, ?)`, id, word, meaning, person,
	); err != nil {
		return fmt.Errorf("index entry: %w", err)
	}
	return nil
}

// RebuildSearchIndex recreates the trigram index from the live entries. Trashed
// rows are skipped: they are invisible to search until an admin restores them,
// and restoring reindexes them.
func (s *Store) RebuildSearchIndex(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin search rebuild: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM entries_fts`); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear search index: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO entries_fts(rowid, word, meaning, person)
		 SELECT id, word, meaning, person FROM entries WHERE deleted_at IS NULL`,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("rebuild search index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit search rebuild: %w", err)
	}
	return nil
}

// GetEntry returns a single live entry by ID. A trashed entry is reported as
// ErrNotFound; use GetEntryIncludingDeleted to read it as an administrator.
func (s *Store) GetEntry(ctx context.Context, id int64) (Entry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT e.id, e.word, e.meaning, e.person, e.created_at, e.deleted_at,
		EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)
		FROM entries e WHERE e.id = ? AND e.deleted_at IS NULL`, id)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return entry, err
}

// GetEntryIncludingDeleted returns an entry whether it is live or trashed. It
// backs the admin edit page, the admin trash view and the original-photo
// download.
func (s *Store) GetEntryIncludingDeleted(ctx context.Context, id int64) (Entry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT e.id, e.word, e.meaning, e.person, e.created_at, e.deleted_at,
		EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)
		FROM entries e WHERE e.id = ?`, id)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return entry, err
}

// ListTrashedEntries returns every trashed entry, newest deletion first.
func (s *Store) ListTrashedEntries(ctx context.Context) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id, e.word, e.meaning, e.person, e.created_at, e.deleted_at,
		EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = e.id)
		FROM entries e WHERE e.deleted_at IS NOT NULL
		ORDER BY e.deleted_at DESC, e.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query trashed entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]Entry, 0)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trashed entries: %w", err)
	}
	return entries, nil
}

// GetEntryPhoto returns the original photo attached to an entry.
func (s *Store) GetEntryPhoto(ctx context.Context, id int64) (Photo, error) {
	return s.GetEntryPhotoSize(ctx, id, "original")
}

// GetEntryPhotoSize returns the requested photo variant. Unknown sizes fall
// back to the clean display image, and missing derivatives fall back to the
// original.
func (s *Store) GetEntryPhotoSize(ctx context.Context, id int64, size string) (Photo, error) {
	var query string
	switch size {
	case "original":
		query = `SELECT content_type, data, sha256, size, COALESCE(width, 0), COALESCE(height, 0)
			FROM entry_photos WHERE entry_id = ?`
	case "thumb":
		query = `SELECT COALESCE(thumb_content_type, display_content_type, content_type),
			COALESCE(thumb_data, display_data, data),
			COALESCE(thumb_sha256, display_sha256, sha256),
			COALESCE(thumb_size, display_size, size),
			COALESCE(thumb_width, display_width, width, 0),
			COALESCE(thumb_height, display_height, height, 0)
			FROM entry_photos WHERE entry_id = ?`
	default:
		query = `SELECT COALESCE(display_content_type, content_type),
			COALESCE(display_data, data),
			COALESCE(display_sha256, sha256),
			COALESCE(display_size, size),
			COALESCE(display_width, width, 0),
			COALESCE(display_height, height, 0)
			FROM entry_photos WHERE entry_id = ?`
	}
	var photo Photo
	err := s.db.QueryRowContext(ctx, query, id).Scan(&photo.ContentType, &photo.Data, &photo.SHA256, &photo.Size, &photo.Width, &photo.Height)
	if errors.Is(err, sql.ErrNoRows) {
		return Photo{}, ErrNotFound
	}
	if err != nil {
		return Photo{}, fmt.Errorf("query entry photo: %w", err)
	}
	return photo, nil
}

// ListPhotosMissingDerivatives returns photos that predate the derivative
// pipeline so they can be processed once.
func (s *Store) ListPhotosMissingDerivatives(ctx context.Context) ([]PhotoBackfill, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT entry_id, content_type, data, sha256, size FROM entry_photos
		WHERE display_data IS NULL OR thumb_data IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("query photos needing derivatives: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var photos []PhotoBackfill
	for rows.Next() {
		var photo PhotoBackfill
		if err := rows.Scan(&photo.EntryID, &photo.ContentType, &photo.Data, &photo.SHA256, &photo.Size); err != nil {
			return nil, fmt.Errorf("scan photo needing derivatives: %w", err)
		}
		photos = append(photos, photo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photos needing derivatives: %w", err)
	}
	return photos, nil
}

// SetPhotoDerivatives stores processed variants for an existing photo.
func (s *Store) SetPhotoDerivatives(ctx context.Context, id int64, width, height int, display, thumb PhotoVariant) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE entry_photos SET width = ?, height = ?,
			display_content_type = ?, display_data = ?, display_sha256 = ?, display_size = ?,
			display_width = ?, display_height = ?,
			thumb_content_type = ?, thumb_data = ?, thumb_sha256 = ?, thumb_size = ?,
			thumb_width = ?, thumb_height = ?
		WHERE entry_id = ?`,
		width, height,
		display.ContentType, display.Data, display.SHA256, display.Size, display.Width, display.Height,
		thumb.ContentType, thumb.Data, thumb.SHA256, thumb.Size, thumb.Width, thumb.Height,
		id,
	)
	if err != nil {
		return fmt.Errorf("store photo derivatives: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("photo derivative rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func photoInsertArgs(id int64, photo *Photo) []any {
	args := []any{id, photo.ContentType, photo.Data, photo.SHA256, photo.Size, photo.Width, photo.Height}
	args = appendVariantArgs(args, photo.Display)
	args = appendVariantArgs(args, photo.Thumb)
	return args
}

func appendVariantArgs(args []any, variant *PhotoVariant) []any {
	if variant == nil {
		return append(args, nil, nil, nil, nil, nil, nil)
	}
	return append(args, variant.ContentType, variant.Data, variant.SHA256, variant.Size, variant.Width, variant.Height)
}

// ValidatePerson checks the optional person name of an entry.
func ValidatePerson(person string) error {
	if len([]rune(person)) > MaxPersonLen {
		return fmt.Errorf("person must be at most %d characters", MaxPersonLen)
	}
	return nil
}

// insertEntryTx inserts an entry and its optional photo, updates the search
// index in the same transaction and returns the new entry id. A zero createdAt
// uses the column default.
func insertEntryTx(ctx context.Context, tx *sql.Tx, word, meaning, person string, createdAt time.Time, photo *Photo) (int64, error) {
	var (
		res sql.Result
		err error
	)
	if createdAt.IsZero() {
		res, err = tx.ExecContext(ctx,
			`INSERT INTO entries (word, meaning, person) VALUES (?, ?, ?)`, word, meaning, person)
	} else {
		res, err = tx.ExecContext(ctx,
			`INSERT INTO entries (word, meaning, person, created_at) VALUES (?, ?, ?, ?)`,
			word, meaning, person, createdAt.UTC().Format(time.RFC3339))
	}
	if err != nil {
		return 0, fmt.Errorf("insert entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	if photo != nil {
		if err := upsertEntryPhotoTx(ctx, tx, id, photo); err != nil {
			return 0, err
		}
	}
	if err := indexEntry(ctx, tx, id, word, meaning, person); err != nil {
		return 0, err
	}
	return id, nil
}

// upsertEntryPhotoTx stores photo bytes for an entry, replacing any existing
// photo for that entry.
func upsertEntryPhotoTx(ctx context.Context, tx *sql.Tx, id int64, photo *Photo) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO entry_photos (entry_id, content_type, data, sha256, size, width, height,
			display_content_type, display_data, display_sha256, display_size, display_width, display_height,
			thumb_content_type, thumb_data, thumb_sha256, thumb_size, thumb_width, thumb_height)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(entry_id) DO UPDATE SET content_type = excluded.content_type,
			data = excluded.data, sha256 = excluded.sha256, size = excluded.size,
			width = excluded.width, height = excluded.height,
			display_content_type = excluded.display_content_type, display_data = excluded.display_data,
			display_sha256 = excluded.display_sha256, display_size = excluded.display_size,
			display_width = excluded.display_width, display_height = excluded.display_height,
			thumb_content_type = excluded.thumb_content_type, thumb_data = excluded.thumb_data,
			thumb_sha256 = excluded.thumb_sha256, thumb_size = excluded.thumb_size,
			thumb_width = excluded.thumb_width, thumb_height = excluded.thumb_height`,
		photoInsertArgs(id, photo)...,
	); err != nil {
		return fmt.Errorf("store entry photo: %w", err)
	}
	return nil
}

// CreateEntry validates and inserts a new ledger entry, optionally with a photo.
func (s *Store) CreateEntry(ctx context.Context, word, meaning, person string, photo *Photo) (Entry, error) {
	word = strings.TrimSpace(word)
	meaning = strings.TrimSpace(meaning)
	person = strings.TrimSpace(person)
	if err := ValidateEntry(word, meaning, photo != nil); err != nil {
		return Entry{}, err
	}
	if err := ValidatePerson(person); err != nil {
		return Entry{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("begin entry insert: %w", err)
	}
	id, err := insertEntryTx(ctx, tx, word, meaning, person, time.Time{}, photo)
	if err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return Entry{}, ErrDuplicate
		}
		return Entry{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("commit entry insert: %w", err)
	}
	return s.GetEntry(ctx, id)
}

// AllEntryIDs returns the ids of every live entry in ascending order. It backs
// the sitemap without loading full rows, and excludes trashed entries.
func (s *Store) AllEntryIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM entries WHERE deleted_at IS NULL ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query entry ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan entry id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entry ids: %w", err)
	}
	return ids, nil
}

// CountEntries returns the total number of live ledger entries.
func (s *Store) CountEntries(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE deleted_at IS NULL`).Scan(&count); err != nil {
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
		FROM entries WHERE deleted_at IS NULL`).Scan(&stats.Total, &stats.UniqueWords, &firstEntry, &lastEntry, &stats.EntriesLast30d)
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
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entry_photos p
		 JOIN entries e ON e.id = p.entry_id
		 WHERE e.deleted_at IS NULL`,
	).Scan(&stats.WithPhoto); err != nil {
		return Stats{}, fmt.Errorf("count photos: %w", err)
	}
	yearRows, err := s.db.QueryContext(ctx, `
		SELECT CAST(strftime('%Y', created_at) AS INTEGER) AS year, COUNT(*)
		FROM entries
		WHERE deleted_at IS NULL
		GROUP BY year
		ORDER BY year`)
	if err != nil {
		return Stats{}, fmt.Errorf("query entries by year: %w", err)
	}
	defer func() { _ = yearRows.Close() }()
	for yearRows.Next() {
		var yc YearCount
		if err := yearRows.Scan(&yc.Year, &yc.Count); err != nil {
			return Stats{}, fmt.Errorf("scan entries by year: %w", err)
		}
		stats.ByYear = append(stats.ByYear, yc)
	}
	if err := yearRows.Err(); err != nil {
		return Stats{}, fmt.Errorf("iterate entries by year: %w", err)
	}
	personRows, err := s.db.QueryContext(ctx, `
		SELECT person, COUNT(*)
		FROM entries
		WHERE person <> '' AND deleted_at IS NULL
		GROUP BY person COLLATE NOCASE
		ORDER BY COUNT(*) DESC, person COLLATE NOCASE
		LIMIT 8`)
	if err != nil {
		return Stats{}, fmt.Errorf("query top persons: %w", err)
	}
	defer func() { _ = personRows.Close() }()
	for personRows.Next() {
		var pc PersonCount
		if err := personRows.Scan(&pc.Person, &pc.Count); err != nil {
			return Stats{}, fmt.Errorf("scan top persons: %w", err)
		}
		stats.TopPersons = append(stats.TopPersons, pc)
	}
	if err := personRows.Err(); err != nil {
		return Stats{}, fmt.Errorf("iterate top persons: %w", err)
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
		deletedAt sql.NullString
		hasPhoto  bool
	)
	if err := row.Scan(&entry.ID, &entry.Word, &entry.Meaning, &entry.Person, &createdAt, &deletedAt, &hasPhoto); err != nil {
		return Entry{}, err
	}
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return Entry{}, fmt.Errorf("parse created_at %q: %w", createdAt, err)
	}
	entry.CreatedAt = parsed.UTC()
	if deletedAt.Valid {
		deleted, err := time.Parse(time.RFC3339, deletedAt.String)
		if err != nil {
			return Entry{}, fmt.Errorf("parse deleted_at %q: %w", deletedAt.String, err)
		}
		deleted = deleted.UTC()
		entry.DeletedAt = &deleted
	}
	if hasPhoto {
		entry.PhotoURL = photoURL(entry.ID)
	}
	return entry, nil
}

// Admin is a local or OIDC-backed administrator account.
type Admin struct {
	ID           int64      `json:"id"`
	Username     string     `json:"username,omitempty"`
	PasswordHash string     `json:"-"`
	Email        string     `json:"email,omitempty"`
	DisplayName  string     `json:"display_name,omitempty"`
	Role         string     `json:"role"`
	OIDCIssuer   string     `json:"-"`
	OIDCSubject  string     `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	DisabledAt   *time.Time `json:"disabled_at,omitempty"`
}

// HasPassword reports whether the admin can sign in locally.
func (a Admin) HasPassword() bool { return a.Username != "" && a.PasswordHash != "" }

// IsOIDC reports whether the admin was created through OpenID Connect.
func (a Admin) IsOIDC() bool { return a.OIDCSubject != "" }

// IsOwner reports whether the admin holds the owner role.
func (a Admin) IsOwner() bool { return a.Role == RoleOwner }

// IsDisabled reports whether the account is disabled.
func (a Admin) IsDisabled() bool { return a.DisabledAt != nil }

// Session is an authenticated browser session.
type Session struct {
	TokenHash  string
	AdminID    int64
	CSRFToken  string
	SessionRef string
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// SessionView is one row of the session list.
type SessionView struct {
	Ref       string
	CreatedAt time.Time
	ExpiresAt time.Time
	Current   bool
}

// AuditRecord is one audit row to insert.
type AuditRecord struct {
	ActorID   *int64
	ActorName string
	Action    string
	Subject   string
	Detail    string
	IP        string
	CreatedAt time.Time
}

// AuditEntry is one audit row read back from the table.
type AuditEntry struct {
	ID        int64
	ActorID   *int64
	ActorName string
	Action    string
	Subject   string
	Detail    string
	IP        string
	CreatedAt time.Time
}

// AuditFilter narrows and paginates the audit viewer.
type AuditFilter struct {
	Action  string
	Actor   string
	From    string
	To      string
	Page    int
	PerPage int
}

// AuditPage is a page of audit rows plus the filtered total.
type AuditPage struct {
	Entries []AuditEntry
	Total   int
	Page    int
	Pages   int
	PerPage int
}

// SessionInfo bundles a session with its admin.
type SessionInfo struct {
	Session Session
	Admin   Admin
}

// EntryUpdate describes an administrator edit of a ledger entry.
type EntryUpdate struct {
	ID          int64
	Word        string
	Meaning     string
	Person      string
	CreatedAt   time.Time
	Photo       *Photo
	RemovePhoto bool
}

// AdminCount returns the number of administrator accounts.
func (s *Store) AdminCount(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return count, nil
}

// CreateFirstAdmin creates the initial local administrator. It fails with
// ErrAdminExists if any admin already exists, closing the first-run setup race.
func (s *Store) CreateFirstAdmin(ctx context.Context, username, passwordHash string) (Admin, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Admin{}, fmt.Errorf("begin first admin: %w", err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins`).Scan(&count); err != nil {
		_ = tx.Rollback()
		return Admin{}, fmt.Errorf("count admins: %w", err)
	}
	if count > 0 {
		_ = tx.Rollback()
		return Admin{}, ErrAdminExists
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO admins (username, password_hash, role) VALUES (?, ?, ?)`,
		username, passwordHash, RoleOwner)
	if err != nil {
		_ = tx.Rollback()
		return Admin{}, fmt.Errorf("insert first admin: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return Admin{}, fmt.Errorf("first admin id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Admin{}, fmt.Errorf("commit first admin: %w", err)
	}
	return s.GetAdmin(ctx, id)
}

// GetAdmin returns an admin by ID.
func (s *Store) GetAdmin(ctx context.Context, id int64) (Admin, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, email, display_name,
		role, oidc_issuer, oidc_subject, created_at, last_login_at, disabled_at
		FROM admins WHERE id = ?`, id)
	return scanAdmin(row)
}

// GetAdminByUsername returns a local admin by username (case-insensitive).
// Disabled accounts are treated as absent so login fails immediately.
func (s *Store) GetAdminByUsername(ctx context.Context, username string) (Admin, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, email, display_name,
		role, oidc_issuer, oidc_subject, created_at, last_login_at, disabled_at
		FROM admins WHERE username = ? COLLATE NOCASE AND disabled_at IS NULL`,
		strings.TrimSpace(username))
	return scanAdmin(row)
}

// ListAdmins returns every admin, oldest first.
func (s *Store) ListAdmins(ctx context.Context) ([]Admin, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, password_hash, email, display_name,
		role, oidc_issuer, oidc_subject, created_at, last_login_at, disabled_at
		FROM admins ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var admins []Admin
	for rows.Next() {
		admin, err := scanAdmin(rows)
		if err != nil {
			return nil, fmt.Errorf("scan admin: %w", err)
		}
		admins = append(admins, admin)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admins: %w", err)
	}
	return admins, nil
}

// CreateAdmin creates a local admin with an explicit role.
func (s *Store) CreateAdmin(ctx context.Context, username, displayName, passwordHash, role string) (Admin, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !ValidRole(role) {
		return Admin{}, fmt.Errorf("invalid admin role %q", role)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO admins (username, password_hash, display_name, role) VALUES (?, ?, ?, ?)`,
		username, passwordHash, strings.TrimSpace(displayName), role)
	if err != nil {
		if isUniqueViolation(err) {
			return Admin{}, ErrAdminExists
		}
		return Admin{}, fmt.Errorf("create admin: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Admin{}, fmt.Errorf("create admin id: %w", err)
	}
	return s.GetAdmin(ctx, id)
}

// CountEnabledOwners returns the number of enabled owners.
func (s *Store) CountEnabledOwners(ctx context.Context) (int, error) {
	return countEnabledOwners(ctx, s.db)
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func countEnabledOwners(ctx context.Context, db queryRower) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM admins WHERE role = ? AND disabled_at IS NULL`, RoleOwner,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count enabled owners: %w", err)
	}
	return count, nil
}

// SetAdminRole changes an admin's role. It refuses to demote the last enabled
// owner.
func (s *Store) SetAdminRole(ctx context.Context, id int64, role string) (Admin, error) {
	if !ValidRole(role) {
		return Admin{}, fmt.Errorf("invalid admin role %q", role)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Admin{}, fmt.Errorf("begin role change: %w", err)
	}
	var current string
	var disabled sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT role, disabled_at FROM admins WHERE id = ?`, id).Scan(&current, &disabled); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return Admin{}, ErrAdminNotFound
		}
		return Admin{}, fmt.Errorf("load admin role: %w", err)
	}
	if current == RoleOwner && role != RoleOwner && !disabled.Valid {
		owners, err := countEnabledOwners(ctx, tx)
		if err != nil {
			_ = tx.Rollback()
			return Admin{}, err
		}
		if owners < 2 {
			_ = tx.Rollback()
			return Admin{}, ErrLastOwner
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admins SET role = ? WHERE id = ?`, role, id); err != nil {
		_ = tx.Rollback()
		return Admin{}, fmt.Errorf("set admin role: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Admin{}, fmt.Errorf("commit role change: %w", err)
	}
	return s.GetAdmin(ctx, id)
}

// DisableAdmin sets disabled_at, refusing to disable the last enabled owner.
func (s *Store) DisableAdmin(ctx context.Context, id int64, at time.Time) error {
	return s.setAdminDisabled(ctx, id, true, at)
}

// EnableAdmin clears disabled_at.
func (s *Store) EnableAdmin(ctx context.Context, id int64) error {
	return s.setAdminDisabled(ctx, id, false, time.Time{})
}

func (s *Store) setAdminDisabled(ctx context.Context, id int64, disable bool, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin admin disable: %w", err)
	}
	var role string
	var disabled sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT role, disabled_at FROM admins WHERE id = ?`, id).Scan(&role, &disabled); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAdminNotFound
		}
		return fmt.Errorf("load admin: %w", err)
	}
	if disable && role == RoleOwner && !disabled.Valid {
		owners, err := countEnabledOwners(ctx, tx)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if owners < 2 {
			_ = tx.Rollback()
			return ErrLastOwner
		}
	}
	var value any
	if disable {
		value = at.UTC().Format(time.RFC3339)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admins SET disabled_at = ? WHERE id = ?`, value, id); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("set admin disabled_at: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit admin disable: %w", err)
	}
	return nil
}

// DeleteAdmin removes an admin row, cascading its sessions. It refuses to
// delete the last enabled owner.
func (s *Store) DeleteAdmin(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin admin delete: %w", err)
	}
	var role string
	var disabled sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT role, disabled_at FROM admins WHERE id = ?`, id).Scan(&role, &disabled); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAdminNotFound
		}
		return fmt.Errorf("load admin: %w", err)
	}
	if role == RoleOwner && !disabled.Valid {
		owners, err := countEnabledOwners(ctx, tx)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if owners < 2 {
			_ = tx.Rollback()
			return ErrLastOwner
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM admins WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete admin: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit admin delete: %w", err)
	}
	return nil
}

// SetAdminPassword replaces an admin's hash and revokes their sessions in one
// transaction. keepTokenHash, when non-empty, survives the revocation so a
// self-service change keeps the current session.
func (s *Store) SetAdminPassword(ctx context.Context, id int64, passwordHash, keepTokenHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password change: %w", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE admins SET password_hash = ? WHERE id = ?`, passwordHash, id)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("set admin password: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		_ = tx.Rollback()
		return ErrAdminNotFound
	}
	if keepTokenHash == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE admin_id = ?`, id)
	} else {
		_, err = tx.ExecContext(ctx,
			`DELETE FROM admin_sessions WHERE admin_id = ? AND token_hash <> ?`, id, keepTokenHash)
	}
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("revoke admin sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit password change: %w", err)
	}
	return nil
}

// UpsertOIDCAdmin returns the admin matching an OIDC identity, creating one on
// first login and refreshing profile fields on later logins.
func (s *Store) UpsertOIDCAdmin(ctx context.Context, issuer, subject, email, displayName string) (Admin, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Admin{}, fmt.Errorf("begin oidc admin: %w", err)
	}
	var id int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM admins WHERE oidc_issuer = ? AND oidc_subject = ?`, issuer, subject,
	).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// A fresh instance's first OIDC identity becomes its owner; later
		// identities are editors so an OIDC login cannot silently gain owner
		// rights on an established instance.
		var count int
		if countErr := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins`).Scan(&count); countErr != nil {
			_ = tx.Rollback()
			return Admin{}, fmt.Errorf("count admins: %w", countErr)
		}
		role := RoleEditor
		if count == 0 {
			role = RoleOwner
		}
		res, insertErr := tx.ExecContext(ctx,
			`INSERT INTO admins (email, display_name, oidc_issuer, oidc_subject, role) VALUES (?, ?, ?, ?, ?)`,
			email, displayName, issuer, subject, role)
		if insertErr != nil {
			_ = tx.Rollback()
			return Admin{}, fmt.Errorf("insert oidc admin: %w", insertErr)
		}
		id, err = res.LastInsertId()
		if err != nil {
			_ = tx.Rollback()
			return Admin{}, fmt.Errorf("oidc admin id: %w", err)
		}
	case err != nil:
		_ = tx.Rollback()
		return Admin{}, fmt.Errorf("find oidc admin: %w", err)
	default:
		if _, err := tx.ExecContext(ctx,
			`UPDATE admins SET email = ?, display_name = ? WHERE id = ?`, email, displayName, id,
		); err != nil {
			_ = tx.Rollback()
			return Admin{}, fmt.Errorf("update oidc admin: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Admin{}, fmt.Errorf("commit oidc admin: %w", err)
	}
	return s.GetAdmin(ctx, id)
}

// TouchAdmin records a successful login.
func (s *Store) TouchAdmin(ctx context.Context, id int64, at time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE admins SET last_login_at = ? WHERE id = ?`, at.UTC().Format(time.RFC3339), id,
	); err != nil {
		return fmt.Errorf("touch admin: %w", err)
	}
	return nil
}

// CreateSession stores a hashed session token together with a fresh random
// reference used for per-session revocation.
func (s *Store) CreateSession(ctx context.Context, session Session) error {
	ref, err := newSessionRef()
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_sessions (token_hash, admin_id, csrf_token, session_ref, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		session.TokenHash, session.AdminID, session.CSRFToken, ref,
		session.ExpiresAt.UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// newSessionRef returns a 16-byte random hex reference.
func newSessionRef() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("session reference: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// GetSession returns a live session and its admin. Disabled admins are treated
// as having no session so disabling takes effect on the next request.
func (s *Store) GetSession(ctx context.Context, tokenHash string) (SessionInfo, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	row := s.db.QueryRowContext(ctx, `SELECT s.token_hash, s.admin_id, s.csrf_token, s.session_ref,
		s.created_at, s.expires_at,
		a.id, a.username, a.password_hash, a.email, a.display_name, a.role, a.oidc_issuer,
		a.oidc_subject, a.created_at, a.last_login_at
		FROM admin_sessions s JOIN admins a ON a.id = s.admin_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND a.disabled_at IS NULL`, tokenHash, now)

	var (
		info       SessionInfo
		sessionRef string
		username   sql.NullString
		password   sql.NullString
		created    string
		expires    string
		aCreated   string
		lastLogin  sql.NullString
	)
	if err := row.Scan(&info.Session.TokenHash, &info.Session.AdminID, &info.Session.CSRFToken,
		&sessionRef, &created, &expires, &info.Admin.ID, &username, &password,
		&info.Admin.Email, &info.Admin.DisplayName, &info.Admin.Role, &info.Admin.OIDCIssuer,
		&info.Admin.OIDCSubject, &aCreated, &lastLogin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionInfo{}, ErrSessionNotFound
		}
		return SessionInfo{}, fmt.Errorf("query session: %w", err)
	}
	info.Session.SessionRef = sessionRef
	info.Admin.Username = username.String
	info.Admin.PasswordHash = password.String
	var err error
	if info.Session.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
		return SessionInfo{}, fmt.Errorf("parse session created_at: %w", err)
	}
	if info.Session.ExpiresAt, err = time.Parse(time.RFC3339, expires); err != nil {
		return SessionInfo{}, fmt.Errorf("parse session expires_at: %w", err)
	}
	if info.Admin.CreatedAt, err = time.Parse(time.RFC3339, aCreated); err != nil {
		return SessionInfo{}, fmt.Errorf("parse admin created_at: %w", err)
	}
	if lastLogin.Valid {
		if t, err := time.Parse(time.RFC3339, lastLogin.String); err == nil {
			t = t.UTC()
			info.Admin.LastLoginAt = &t
		}
	}
	return info, nil
}

// DeleteSession removes one session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes sessions whose expiry has passed.
func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM admin_sessions WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}

// ListSessionsForAdmin returns an admin's sessions, newest first.
func (s *Store) ListSessionsForAdmin(ctx context.Context, adminID int64, currentTokenHash string) ([]SessionView, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_ref, token_hash, created_at, expires_at FROM admin_sessions
		WHERE admin_id = ? ORDER BY created_at DESC`, adminID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var sessions []SessionView
	for rows.Next() {
		var (
			view    SessionView
			ref     string
			token   string
			created string
			expires string
		)
		if err := rows.Scan(&ref, &token, &created, &expires); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		view.Ref = ref
		view.Current = token == currentTokenHash
		if view.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, fmt.Errorf("parse session created_at: %w", err)
		}
		if view.ExpiresAt, err = time.Parse(time.RFC3339, expires); err != nil {
			return nil, fmt.Errorf("parse session expires_at: %w", err)
		}
		sessions = append(sessions, view)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}

// DeleteSessionByRef removes one session by its reference. When scopeAdminID is
// non-zero the session must belong to that admin; zero lets an owner revoke any
// reference. The reference resolves to a token hash and the row is deleted by
// that hash, so the delete itself always targets a single token. It returns how
// many rows were deleted.
func (s *Store) DeleteSessionByRef(ctx context.Context, ref string, scopeAdminID int64) (int64, error) {
	if strings.TrimSpace(ref) == "" {
		return 0, nil
	}
	lookup := `SELECT token_hash FROM admin_sessions WHERE session_ref = ?`
	args := []any{ref}
	if scopeAdminID != 0 {
		lookup += ` AND admin_id = ?`
		args = append(args, scopeAdminID)
	}
	var tokenHash string
	if err := s.db.QueryRowContext(ctx, lookup, args...).Scan(&tokenHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("find session by ref: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE token_hash = ?`, tokenHash)
	if err != nil {
		return 0, fmt.Errorf("delete session by token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted sessions: %w", err)
	}
	return n, nil
}

// DeleteOtherSessionsForAdmin removes every session of adminID except the one
// identified by keepTokenHash, returning the number deleted.
func (s *Store) DeleteOtherSessionsForAdmin(ctx context.Context, adminID int64, keepTokenHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM admin_sessions WHERE admin_id = ? AND token_hash <> ?`, adminID, keepTokenHash)
	if err != nil {
		return 0, fmt.Errorf("delete other sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted sessions: %w", err)
	}
	return n, nil
}

// RecordAudit appends one audit row. Callers log and ignore a failure rather
// than reverting the action it describes.
func (s *Store) RecordAudit(ctx context.Context, rec AuditRecord) error {
	createdAt := rec.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_audit (actor_id, actor_name, action, subject, detail, ip, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rec.ActorID, rec.ActorName, rec.Action, rec.Subject, rec.Detail, rec.IP,
		createdAt.UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("record audit: %w", err)
	}
	return nil
}

// ListAudit returns a filtered, paginated page of audit rows newest first.
func (s *Store) ListAudit(ctx context.Context, filter AuditFilter) (AuditPage, error) {
	perPage := filter.PerPage
	if perPage <= 0 {
		perPage = 50
	}
	if perPage > 200 {
		perPage = 200
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}

	var (
		where []string
		args  []any
	)
	if action := strings.TrimSpace(filter.Action); action != "" {
		where = append(where, `action LIKE ?`)
		args = append(args, "%"+action+"%")
	}
	if actor := strings.TrimSpace(filter.Actor); actor != "" {
		where = append(where, `actor_name LIKE ?`)
		args = append(args, "%"+actor+"%")
	}
	if from := strings.TrimSpace(filter.From); from != "" {
		where = append(where, `created_at >= ?`)
		args = append(args, normalizeAuditDate(from, false))
	}
	if to := strings.TrimSpace(filter.To); to != "" {
		where = append(where, `created_at <= ?`)
		args = append(args, normalizeAuditDate(to, true))
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit`+clause, args...).Scan(&total); err != nil {
		return AuditPage{}, fmt.Errorf("count audit: %w", err)
	}
	pages := 0
	if total > 0 {
		pages = (total + perPage - 1) / perPage
	}
	if pages > 0 && page > pages {
		page = pages
	}

	query := `SELECT id, actor_id, actor_name, action, subject, detail, ip, created_at
		FROM admin_audit` + clause + ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		return AuditPage{}, fmt.Errorf("list audit: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := AuditPage{Total: total, Page: page, Pages: pages, PerPage: perPage}
	for rows.Next() {
		var (
			entry     AuditEntry
			actorID   sql.NullInt64
			createdAt string
		)
		if err := rows.Scan(&entry.ID, &actorID, &entry.ActorName, &entry.Action,
			&entry.Subject, &entry.Detail, &entry.IP, &createdAt); err != nil {
			return AuditPage{}, fmt.Errorf("scan audit: %w", err)
		}
		if actorID.Valid {
			id := actorID.Int64
			entry.ActorID = &id
		}
		if entry.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
			return AuditPage{}, fmt.Errorf("parse audit created_at: %w", err)
		}
		out.Entries = append(out.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return AuditPage{}, fmt.Errorf("iterate audit: %w", err)
	}
	return out, nil
}

// normalizeAuditDate turns a date-only filter into a full timestamp so string
// comparison against RFC3339 rows behaves as expected.
func normalizeAuditDate(value string, endOfDay bool) string {
	if len(value) == 10 {
		if endOfDay {
			return value + "T23:59:59Z"
		}
		return value + "T00:00:00Z"
	}
	return value
}

// UpdateEntry applies an administrator edit, optionally replacing or removing
// the photo, and returns the previous snapshot together with the updated entry.
// The previous snapshot is captured inside the same transaction as the write so
// a webhook diff can never be built from a stale read.
func (s *Store) UpdateEntry(ctx context.Context, update EntryUpdate) (Entry, Entry, error) {
	update.Word = strings.TrimSpace(update.Word)
	update.Meaning = strings.TrimSpace(update.Meaning)
	update.Person = strings.TrimSpace(update.Person)
	if update.Photo != nil && update.RemovePhoto {
		return Entry{}, Entry{}, errors.New("choose either a new photo or remove the existing one")
	}
	if err := ValidatePerson(update.Person); err != nil {
		return Entry{}, Entry{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, Entry{}, fmt.Errorf("begin entry update: %w", err)
	}
	var (
		previous    Entry
		createdAt   string
		deletedAt   sql.NullString
		previousSHA string
	)
	previous.ID = update.ID
	if err := tx.QueryRowContext(ctx, `SELECT e.word, e.meaning, e.person, e.created_at, e.deleted_at,
		COALESCE((SELECT p.sha256 FROM entry_photos p WHERE p.entry_id = e.id), '')
		FROM entries e WHERE e.id = ?`, update.ID).Scan(
		&previous.Word, &previous.Meaning, &previous.Person, &createdAt, &deletedAt, &previousSHA,
	); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return Entry{}, Entry{}, ErrNotFound
		}
		return Entry{}, Entry{}, fmt.Errorf("load entry: %w", err)
	}
	previous.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		_ = tx.Rollback()
		return Entry{}, Entry{}, fmt.Errorf("parse created_at %q: %w", createdAt, err)
	}
	if deletedAt.Valid && deletedAt.String != "" {
		t, err := time.Parse(time.RFC3339, deletedAt.String)
		if err != nil {
			_ = tx.Rollback()
			return Entry{}, Entry{}, fmt.Errorf("parse deleted_at %q: %w", deletedAt.String, err)
		}
		previous.DeletedAt = &t
	}
	previous.PhotoSHA256 = previousSHA
	if previousSHA != "" {
		previous.PhotoURL = photoURL(update.ID)
	}

	hasPhoto := previousSHA != ""
	if update.CreatedAt.IsZero() {
		update.CreatedAt = previous.CreatedAt
	}
	if update.Photo != nil {
		hasPhoto = true
	}
	if update.RemovePhoto {
		hasPhoto = false
	}
	if err := ValidateEntry(update.Word, update.Meaning, hasPhoto); err != nil {
		_ = tx.Rollback()
		return Entry{}, Entry{}, err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE entries SET word = ?, meaning = ?, person = ?, created_at = ? WHERE id = ?`,
		update.Word, update.Meaning, update.Person, update.CreatedAt.UTC().Format(time.RFC3339), update.ID,
	); err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return Entry{}, Entry{}, ErrDuplicate
		}
		return Entry{}, Entry{}, fmt.Errorf("update entry: %w", err)
	}
	newSHA := previousSHA
	switch {
	case update.Photo != nil:
		if err := upsertEntryPhotoTx(ctx, tx, update.ID, update.Photo); err != nil {
			_ = tx.Rollback()
			return Entry{}, Entry{}, err
		}
		newSHA = update.Photo.SHA256
	case update.RemovePhoto:
		if _, err := tx.ExecContext(ctx, `DELETE FROM entry_photos WHERE entry_id = ?`, update.ID); err != nil {
			_ = tx.Rollback()
			return Entry{}, Entry{}, fmt.Errorf("remove entry photo: %w", err)
		}
		newSHA = ""
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO entries_fts(rowid, word, meaning, person) VALUES (?, ?, ?, ?)`,
		update.ID, update.Word, update.Meaning, update.Person,
	); err != nil {
		_ = tx.Rollback()
		return Entry{}, Entry{}, fmt.Errorf("update search index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, Entry{}, fmt.Errorf("commit entry update: %w", err)
	}
	updated, err := s.GetEntryIncludingDeleted(ctx, update.ID)
	if err != nil {
		return Entry{}, Entry{}, err
	}
	updated.PhotoSHA256 = newSHA
	return previous, updated, nil
}

// cleanIDs drops non-positive and duplicate ids while preserving order.
func cleanIDs(ids []int64) []any {
	seen := make(map[int64]bool, len(ids))
	clean := make([]any, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	return clean
}

// placeholders builds "?,?,?" for n arguments.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// selectLifecycleEntriesTx loads the rows a bulk lifecycle action will change,
// inside the same transaction as the change, so the returned slice is exactly
// the set of entries that were affected.
func selectLifecycleEntriesTx(ctx context.Context, tx *sql.Tx, ids []any, trashed bool) ([]Entry, error) {
	clause := `id IN (` + placeholders(len(ids)) + `)`
	if trashed {
		clause += ` AND deleted_at IS NOT NULL`
	} else {
		clause += ` AND deleted_at IS NULL`
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, word, meaning, person, created_at, deleted_at,
		EXISTS (SELECT 1 FROM entry_photos p WHERE p.entry_id = entries.id)
		FROM entries WHERE `+clause+` ORDER BY id`, ids...)
	if err != nil {
		return nil, fmt.Errorf("query lifecycle entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []Entry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lifecycle entries: %w", err)
	}
	return entries, nil
}

// SoftDeleteEntries marks entries as trashed by setting deleted_at and returns
// the rows that actually moved to the trash, one per changed entry. Ids that
// were already trashed are not returned, so a bulk action's slice length is its
// exact changed count. The index rows are removed so a trashed entry cannot be
// found even by a future path that forgets the deleted_at guard;
// RestoreEntries reindexes them. Photos are kept.
func (s *Store) SoftDeleteEntries(ctx context.Context, ids []int64) ([]Entry, error) {
	clean := cleanIDs(ids)
	if len(clean) == 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin entry trash: %w", err)
	}
	changed, err := selectLifecycleEntriesTx(ctx, tx, clean, false)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM entries_fts WHERE rowid IN (`+placeholders(len(clean))+`)`, clean...,
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("remove trashed search rows: %w", err)
	}
	deletedAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx,
		`UPDATE entries SET deleted_at = ? WHERE id IN (`+placeholders(len(clean))+`) AND deleted_at IS NULL`,
		append([]any{deletedAt.Format(time.RFC3339)}, clean...)...,
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("soft delete entries: %w", err)
	}
	// The rows were selected before the update; stamp the deletion time so a
	// receiver sees the post-change snapshot and entry.deleted can carry
	// deleted_at.
	for i := range changed {
		changed[i].DeletedAt = &deletedAt
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit entry trash: %w", err)
	}
	return changed, nil
}

// RestoreEntries clears deleted_at for the matching trashed entries and returns
// the rows that were actually restored. Ids that are already live are ignored,
// making restore idempotent. Restored rows are reindexed with all three
// searchable columns.
func (s *Store) RestoreEntries(ctx context.Context, ids []int64) ([]Entry, error) {
	clean := cleanIDs(ids)
	if len(clean) == 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin entry restore: %w", err)
	}
	changed, err := selectLifecycleEntriesTx(ctx, tx, clean, true)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE entries SET deleted_at = NULL WHERE id IN (`+placeholders(len(clean))+`) AND deleted_at IS NOT NULL`,
		clean...,
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("restore entries: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO entries_fts(rowid, word, meaning, person)
		 SELECT id, word, meaning, person FROM entries
		 WHERE id IN (`+placeholders(len(clean))+`) AND deleted_at IS NULL`,
		clean...,
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("reindex restored entries: %w", err)
	}
	// A restored row is live again, so its snapshot must not carry deleted_at.
	for i := range changed {
		changed[i].DeletedAt = nil
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit entry restore: %w", err)
	}
	return changed, nil
}

// PurgeEntries permanently deletes trashed entries: the FTS rows first, then
// the entry rows, whose photos cascade away. It returns the deleted snapshots
// so the caller can emit one entry.purged event per entry and erase terminal
// delivery history for the purged ids in the same transaction. Live ids are
// ignored.
func (s *Store) PurgeEntries(ctx context.Context, ids []int64) ([]Entry, error) {
	clean := cleanIDs(ids)
	if len(clean) == 0 {
		return nil, nil
	}
	args := placeholders(len(clean))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin entry purge: %w", err)
	}
	removed, err := selectLifecycleEntriesTx(ctx, tx, clean, true)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM entries_fts WHERE rowid IN (
			SELECT id FROM entries WHERE id IN (`+args+`) AND deleted_at IS NOT NULL)`,
		clean...,
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("purge search index rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM entries WHERE id IN (`+args+`) AND deleted_at IS NOT NULL`, clean...,
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("purge entries: %w", err)
	}
	// A delivered or cancelled copy of the entry's text must not outlive the
	// purge. Pending and failed rows remain because they exist to announce the
	// purge itself and are bounded by the retention rules.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM webhook_deliveries WHERE entry_id IN (`+args+`) AND status IN (?, ?)`,
		append(append([]any{}, clean...), WebhookStatusDelivered, WebhookStatusCancelled)...,
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("purge webhook delivery history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit entry purge: %w", err)
	}
	return removed, nil
}

// GetSettings returns all persisted runtime settings.
func (s *Store) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("query settings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	values := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settings: %w", err)
	}
	return values, nil
}

// GetSetting returns one persisted setting and whether the key exists. It is
// the single-key counterpart of GetSettings, used by runtime state such as the
// daily slip pin that must not load the whole configuration.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query setting %q: %w", key, err)
	}
	return value, true, nil
}

// SetSettingIfAbsent inserts value under key only when the key has no row yet,
// then reads the row back so the caller always sees the winning value. Two
// concurrent first writers therefore agree on exactly one value.
func (s *Store) SetSettingIfAbsent(ctx context.Context, key, value string) (string, error) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO NOTHING`,
		key, value,
	); err != nil {
		return "", fmt.Errorf("insert setting %q: %w", key, err)
	}
	winning, exists, err := s.GetSetting(ctx, key)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("insert setting %q: row disappeared", key)
	}
	return winning, nil
}

// PruneSettingsPrefix deletes every row whose key starts with prefix except
// keep, and reports how many rows were removed. It backs the daily slip so the
// table holds at most one daily pin.
func (s *Store) PruneSettingsPrefix(ctx context.Context, prefix, keep string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM settings WHERE substr(key, 1, length(?)) = ? AND key <> ?`,
		prefix, prefix, keep,
	)
	if err != nil {
		return 0, fmt.Errorf("prune settings %q: %w", prefix, err)
	}
	removed, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count pruned settings %q: %w", prefix, err)
	}
	return removed, nil
}

// SetSettings upserts runtime settings in one transaction.
func (s *Store) SetSettings(ctx context.Context, values map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings update: %w", err)
	}
	for key, value := range values {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value,
				updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
			key, value,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("set setting %q: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings: %w", err)
	}
	return nil
}

func scanAdmin(row rowScanner) (Admin, error) {
	var (
		admin      Admin
		username   sql.NullString
		password   sql.NullString
		created    string
		lastLogin  sql.NullString
		disabledAt sql.NullString
	)
	if err := row.Scan(&admin.ID, &username, &password, &admin.Email,
		&admin.DisplayName, &admin.Role, &admin.OIDCIssuer, &admin.OIDCSubject,
		&created, &lastLogin, &disabledAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Admin{}, ErrAdminNotFound
		}
		return Admin{}, err
	}
	admin.Username = username.String
	admin.PasswordHash = password.String
	var err error
	if admin.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
		return Admin{}, fmt.Errorf("parse admin created_at: %w", err)
	}
	if lastLogin.Valid {
		if t, err := time.Parse(time.RFC3339, lastLogin.String); err == nil {
			t = t.UTC()
			admin.LastLoginAt = &t
		}
	}
	if disabledAt.Valid {
		if t, err := time.Parse(time.RFC3339, disabledAt.String); err == nil {
			t = t.UTC()
			admin.DisabledAt = &t
		}
	}
	return admin, nil
}
