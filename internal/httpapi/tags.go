package httpapi

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jmoiron/sqlx"
)

const (
	// maxTagsPerNote is the most tags one note may carry, counted after duplicates are removed.
	maxTagsPerNote = 10
	// maxTagLength is the longest tag name, and the bound the tag error message names.
	maxTagLength = 30
)

// tagPattern is the one rule a tag name has to satisfy: one or more characters from a-z, 0-9 and
// -, so the alphabet and the length are a single check with a single message.
var tagPattern = regexp.MustCompile(fmt.Sprintf(`^[a-z0-9-]{1,%d}$`, maxTagLength))

// tagsRequest is the body accepted when a note's tags are replaced. An absent or null tags field
// unmarshals to the zero value, which means the empty set: the note keeps no tags at all.
type tagsRequest struct {
	Tags []string `json:"tags"`
}

// normalizeTags turns the names of a request into the set to store: duplicates count once and the
// first-seen order is kept, more than maxTagsPerNote names is an error, and so is any name that
// breaks tagPattern.
func normalizeTags(tags []string) ([]string, error) {
	names := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, name := range tags {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) > maxTagsPerNote {
		return nil, errors.New("a note has at most 10 tags")
	}
	for _, name := range names {
		if !tagPattern.MatchString(name) {
			return nil, errors.New("tag must be 1 to 30 characters of a-z, 0-9 and -")
		}
	}
	return names, nil
}

// loadTags returns the tag names of every requested note, keyed by the note's UUID and sorted
// ascending. It is one statement for the whole slice of ids, never one per note, so listing a page
// of notes costs the same number of queries whatever its size. A note without tags is absent from
// the map.
func loadTags(ctx context.Context, db *sqlx.DB, noteIDs []string) (map[string][]string, error) {
	tags := make(map[string][]string, len(noteIDs))
	if len(noteIDs) == 0 {
		return tags, nil
	}

	query, args, err := sqlx.In(
		"SELECT nt.note_id, t.name FROM note_tags nt JOIN tags t ON t.id = nt.tag_id WHERE nt.note_id IN (?) ORDER BY t.name",
		noteIDs)
	if err != nil {
		return nil, fmt.Errorf("building tags query: %w", err)
	}
	rows, err := db.QueryContext(ctx, db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("loading tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			noteID string
			name   string
		)
		if err := rows.Scan(&noteID, &name); err != nil {
			return nil, fmt.Errorf("scanning tag: %w", err)
		}
		tags[noteID] = append(tags[noteID], name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("loading tags: %w", err)
	}
	return tags, nil
}

// replaceTags makes the note's tags exactly names. Every write runs inside one transaction, so a
// failure part-way leaves the note's previous tags in place. Empty names clears the note's tags.
// Tag rows are shared: a name already stored is reused instead of inserted twice.
func replaceTags(ctx context.Context, db *sqlx.DB, noteID string, names []string) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting tags transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM note_tags WHERE note_id = ?", noteID); err != nil {
		return fmt.Errorf("clearing tags of note %s: %w", noteID, err)
	}

	for _, name := range names {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO tags (name) VALUES (?) ON CONFLICT (name) DO NOTHING", name); err != nil {
			return fmt.Errorf("storing tag %q: %w", name, err)
		}
		var tagID int64
		if err := tx.GetContext(ctx, &tagID, "SELECT id FROM tags WHERE name = ?", name); err != nil {
			return fmt.Errorf("loading tag %q: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO note_tags (note_id, tag_id) VALUES (?, ?)", noteID, tagID); err != nil {
			return fmt.Errorf("linking tag %q: %w", name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing tags of note %s: %w", noteID, err)
	}
	return nil
}
