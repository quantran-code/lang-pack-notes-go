package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two canonical lowercase hyphenated UUID v4 literals, the form the notes table keys on, so a test
// can name a note it inserted without reading the id back first.
const (
	testNoteID      = "3f2a6c1e-0b7d-4a52-9c8e-6d1f4b2a7c30"
	testOtherNoteID = "8b1d4e6f-2c39-4b7a-9e51-0f6c3a8d2b14"
)

func TestOpenAppliesMigrations(t *testing.T) {
	db, err := Open(MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var version int
	require.NoError(t, db.Get(&version, "SELECT version FROM schema_migrations"))
	assert.Equal(t, 4, version)

	_, err = db.Exec("INSERT INTO notes (id, title, body, created_at) VALUES (?, 't', 'b', CURRENT_TIMESTAMP)", testNoteID)
	assert.NoError(t, err)
}

// TestMigrationsAreReversible runs the embedded down migration of the tag migration and checks it
// undoes exactly its own up migration: the tag tables go, the notes table and its rows stay.
func TestMigrationsAreReversible(t *testing.T) {
	db, err := Open(MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("INSERT INTO notes (id, title, body, created_at) VALUES (?, 'kept', 'b', CURRENT_TIMESTAMP)", testNoteID)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO tags (name) VALUES ('go')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO note_tags (note_id, tag_id) VALUES (?, 1)", testNoteID)
	require.NoError(t, err)

	down, err := migrationsFS.ReadFile("migrations/000002_create_tags.down.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(down))
	require.NoError(t, err)

	for _, table := range []string{"note_tags", "tags"} {
		var n int
		require.NoError(t, db.Get(&n, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table))
		assert.Equal(t, 0, n, "the %s table is gone", table)
	}

	var title string
	require.NoError(t, db.Get(&title, "SELECT title FROM notes WHERE id = ?", testNoteID))
	assert.Equal(t, "kept", title, "the notes table and its rows survive")
}

// TestNoteStatusMigrationIsReversible runs the embedded down migration of the status migration and
// then its up migration again: the status column goes and comes back while the notes table and its
// rows stay, and a row inserted while the column was absent reads 0 once it is back.
func TestNoteStatusMigrationIsReversible(t *testing.T) {
	db, err := Open(MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("INSERT INTO notes (id, title, body, created_at, status) VALUES (?, 'kept', 'b', CURRENT_TIMESTAMP, 2)", testNoteID)
	require.NoError(t, err)

	down, err := migrationsFS.ReadFile("migrations/000003_add_note_status.down.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(down))
	require.NoError(t, err)

	var n int
	require.NoError(t, db.Get(&n, "SELECT COUNT(*) FROM pragma_table_info('notes') WHERE name = 'status'"))
	assert.Equal(t, 0, n, "the status column is gone")

	var title string
	require.NoError(t, db.Get(&title, "SELECT title FROM notes WHERE id = ?", testNoteID))
	assert.Equal(t, "kept", title, "the notes table and its rows survive")

	// A note created while the column was absent has no status of its own to restore.
	_, err = db.Exec("INSERT INTO notes (id, title, body, created_at) VALUES (?, 'added', 'b', CURRENT_TIMESTAMP)", testOtherNoteID)
	require.NoError(t, err)

	up, err := migrationsFS.ReadFile("migrations/000003_add_note_status.up.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(up))
	require.NoError(t, err)

	require.NoError(t, db.Get(&n, "SELECT COUNT(*) FROM pragma_table_info('notes') WHERE name = 'status'"))
	assert.Equal(t, 1, n, "the status column is back")

	for _, want := range []struct {
		id     string
		title  string
		status int
	}{
		{testNoteID, "kept", 0},
		{testOtherNoteID, "added", 0},
	} {
		require.NoError(t, db.Get(&title, "SELECT title FROM notes WHERE id = ?", want.id))
		assert.Equal(t, want.title, title, "note %s keeps its row", want.id)

		var status int
		require.NoError(t, db.Get(&status, "SELECT status FROM notes WHERE id = ?", want.id))
		assert.Equal(t, want.status, status, "note %s is a draft", want.id)
	}
}

// TestDeletingNoteCascadesNoteTags checks that the foreign key on note_tags is enforced: deleting
// a note takes its links with it instead of leaving rows pointing at a note that is gone.
func TestDeletingNoteCascadesNoteTags(t *testing.T) {
	db, err := Open(MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("INSERT INTO notes (id, title, body, created_at) VALUES (?, 't', 'b', CURRENT_TIMESTAMP)", testNoteID)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO tags (name) VALUES ('go')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO note_tags (note_id, tag_id) VALUES (?, 1)", testNoteID)
	require.NoError(t, err)

	_, err = db.Exec("DELETE FROM notes WHERE id = ?", testNoteID)
	require.NoError(t, err)

	var n int
	require.NoError(t, db.Get(&n, "SELECT COUNT(*) FROM note_tags"))
	assert.Equal(t, 0, n, "the note's links are gone with it")

	// The tag row itself is kept: tags are shared and outlive the notes that used them.
	require.NoError(t, db.Get(&n, "SELECT COUNT(*) FROM tags WHERE name = 'go'"))
	assert.Equal(t, 1, n)
}

func TestOpenGivesEachCallItsOwnDatabase(t *testing.T) {
	first, err := Open(MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	second, err := Open(MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	_, err = first.Exec("INSERT INTO notes (id, title, body, created_at) VALUES (?, 't', 'b', CURRENT_TIMESTAMP)", testNoteID)
	require.NoError(t, err)

	var n int
	require.NoError(t, second.Get(&n, "SELECT COUNT(*) FROM notes"))
	assert.Equal(t, 0, n)
}
