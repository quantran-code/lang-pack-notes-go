package database

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

const (
	// notesUUIDMigrationVersion is the migration that turns note ids into UUIDs. The number matches
	// the 000004_ prefix of the two files it is made of.
	notesUUIDMigrationVersion int = 4

	// migrationsDir is the embedded directory the migration files live in.
	migrationsDir = "migrations"

	// notesUUIDUpFile is the up half of notesUUIDMigrationVersion: the one migration whose data
	// cannot be written in SQL, because SQLite cannot mint a UUID.
	notesUUIDUpFile = "000004_notes_uuid.up.sql"

	// notesUUIDDownFile is the down half of notesUUIDMigrationVersion.
	notesUUIDDownFile = "000004_notes_uuid.down.sql"

	// notesUUIDMarker is the line of notesUUIDUpFile that the generated note-to-UUID mapping
	// replaces. It is a line of its own rather than part of a comment, so the statement that
	// replaces it runs.
	notesUUIDMarker = "{{note_id_map}}"
)

// noteIDPair is one note of the migration: the integer id it has before it and the UUID it gets.
// NewID is a canonical lowercase hyphenated UUID v4, taken from uuid.New().String().
type noteIDPair struct {
	OldID int64
	NewID string
}

// existingNoteIDs returns the integer ids of the notes stored right now, in id order: exactly the
// rows notesUUIDMigrationVersion has to give a UUID to. A database without a notes table yet (a
// fresh one) and a database whose notes already carry UUIDs (one that was migrated before) both
// have no such rows, so both report no ids instead of an error.
func existingNoteIDs(ctx context.Context, db *sqlx.DB) ([]int64, error) {
	var tables int
	if err := db.GetContext(ctx, &tables,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'notes'"); err != nil {
		return nil, fmt.Errorf("looking up the notes table: %w", err)
	}
	if tables == 0 {
		return nil, nil
	}

	ids := make([]int64, 0)
	if err := db.SelectContext(ctx, &ids,
		"SELECT id FROM notes WHERE typeof(id) = 'integer' ORDER BY id"); err != nil {
		return nil, fmt.Errorf("listing note ids: %w", err)
	}
	return ids, nil
}

// noteIDMapStatements renders pairs as the one statement that fills the migration's temporary map,
// or as the empty string when there is nothing to map: the up migration stays valid SQL either way,
// and a database without notes simply has nothing to remap.
func noteIDMapStatements(pairs []noteIDPair) string {
	if len(pairs) == 0 {
		return ""
	}
	values := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		values = append(values, fmt.Sprintf("(%d, '%s')", pair.OldID, pair.NewID))
	}
	return "INSERT INTO note_id_map (old_id, new_id) VALUES " + strings.Join(values, ", ") + ";"
}

// migrationsWithNotesUUID returns the embedded migrations with the up half of
// notesUUIDMigrationVersion replaced by the version that carries the generated mapping: every note
// stored right now gets a fresh UUID from uuid.New().String(), minted here because SQL has no way
// to mint one. The result is what the second migration pass reads, so the marker never reaches the
// migrator. The set is rooted at the migrations directory, so it is opened at ".".
func migrationsWithNotesUUID(ctx context.Context, db *sqlx.DB) (fs.FS, error) {
	ids, err := existingNoteIDs(ctx, db)
	if err != nil {
		return nil, err
	}
	pairs := make([]noteIDPair, 0, len(ids))
	for _, id := range ids {
		pairs = append(pairs, noteIDPair{OldID: id, NewID: uuid.New().String()})
	}

	files, err := embeddedMigrations()
	if err != nil {
		return nil, err
	}
	up, ok := files[notesUUIDUpFile]
	if !ok {
		return nil, fmt.Errorf("migration %s is not embedded", notesUUIDUpFile)
	}
	if !bytes.Contains(up, []byte(notesUUIDMarker)) {
		return nil, fmt.Errorf("migration %s no longer holds the %s marker", notesUUIDUpFile, notesUUIDMarker)
	}
	return files.with(notesUUIDUpFile,
		bytes.Replace(up, []byte(notesUUIDMarker), []byte(noteIDMapStatements(pairs)), 1)), nil
}

