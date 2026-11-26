package session

import (
	"context"
	"sync"

	"github.com/sdougbrown/poo"
)

// Recording is a store that keeps the records it writes.
//
// The store writes every record to the store it wraps, and keeps a copy
// of it. A consumer that serves a session's history needs the
// records: the history is a projection of them, and a store that
// only writes them to a file cannot hand them over.
type Recording struct {
	// Store is the store the records are written to. A recording over
	// nothing keeps its records in memory.
	Store

	// mu guards the records.
	mu sync.Mutex
	// records are the records that were written, in order.
	records []poo.Record
}

// Append writes a record and keeps it.
func (r *Recording) Append(ctx context.Context, rec poo.Record) error {
	if r.Store != nil {
		// The buffer is a copy of what the store accepted: a record the
		// write refused is in neither.
		if err := r.Store.Append(ctx, rec); err != nil {
			return err
		}
	}
	r.mu.Lock()
	r.records = append(r.records, rec)
	r.mu.Unlock()
	return nil
}

// ReadRecords reads the session's records back, oldest first.
//
// It is the read side poo.RecordReader asks for. The recording writes
// through its store, so a store that can read its chain back serves
// the read: a session restored from an earlier one reads what the
// store retained, not just what this process wrote. Without a readable
// store, the memory is what there is.
func (r *Recording) ReadRecords(ctx context.Context) ([]poo.Record, error) {
	if reader, ok := r.Store.(poo.RecordReader); ok {
		return reader.ReadRecords(ctx)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]poo.Record, len(r.records))
	copy(out, r.records)
	return out, nil
}

// Records returns the records that were written, oldest first.
//
// The copy is the caller's: a consumer that changes it does not change
// what the store holds.
func (r *Recording) Records() []poo.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]poo.Record(nil), r.records...)
}

// RecordsWith returns the records of a loaded session: the ones that
// were read, and the ones that were written since.
func (r *Recording) RecordsWith(loaded []poo.Record) []poo.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]poo.Record, 0, len(loaded)+len(r.records))
	out = append(out, loaded...)
	out = append(out, r.records...)
	return out
}

// LeaseLost returns the lease-loss channel of the store the recording
// writes through, when that store's ownership is a lease.
//
// The session asserts its sink for LeaseLostWatch when it starts, and a
// session served over the wire writes through the recording: without this
// pass-through, the wire's sessions would keep running after their lease
// was taken over, and only a refused write would stop them.
func (r *Recording) LeaseLost() <-chan struct{} {
	if watcher, ok := r.Store.(poo.LeaseLostWatch); ok {
		return watcher.LeaseLost()
	}
	return nil
}
