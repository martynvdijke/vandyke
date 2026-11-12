package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MaxTagNameLen is the maximum accepted length of a tag name in runes.
const MaxTagNameLen = 40

// ErrTagExists is returned when a tag name already exists case-insensitively.
var ErrTagExists = errors.New("a tag with that name already exists")

// Tag is one row of the curated vocabulary. LiveCount counts entries with a
// null deleted_at; TotalCount counts every assignment, trash included, and is
// only populated by ListAdminTags.
type Tag struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	Color      string    `json:"color"`
	CreatedAt  time.Time `json:"created_at"`
	LiveCount  int       `json:"live_count"`
	TotalCount int       `json:"total_count"`
}

// ValidateTag checks a tag name and colour. The name is trimmed and capped at
// MaxTagNameLen runes; the derived slug must be non-empty; the colour must be
// a #RRGGBB value.
func ValidateTag(name, color string) error {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return errors.New("name is required")
	case len([]rune(name)) > MaxTagNameLen:
		return fmt.Errorf("name must be at most %d characters", MaxTagNameLen)
	case slugifyTag(name) == "":
		return errors.New("name must contain a letter or digit")
	}
	if !validTagColor(color) {
		return errors.New("colour must be a hex value like #8c3b3b")
	}
	return nil
}

func validTagColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for _, r := range color[1:] {
		isDigit := r >= '0' && r <= '9'
		isLower := r >= 'a' && r <= 'f'
		isUpper := r >= 'A' && r <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}

// slugifyTag derives a lowercase slug: ASCII letters and digits are kept, every
// other run collapses to one hyphen, and leading or trailing hyphens are
// trimmed.
func slugifyTag(name string) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if separator && b.Len() > 0 {
				b.WriteByte('-')
			}
			separator = false
			b.WriteRune(r)
			continue
		}
		separator = true
	}
	return b.String()
}

// normalizeTagInput trims a name and a colour and lowercases the colour.
func normalizeTagInput(name, color string) (string, string) {
	return strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(color))
}

// GetTag returns a tag by id, or ErrNotFound.
func (s *Store) GetTag(ctx context.Context, id int64) (Tag, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, slug, color, created_at FROM tags WHERE id = ?`, id)
	tag, err := scanTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Tag{}, ErrNotFound
	}
	return tag, err
}

// GetTagBySlug returns a tag by slug, case-insensitively, or ErrNotFound.
func (s *Store) GetTagBySlug(ctx context.Context, slug string) (Tag, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, color, created_at FROM tags WHERE slug = ? COLLATE NOCASE`,
		strings.TrimSpace(slug))
	tag, err := scanTag(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Tag{}, ErrNotFound
	}
	return tag, err
}

// ListTags returns every tag with its live entry count, ordered by name.
func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	return s.listTags(ctx, false)
}

// ListAdminTags returns every tag with both its live count and its total
// assignment count, trash included.
func (s *Store) ListAdminTags(ctx context.Context) ([]Tag, error) {
	return s.listTags(ctx, true)
}

func (s *Store) listTags(ctx context.Context, admin bool) ([]Tag, error) {
	query := `SELECT t.id, t.name, t.slug, t.color, t.created_at,
		(SELECT COUNT(*) FROM entry_tags et
			JOIN entries e ON e.id = et.entry_id
			WHERE et.tag_id = t.id AND e.deleted_at IS NULL) AS live_count`
	if admin {
		query += `,
		(SELECT COUNT(*) FROM entry_tags et WHERE et.tag_id = t.id) AS total_count`
	}
	query += ` FROM tags t ORDER BY t.name COLLATE NOCASE, t.id`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tags []Tag
	for rows.Next() {
		tag, err := scanTagRow(rows, admin)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tags: %w", err)
	}
	return tags, nil
}

// CreateTag inserts a tag. The slug is derived from the name and never changes
// afterwards. A duplicate name, case-insensitively, returns ErrTagExists.
func (s *Store) CreateTag(ctx context.Context, name, color string) (Tag, error) {
	name, color = normalizeTagInput(name, color)
	if err := ValidateTag(name, color); err != nil {
		return Tag{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tags (name, slug, color) VALUES (?, ?, ?)`,
		name, slugifyTag(name), color)
	if err != nil {
		if isUniqueViolation(err) {
			return Tag{}, ErrTagExists
		}
		return Tag{}, fmt.Errorf("create tag: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Tag{}, fmt.Errorf("create tag id: %w", err)
	}
	return s.GetTag(ctx, id)
}

// UpdateTag changes a tag's name and colour. The slug is deliberately left
// untouched so renaming never breaks an existing /tags/{slug} link.
func (s *Store) UpdateTag(ctx context.Context, id int64, name, color string) (Tag, error) {
	name, color = normalizeTagInput(name, color)
	if err := ValidateTag(name, color); err != nil {
		return Tag{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE tags SET name = ?, color = ? WHERE id = ?`, name, color, id)
	if err != nil {
		if isUniqueViolation(err) {
			return Tag{}, ErrTagExists
		}
		return Tag{}, fmt.Errorf("update tag: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return Tag{}, ErrNotFound
	}
	return s.GetTag(ctx, id)
}

// DeleteTag removes a tag and, by cascade, every assignment, returning the
// number of join rows removed in the same transaction. An unknown id returns
// ErrNotFound.
func (s *Store) DeleteTag(ctx context.Context, id int64) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tag delete: %w", err)
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE id = ?`, id).Scan(&exists); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("count tag: %w", err)
	}
	if exists == 0 {
		_ = tx.Rollback()
		return 0, ErrNotFound
	}
	var removed int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entry_tags WHERE tag_id = ?`, id).Scan(&removed); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("count tag assignments: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("delete tag: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tag delete: %w", err)
	}
	return removed, nil
}

