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
	// Store is the store the records are written to.
	Store

	// mu guards the records.
	mu sync.Mutex
	// records are the records that were written, in order.
	records []poo.Record
}

// Append writes a record and keeps it.
func (r *Recording) Append(ctx context.Context, rec poo.Record) error {
	r.mu.Lock()
	r.records = append(r.records, rec)
	r.mu.Unlock()
	return r.Store.Append(ctx, rec)
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
