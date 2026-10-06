// Package store provides the SQLite persistence layer for the vanDyke ledger.
package store

import (
	"context"
	"database/sql"
	"embed"
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
)

// Entry is one row of the ledger: a misspelling and its correction.
type Entry struct {
	ID        int64     `json:"id"`
	Word      string    `json:"word"`
	Meaning   string    `json:"meaning"`
	CreatedAt time.Time `json:"created_at"`
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

// ValidateEntry checks user-supplied word and meaning values.
func ValidateEntry(word, meaning string) error {
	switch {
	case strings.TrimSpace(word) == "":
		return errors.New("word is required")
	case len([]rune(word)) > MaxWordLen:
		return fmt.Errorf("word must be at most %d characters", MaxWordLen)
	case strings.TrimSpace(meaning) == "":
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
		where = ` WHERE (word LIKE ? ESCAPE '\' OR meaning LIKE ? ESCAPE '\')`
		args = append(args, pattern, pattern)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`+where, args...).Scan(&total); err != nil {
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

	query := `SELECT id, word, meaning, created_at FROM entries` + where +
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
	row := s.db.QueryRowContext(ctx, `SELECT id, word, meaning, created_at FROM entries WHERE id = ?`, id)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return entry, err
}

// CreateEntry validates and inserts a new ledger entry.
func (s *Store) CreateEntry(ctx context.Context, word, meaning string) (Entry, error) {
	word = strings.TrimSpace(word)
	meaning = strings.TrimSpace(meaning)
	if err := ValidateEntry(word, meaning); err != nil {
		return Entry{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO entries (word, meaning) VALUES (?, ?)`, word, meaning)
	if err != nil {
		return Entry{}, fmt.Errorf("insert entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Entry{}, fmt.Errorf("last insert id: %w", err)
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
			COUNT(DISTINCT word COLLATE NOCASE),
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
	)
	if err := row.Scan(&entry.ID, &entry.Word, &entry.Meaning, &createdAt); err != nil {
		return Entry{}, err
	}
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return Entry{}, fmt.Errorf("parse created_at %q: %w", createdAt, err)
	}
	entry.CreatedAt = parsed.UTC()
	return entry, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
