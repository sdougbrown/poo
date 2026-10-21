// Package session writes the session log.
//
// The log is append-only JSONL: one record per line, no lock, no recovery.
// The record names and fields are the v0 vocabulary, documented in
// docs/session-log-v0.md.
package session

import (
	"bufio"
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
type Log struct {
	mu sync.Mutex
	f  *os.File
	w  *bufio.Writer
	// seq numbers the records written.
	seq int64
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
	return &Log{f: f, w: bufio.NewWriter(f)}, nil
}

// Append writes one record.
//
// The context is checked before anything else: a write whose budget the
// caller already spent is refused without consuming a sequence number,
// so the log's numbering stays dense.
func (l *Log) Append(ctx context.Context, rec poo.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return fmt.Errorf("session: the log is closed")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("session: appending %s: %w", rec.Meta().Record, err)
	}
	l.seq++
	poo.Stamp(rec, l.seq, time.Now())

	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("session: encoding %s: %w", rec.Meta().Record, err)
	}
	b = append(b, '\n')
	if _, err := l.w.Write(b); err != nil {
		return fmt.Errorf("session: writing %s: %w", rec.Meta().Record, err)
	}
	return nil
}

// Flush pushes buffered records to the file.
func (l *Log) Flush(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("session: flushing: %w", err)
	}
	return l.w.Flush()
}

// Close flushes and closes the log.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.w.Flush()
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