// EntryTags returns the tags carried by one entry, ordered by name. It works
// for trashed entries too, because the admin edit page still shows them.
func (s *Store) EntryTags(ctx context.Context, entryID int64) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.slug, t.color, t.created_at
		FROM tags t JOIN entry_tags et ON et.tag_id = t.id
		WHERE et.entry_id = ?
		ORDER BY t.name COLLATE NOCASE, t.id`, entryID)
	if err != nil {
		return nil, fmt.Errorf("query entry tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tags []Tag
	for rows.Next() {
		tag, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entry tags: %w", err)
	}
	return tags, nil
}

// TagsByEntry returns the tags of every listed entry in one query, keyed by
// entry id, so a ledger page adds one statement regardless of page size.
func (s *Store) TagsByEntry(ctx context.Context, entryIDs []int64) (map[int64][]Tag, error) {
	out := make(map[int64][]Tag)
	clean := cleanIDs(entryIDs)
	if len(clean) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT et.entry_id, t.id, t.name, t.slug, t.color, t.created_at
		FROM entry_tags et JOIN tags t ON t.id = et.tag_id
		WHERE et.entry_id IN (`+placeholders(len(clean))+`)
		ORDER BY et.entry_id, t.name COLLATE NOCASE, t.id`, clean...)
	if err != nil {
		return nil, fmt.Errorf("query tags by entry: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			entryID   int64
			tag       Tag
			createdAt string
		)
		if err := rows.Scan(&entryID, &tag.ID, &tag.Name, &tag.Slug, &tag.Color, &createdAt); err != nil {
			return nil, fmt.Errorf("scan tag by entry: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse tag created_at %q: %w", createdAt, err)
		}
		tag.CreatedAt = parsed.UTC()
		out[entryID] = append(out[entryID], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tags by entry: %w", err)
	}
	return out, nil
}

// SetEntryTags replaces the complete tag set of one entry in a single
// transaction. Duplicate and unknown tag ids are ignored, and an empty set
// clears every assignment.
func (s *Store) SetEntryTags(ctx context.Context, entryID int64, tagIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin entry tags: %w", err)
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE id = ?`, entryID).Scan(&exists); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("count entry: %w", err)
	}
	if exists == 0 {
		_ = tx.Rollback()
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM entry_tags WHERE entry_id = ?`, entryID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear entry tags: %w", err)
	}
	clean := cleanIDs(tagIDs)
	if len(clean) > 0 {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO entry_tags (entry_id, tag_id)
			SELECT ?, id FROM tags WHERE id IN (`+placeholders(len(clean))+`)`,
			append([]any{entryID}, clean...)...,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("assign entry tags: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit entry tags: %w", err)
	}
	return nil
}

// normalizeTagSlugs trims each requested slug, drops empty values and
// de-duplicates case-insensitively while preserving the first spelling.
func normalizeTagSlugs(values []string) []string {
	var out []string
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		slug := strings.TrimSpace(value)
		if slug == "" {
			continue
		}
		key := strings.ToLower(slug)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, slug)
	}
	return out
}

// scanTag scans the five base tag columns.
func scanTag(row rowScanner) (Tag, error) {
	var (
		tag       Tag
		createdAt string
	)
	if err := row.Scan(&tag.ID, &tag.Name, &tag.Slug, &tag.Color, &createdAt); err != nil {
		return Tag{}, err
	}
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return Tag{}, fmt.Errorf("parse tag created_at %q: %w", createdAt, err)
	}
	tag.CreatedAt = parsed.UTC()
	return tag, nil
}

// scanTagRow scans five or six columns depending on the admin variant.
func scanTagRow(rows rowScanner, admin bool) (Tag, error) {
	var (
		tag       Tag
		createdAt string
	)
	dest := []any{&tag.ID, &tag.Name, &tag.Slug, &tag.Color, &createdAt, &tag.LiveCount}
	if admin {
		dest = append(dest, &tag.TotalCount)
	}
	if err := rows.Scan(dest...); err != nil {
		return Tag{}, fmt.Errorf("scan tag: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return Tag{}, fmt.Errorf("parse tag created_at %q: %w", createdAt, err)
	}
	tag.CreatedAt = parsed.UTC()
	return tag, nil
}
