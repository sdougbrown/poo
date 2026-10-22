package session

import (
	"context"
	"sync"
	"time"

	"github.com/sdougbrown/poo"
)

// Memory is the ephemeral store.
//
// The memory keeps a session's records for the length of the process and
// writes no file: an ephemeral session has no file, no lock and no
// recovery. Loading an ephemeral session fails with
// ErrSessionNotFound: there is nothing on disk to load, and a session
// that was never written cannot be read back.
type Memory struct {
	mu sync.Mutex
	// records holds the records, in order.
	records []poo.Record
	// seq numbers the records the memory stamped.
	seq int64
}

// NewMemory returns an empty memory.
func NewMemory() *Memory { return &Memory{} }

// Append keeps one record.
//
// The write is serialized, so a concurrent append never interleaves two
// records. The record is stamped with the memory's own sequence number
// and timestamp, as a store does.
func (m *Memory) Append(ctx context.Context, rec poo.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.seq++
	poo.Stamp(rec, m.seq, time.Now())
	m.records = append(m.records, rec)
	return nil
}

// Flush does nothing: the records are already in memory.
func (m *Memory) Flush(ctx context.Context) error { return nil }

// Records returns the records the memory kept, in order.
//
// The slice is a copy, but the records in it are the memory's own: they
// are live.
func (m *Memory) Records() []poo.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]poo.Record(nil), m.records...)
}

// Create starts a session in the memory.
//
// The session's records are kept in memory, like every other record: the
// memory is a store without a file.
func (m *Memory) Create(ctx context.Context, created *poo.SessionCreated) (string, error) {
	if err := CheckMode(Mode(created.Mode)); err != nil {
		return "", err
	}
	if created.SessionID == "" {
		return "", &Error{Code: CodeStoreError, Message: "the session has no id"}
	}
	if created.Format == "" {
		created.Format = poo.FormatVersion
	}
	if created.CreatedAt == "" {
		created.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := m.Append(ctx, created); err != nil {
		return "", &Error{Code: CodeStoreError, Message: "writing the session", Err: err}
	}
	return created.SessionID, nil
}

// Load fails: an ephemeral session is not on disk.
//
// The memory holds the session that is running, and the core keeps its
// records. A session that was never written cannot be read back.
func (m *Memory) Load(ctx context.Context, id string) (*Loaded, error) {
	return nil, ErrSessionNotFound
}

// Pending returns the operations the memory's session has not resolved.
func (m *Memory) Pending(ctx context.Context, id string) (*Pending, error) {
	return pendingOf(id, m.Records()), nil
}

// List returns the summary of the session the memory holds.
func (m *Memory) List(ctx context.Context) ([]Summary, error) {
	recs := m.Records()
	if len(recs) == 0 {
		return nil, nil
	}
	created, ok := recs[0].(*poo.SessionCreated)
	if !ok {
		return nil, &Error{Code: CodeCorruptLog, Message: "the session does not start with a session"}
	}
	meta := metadataOfRecord(created)
	return []Summary{Projection{}.Summarize(meta, recs)}, nil
}

// Prune deletes nothing: the memory holds no files.
func (m *Memory) Prune(ctx context.Context, policy Policy) (*PruneResult, error) {
	if policy.MaxAge <= 0 && policy.Keep <= 0 {
		return nil, ErrNoPolicy
	}
	return &PruneResult{}, nil
}

// Close releases the memory's records.
func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = nil
	return nil
}
