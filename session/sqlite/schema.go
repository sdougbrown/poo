// Package sqlite is an opt-in, pure-Go SQLite-backed session store.
//
// It implements the session.Store contract over a single SQLite
// database: sessions, their records, and their leases are rows, and every
// write is a transaction. The store is the opt-in alternative to the
// file-backed store: a caller that wants a single-file store, indexed
// queries, and SQLite-backed leases opens one over a path.
//
// The package is self-contained: nothing in the core poo or the session
// package imports it. A caller wires it in by opening a store and handing
// it to a core session, the way it would hand one the file-backed store.
package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// The schema's version. A migration is applied when the database's
// recorded version is older than this one.
const schemaVersion = 1

// The busy timeout, in milliseconds. A write that finds the database
// locked waits this long before it fails, so a brief reader does not
// refuse a writer.
const busyTimeoutMS = 5000

// ErrSchema is the failure a schema migration reports.
var ErrSchema = errors.New("session/sqlite: the schema could not be migrated")

// Open opens the SQLite database at path, sets WAL mode and the busy
// timeout, and runs the schema migrations.
//
// The database is opened with WAL journaling, so a reader never blocks a
// writer and a writer never blocks a reader. The busy timeout lets a
// writer wait out a brief reader instead of failing at once. Every
// transaction the caller begins is an immediate one: it takes the write
// lock at the start, so a read-then-write is atomic.
func Open(path string) (*sql.DB, error) {
	dsn := path
	if strings.Contains(path, "?") {
		dsn += "&"
	} else {
		dsn += "?"
	}
	dsn += fmt.Sprintf("_journal_mode=WAL&_busy_timeout=%d&_txlock=immediate", busyTimeoutMS)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// The connections are not held to one: a listing reads with one
	// while it summarizes each session through another, and a lease's
	// heartbeat writes with its own. The write serialization is the
	// database's — one immediate transaction at a time, waited out by
	// the busy timeout — and WAL keeps the readers out of the writer's
	// way.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// migrate runs the schema migrations the database has not had.
//
// The migrations are applied in a single transaction, so a failure
// leaves the schema whole: either the database is at the new version, or
// it is at the old one, never half.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	if current >= schemaVersion {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := applyV1(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, schemaVersion); err != nil {
		return err
	}
	return tx.Commit()
}

// applyV1 creates the schema's first version: the sessions, the records,
// and the leases, and the records' indexes.
func applyV1(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			format TEXT,
			mode TEXT,
			created_at TEXT,
			updated_at TEXT,
			metadata_json TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS records (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT,
			type TEXT,
			run_id TEXT,
			payload TEXT,
			created_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_records_session_seq ON records (session_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_records_session_type ON records (session_id, type)`,
		`CREATE TABLE IF NOT EXISTS leases (
			session_id TEXT PRIMARY KEY,
			owner_id TEXT,
			pid INTEGER,
			host TEXT,
			token TEXT,
			acquired_at TEXT,
			heartbeat_at TEXT,
			expires_at TEXT
		)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