// migrationsBeforeNotesUUID returns the embedded migrations without notesUUIDMigrationVersion, so
// the first pass stops before it: what applies then is the plain SQL of the versions before it.
func migrationsBeforeNotesUUID() (migrationSet, error) {
	files, err := embeddedMigrations()
	if err != nil {
		return nil, err
	}
	return files.without(notesUUIDUpFile, notesUUIDDownFile), nil
}

// embeddedMigrations reads every embedded migration file into a set keyed by file name, rooted at
// the migrations directory, so a migrator can read the set at "."..
func embeddedMigrations() (migrationSet, error) {
	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("listing migrations: %w", err)
	}
	files := make(migrationSet, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := migrationsFS.ReadFile(path.Join(migrationsDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}
		files[entry.Name()] = data
	}
	return files, nil
}

// migrationSet is an fs.FS holding the migration files in memory, keyed by file name. It exists so
// the migration pass that needs generated SQL reads the embedded files with exactly one of them
// replaced, without anything being written to disk. The set is flat: it has one directory, ".".
type migrationSet map[string][]byte

// Open opens the directory "." or one of the files of the set.
func (s migrationSet) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return &migrationSetDir{entries: s.dirEntries()}, nil
	}
	data, ok := s[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &migrationSetFile{name: name, data: data}, nil
}

// ReadDir lists the files of the set, so a reader does not have to open the directory to list it.
func (s migrationSet) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) || name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return s.dirEntries(), nil
}

// dirEntries lists the files of the set in name order, so listing it twice gives the same answer.
func (s migrationSet) dirEntries() []fs.DirEntry {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]fs.DirEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, migrationSetEntry{name: name, size: int64(len(s[name]))})
	}
	return entries
}

// without returns a copy of the set with the named files left out, which is how a migration pass
// stops before a version whose file it must not read yet.
func (s migrationSet) without(names ...string) migrationSet {
	out := make(migrationSet, len(s))
	for name, data := range s {
		out[name] = data
	}
	for _, name := range names {
		delete(out, name)
	}
	return out
}

// with returns a copy of the set with the named file replaced by data.
func (s migrationSet) with(name string, data []byte) migrationSet {
	out := make(migrationSet, len(s))
	for file, content := range s {
		out[file] = content
	}
	out[name] = data
	return out
}

// migrationSetEntry is how a file of the set, and the directory holding them, is reported: a
// regular file of its size, or a directory.
type migrationSetEntry struct {
	name string
	size int64
	dir  bool
}

func (e migrationSetEntry) Name() string {
	return e.name
}

func (e migrationSetEntry) Size() int64 {
	return e.size
}

func (e migrationSetEntry) Mode() fs.FileMode {
	if e.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

func (e migrationSetEntry) ModTime() time.Time {
	return time.Time{}
}

func (e migrationSetEntry) IsDir() bool {
	return e.dir
}

func (e migrationSetEntry) Sys() any {
	return nil
}

func (e migrationSetEntry) Type() fs.FileMode {
	return e.Mode().Type()
}

func (e migrationSetEntry) Info() (fs.FileInfo, error) {
	return e, nil
}

// migrationSetFile is one file of a migrationSet, read from its start.
type migrationSetFile struct {
	name string
	data []byte
	read int
}

func (f *migrationSetFile) Stat() (fs.FileInfo, error) {
	return migrationSetEntry{name: f.name, size: int64(len(f.data))}, nil
}

func (f *migrationSetFile) Read(p []byte) (int, error) {
	if f.read >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.read:])
	f.read += n
	return n, nil
}

func (f *migrationSetFile) Close() error { return nil }

// migrationSetDir is the one directory of a migrationSet: the files it holds.
type migrationSetDir struct {
	entries []fs.DirEntry
	read    int
}

func (d *migrationSetDir) Stat() (fs.FileInfo, error) {
	return migrationSetEntry{name: ".", dir: true}, nil
}

func (d *migrationSetDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid}
}

func (d *migrationSetDir) Close() error { return nil }

func (d *migrationSetDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 {
		entries := d.entries[d.read:]
		d.read = len(d.entries)
		return entries, nil
	}
	if d.read >= len(d.entries) {
		return nil, io.EOF
	}
	end := d.read + n
	if end > len(d.entries) {
		end = len(d.entries)
	}
	entries := d.entries[d.read:end]
	d.read = end
	return entries, nil
}
