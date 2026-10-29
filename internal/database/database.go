// Package database opens the SQLite database and brings its schema up to date with the embedded
// golang-migrate migrations.
package database

import (
	"embed"
	"errors"
	"fmt"

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

// migrateUp applies the embedded migrations in order. Nothing to apply is not an error. The
// migrate instance is deliberately not closed: closing it would close db with it.
func migrateUp(db *sqlx.DB) error {
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("reading migrations: %w", err)
	}
	driver, err := migratesqlite.WithInstance(db.DB, &migratesqlite.Config{})
	if err != nil {
		return fmt.Errorf("preparing migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}
