// Package session holds a session's records.
//
// The package has three parts. The Log is an append-only JSONL writer:
// one record per line, no recovery. The Store is the file-backed session
// store: it creates sessions, loads them, lists them and prunes them,
// and it holds the authority over a session while it is open. The Memory
// is the ephemeral store: it keeps a session's records for the length of
// the process and writes no file.
//
// A session's records are the log's vocabulary, documented in
// docs/session-log-v0.md. The log takes a lock around each write, so a
// concurrent append never interleaves two records' bytes. The lock is a
// mutex, not a file lock: the file lock that keeps two processes out of
// one session is the store's authority.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sdougbrown/poo"
)

// Log is an append-only session log.
//
// The log implements poo.RecordSink: Append may be called concurrently,
// and the log serializes the writes, so two records' bytes never
// interleave.
//
// Every record is written as one write of the line and its newline, so
// a reader of the log never sees half a record, and a record that was
// written is a record that is on disk.
type Log struct {
	mu sync.Mutex
	f  *os.File
	// path is the log's path.
	path string
	// seq numbers the records written.
	seq int64
	// syncs counts the syncs the log made. It is a test hook.
	syncs int
	// appends counts the appends the log made.
	appends int
	// appendFault is the number of appends that succeed before an
	// append fails. It is a test hook.
	appendFault int
}

// Open creates a session log at a path.
//
// The file is created exclusively, so an existing log is never extended:
// a session log is one session's record, and appending a second
// session.created to it would break the vocabulary the log promises.
//
// The directory the log lives in is created when it is missing.
func Open(path string) (*Log, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("session: creating %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("session: %s already exists", path)
		}
		return nil, fmt.Errorf("session: opening %s: %w", path, err)
	}
	return &Log{f: f, path: path}, nil
}

// OpenExisting opens a log that already exists, for appending.
//
// The log continues the file's numbering: the records already in the
// file are counted, so the first record the log writes carries the next
// number. A torn tail is not counted: a tail that was cut short is the
// store's business, and the store truncates it before it appends.
func OpenExisting(path string) (*Log, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session: opening %s: %w", path, err)
	}
	seq, err := countRecords(path)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Log{f: f, path: path, seq: seq}, nil
}

// countRecords returns the number of whole records a log holds.
func countRecords(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("session: reading %s: %w", path, err)
	}
	var count int64
	for _, line := range bytesLines(data) {
		if len(line) == 0 {
			continue
		}
		count++
	}
	return count, nil
}

// bytesLines returns the whole lines of a log's bytes.
//
// A tail that was cut short is not a line: the log was cut in the middle
// of one.
func bytesLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] != '\n' {
			continue
		}
		out = append(out, data[start:i])
		start = i + 1
	}
	return out
}

// Path returns the log's path.
func (l *Log) Path() string { return l.path }

// Append writes one record.
//
// The write is serialized, so a concurrent append never interleaves two
// records' bytes: a reader of the log never sees half a record.
//
// A context that is already done is refused before any write work,
// without consuming a sequence number, so the log's numbering stays
// dense. The closed-log check below is a precondition, not write work.
func (l *Log) Append(ctx context.Context, rec poo.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return fmt.Errorf("session: the log is closed")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("session: appending %s: %w", rec.Meta().Record, err)
	}
	l.appends++
	if l.appendFault > 0 && l.appends == l.appendFault {
		return fmt.Errorf("session: appending %s: %w", rec.Meta().Record, errInjected)
	}
	l.seq++
	poo.Stamp(rec, l.seq, time.Now())

	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("session: encoding %s: %w", rec.Meta().Record, err)
	}
	b = append(b, '\n')
	if _, err := l.f.Write(b); err != nil {
		return fmt.Errorf("session: writing %s: %w", rec.Meta().Record, err)
	}
	return nil
}

// Flush pushes buffered records to the file.
//
// The log writes every record as it comes, so there is nothing buffered
// and a flush has nothing to push.
func (l *Log) Flush(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("session: flushing: %w", err)
	}
	return nil
}

// Sync puts the log's records on disk.
//
// The sync is what makes a record durable: the file's contents reach the
// disk, so a crash cannot lose a record the log already wrote.
func (l *Log) Sync(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("session: syncing: %w", err)
	}
	if err := l.f.Sync(); err != nil {
		return fmt.Errorf("session: syncing %s: %w", l.path, err)
	}
	l.syncs++
	return nil
}

// Close flushes and closes the log.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Sync()
	cerr := l.f.Close()
	l.f = nil
	if err != nil {
		return err
	}
	return cerr
}

// DefaultPath returns the path a session log gets when none is given.
//
// The name carries a timestamp, so concurrent runs in one directory do not
// collide.
func DefaultPath(dir string, at time.Time) string {
	name := "poo-session-" + at.UTC().Format("20060102T150405.000000000Z") + ".jsonl"
	return filepath.Join(dir, name)
}
