// Package database opens the SQLite database and brings its schema up to date with the embedded
// golang-migrate migrations.
package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MemoryDSN is an in-memory SQLite database. Every call to Open gets its own, because the pool
// below holds a single connection and an in-memory database lives and dies with its connection.
const MemoryDSN = ":memory:"

// Open connects to dsn with the pure-Go "sqlite" driver and applies every pending migration.
// The pool is limited to one connection: SQLite allows one writer at a time anyway, and it keeps
// an in-memory database from being split across connections.
func Open(dsn string) (*sqlx.DB, error) {
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	db.SetMaxOpenConns(1)

	// SQLite enforces foreign keys only when this pragma is on, and it is off by default. The
	// pool above holds a single connection, so switching it on here covers every later query.
	// It runs before the migrations so the ON DELETE CASCADE of note_tags is real from the start.
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	if err := migrateUp(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// migrateUp applies the embedded migrations in order, in two passes around the one migration whose
// data cannot be written in SQL. The notes of migration 000004 are given UUIDs that SQLite cannot
// mint itself, so they are generated in Go from the rows that are actually there.
//
// The first pass applies the plain SQL migrations up to the version before 000004. Only then are the
// new ids generated, and the second pass applies 000004 from a source built by
// migrationsWithNotesUUID, which is the same set of files with that migration's marker replaced by
// the generated mapping. Applying nothing is not an error. The migrators are deliberately not
// closed: closing one would close db with it.
func migrateUp(db *sqlx.DB) error {
	version, migrated, err := schemaVersion(db)
	if err != nil {
		return err
	}

	// A database that is already past 000004 has nothing left to apply from the versions below it,
	// and migrating it down to them is precisely what must not happen, so that pass is left out
	// entirely. A database already at notesUUIDMigrationVersion is therefore left exactly as it is.
	if !migrated || version < notesUUIDMigrationVersion {
		files, err := migrationsBeforeNotesUUID()
		if err != nil {
			return err
		}
		// 000004 is not applicable yet: its per-note UUIDs have not been generated, so the marker in
		// its file is still the one it is embedded with.
		plain, err := newMigrator(db, files)
		if err != nil {
			return err
		}
		if err := plain.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("applying migrations: %w", err)
		}
	}

	// Read the ids only now, with the schema at the version before 000004: the notes read here are
	// exactly the notes the migration converts. A database that already holds UUID ids has none of
	// them, so its 000004 applies nothing.
	generated, err := migrationsWithNotesUUID(context.Background(), db)
	if err != nil {
		return err
	}
	m, err := newMigrator(db, generated)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

// newMigrator builds a migrator over the migration files of fsys, which is read at ".".
func newMigrator(db *sqlx.DB, fsys fs.FS) (*migrate.Migrate, error) {
	source, err := iofs.New(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("reading migrations: %w", err)
	}
	driver, err := migratesqlite.WithInstance(db.DB, &migratesqlite.Config{})
	if err != nil {
		return nil, fmt.Errorf("preparing migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "sqlite", driver)
	if err != nil {
		return nil, fmt.Errorf("creating migrator: %w", err)
	}
	return m, nil
}

// schemaVersion returns the migration version the database is at, and whether it has been migrated
// at all. The version lives in the table golang-migrate keeps, which does not exist before the first
// migration runs.
func schemaVersion(db *sqlx.DB) (int, bool, error) {
	var tables int
	if err := db.Get(&tables,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'"); err != nil {
		return 0, false, fmt.Errorf("looking up the migration table: %w", err)
	}
	if tables == 0 {
		return 0, false, nil
	}

	var version int
	if err := db.Get(&version, "SELECT version FROM schema_migrations LIMIT 1"); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("reading the migration version: %w", err)
	}
	return version, true, nil
}
