package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAppliesMigrations(t *testing.T) {
	db, err := Open(MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var version int
	require.NoError(t, db.Get(&version, "SELECT version FROM schema_migrations"))
	assert.Equal(t, 2, version)

	_, err = db.Exec("INSERT INTO notes (title, body, created_at) VALUES ('t', 'b', CURRENT_TIMESTAMP)")
	assert.NoError(t, err)
}

// TestMigrationsAreReversible runs the embedded down migration of the tag migration and checks it
// undoes exactly its own up migration: the tag tables go, the notes table and its rows stay.
func TestMigrationsAreReversible(t *testing.T) {
	db, err := Open(MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("INSERT INTO notes (title, body, created_at) VALUES ('kept', 'b', CURRENT_TIMESTAMP)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO tags (name) VALUES ('go')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO note_tags (note_id, tag_id) VALUES (1, 1)")
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
	require.NoError(t, db.Get(&title, "SELECT title FROM notes WHERE id = 1"))
	assert.Equal(t, "kept", title, "the notes table and its rows survive")
}

// TestDeletingNoteCascadesNoteTags checks that the foreign key on note_tags is enforced: deleting
// a note takes its links with it instead of leaving rows pointing at a note that is gone.
func TestDeletingNoteCascadesNoteTags(t *testing.T) {
	db, err := Open(MemoryDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("INSERT INTO notes (title, body, created_at) VALUES ('t', 'b', CURRENT_TIMESTAMP)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO tags (name) VALUES ('go')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO note_tags (note_id, tag_id) VALUES (1, 1)")
	require.NoError(t, err)

	_, err = db.Exec("DELETE FROM notes WHERE id = 1")
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

	_, err = first.Exec("INSERT INTO notes (title, body, created_at) VALUES ('t', 'b', CURRENT_TIMESTAMP)")
	require.NoError(t, err)

	var n int
	require.NoError(t, second.Get(&n, "SELECT COUNT(*) FROM notes"))
	assert.Equal(t, 0, n)
}
