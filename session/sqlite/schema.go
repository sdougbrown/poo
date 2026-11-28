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
	"os"
	"strings"
	"syscall"
	"time"

	sqlite3 "modernc.org/sqlite"
	sqlite3lib "modernc.org/sqlite/lib"
)

// The schema's version. A migration is applied when the database's
// recorded version is older than this one.
const schemaVersion = 1

// The busy timeout, in milliseconds. A write that finds the database
// locked waits this long before it fails, so a brief reader does not
// refuse a writer.
const busyTimeoutMS = 5000

// openRetryInterval is how long an open waits between attempts while
// another opener converts a fresh database to WAL mode.
const openRetryInterval = 2 * time.Millisecond

// ErrSchema is the failure a schema migration reports.
var ErrSchema = errors.New("session/sqlite: the schema could not be migrated")

// Open opens the SQLite database at path, sets WAL mode and the busy
// timeout, and runs the schema migrations.
//
// The database is private to its owner: it holds the user's conversation,
// so the database file and the WAL files the driver keeps beside it are
// created 0600 before the driver touches them, and a database an older run
// left readable is narrowed.
//
// The database is opened with WAL journaling, so a reader never blocks a
// writer and a writer never blocks a reader. The busy timeout lets a
// writer wait out a brief reader instead of failing at once. Every
// transaction the caller begins is an immediate one: it takes the write
// lock at the start, so a read-then-write is atomic. Commits are fully
// synced: the store promises a needs-sync record is on the disk before
// the append returns, and WAL's own default would leave that to the
// next checkpoint.
//
// Open may be called concurrently, including from other processes: a
// fresh database is migrated once, and an opener that races another's
// journal-mode change waits it out.
func Open(path string) (*sql.DB, error) {
	if err := ensurePrivate(path); err != nil {
		return nil, err
	}
	dsn := path
	if strings.Contains(path, "?") {
		dsn += "&"
	} else {
		dsn += "?"
	}
	dsn += fmt.Sprintf("_journal_mode=WAL&_busy_timeout=%d&_txlock=immediate&_pragma=synchronous(FULL)", busyTimeoutMS)
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
	if err := openConnection(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// openConnection opens the database's first connection.
//
// The connection applies the DSN's journal mode, and SQLite changes a
// fresh database's journal mode without invoking the busy handler: an
// opener that races another opener's conversion is refused at once
// instead of waiting the busy timeout. The wait is retried here, for that
// same timeout, so opener races on a fresh database all come up.
func openConnection(db *sql.DB) error {
	deadline := time.Now().Add(busyTimeoutMS * time.Millisecond)
	for {
		err := db.Ping()
		if err == nil {
			return nil
		}
		if !isBusy(err) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(openRetryInterval)
	}
}

// isBusy reports whether an error is SQLite refusing a lock.
func isBusy(err error) bool {
	var sqliteErr *sqlite3.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	switch sqliteErr.Code() & 0xff {
	case sqlite3lib.SQLITE_BUSY, sqlite3lib.SQLITE_LOCKED:
		return true
	}
	return false
}

// beforePrivateOpen is called between the check and the open of each
// private file. It is nil in production: a test sets it to plant an
// entry in that window, which is how the open's own refusal is exercised.
// It is a test hook, called between the check and the open; the package's
// tests are not parallel.
var beforePrivateOpen func(path string)

// ensurePrivate makes the database's files private to their owner.
//
// The driver would create a missing database with its own mode, so each
// file the database and its WAL journal use is created here instead:
// 0600, and not truncated when it already exists. A zero-length WAL is a
// fresh one to the driver. A file an older run left readable is narrowed.
//
// The path may carry a DSN query, which the driver receives whole: only
// the database file and its siblings are touched here. An existing entry
// that is not a regular file is refused, and each file is opened without
// following a link and narrowed through its descriptor: a symlink planted
// in a directory another user can write would otherwise be chmod'd and
// written through.
func ensurePrivate(path string) error {
	dbPath, _, _ := strings.Cut(path, "?")
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		info, err := os.Lstat(p)
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("session/sqlite: making %s private: it is not a regular file", p)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("session/sqlite: making %s private: %w", p, err)
		}
		// O_NOFOLLOW refuses a symlink the Lstat did not see, one planted
		// between the check and the open. O_NONBLOCK keeps a FIFO planted
		// in that window from hanging the open, which would block on it
		// forever: the open returns at once, and a FIFO that does open,
		// one with a reader, is refused by the handle's regular-file check
		// below.
		if beforePrivateOpen != nil {
			beforePrivateOpen(p)
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
		if err != nil {
			if errors.Is(err, syscall.ELOOP) {
				// The open refused the link. The cause keeps its
				// identity, so a caller can tell this refusal from
				// the Lstat's by what the error is, not its wording.
				return fmt.Errorf("session/sqlite: making %s private: the open refused a link: %w", p, syscall.ELOOP)
			}
			return fmt.Errorf("session/sqlite: making %s private: %w", p, err)
		}
		info, err = f.Stat()
		if err != nil {
			f.Close()
			return fmt.Errorf("session/sqlite: making %s private: %w", p, err)
		}
		if !info.Mode().IsRegular() {
			f.Close()
			return fmt.Errorf("session/sqlite: making %s private: it is not a regular file", p)
		}
		// The descriptor is the file that was verified: the mode is set
		// through it, so no path is resolved a second time.
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return fmt.Errorf("session/sqlite: making %s private: %w", p, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("session/sqlite: making %s private: %w", p, err)
		}
	}
	return nil
}

// migrate runs the schema migrations the database has not had.
//
// The version is read and the migration applied inside one immediate
// transaction, so a concurrent open serializes on the write lock: the
// second reader sees the version the first wrote and applies nothing. A
// failure leaves the schema whole: either the database is at the new
// version, or it is at the old one, never half.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The version is read under the transaction's write lock, so the read
	// the migration decides on is the one every other writer sees.
	var current int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	if current >= schemaVersion {
		return tx.Commit()
	}
	if err := applyV1(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, schemaVersion); err != nil {
		// A version row that landed another way is not a failure: the
		// migration is done when the version is current.
		tx.Rollback()
		if readErr := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); readErr != nil {
			return err
		}
		if current >= schemaVersion {
			return nil
		}
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
