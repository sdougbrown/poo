package pootest

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sdougbrown/poo"
)

// ErrSinkFailed is the failure the sink injects.
var ErrSinkFailed = errors.New("pootest: the sink was told to fail")

// Sink is an in-memory record sink.
//
// It keeps every record it accepted, and it can be told to fail: a test
// that needs a broken store sets the failure point and the sink refuses
// the record that reaches it.
//
// The sink serializes its writes, so a concurrent append never interleaves
// two records.
type Sink struct {
	mu sync.Mutex
	// records holds the records that were accepted, in order.
	records []poo.Record
	// seq numbers the records the sink stamped.
	seq int64
	// failAt is the number of the append that fails. Zero means every
	// append succeeds.
	failAt int
	// appends counts every append, including the failed one.
	appends int
	// flushed counts the flushes.
	flushed int
	// err is the failure the sink returns, when it is set.
	err error
}

// NewSink returns an empty sink.
func NewSink() *Sink {
	return &Sink{}
}

// FailAt makes the nth append fail, counting from one.
//
// The failure is the sink's own error unless one was set with SetError.
func (s *Sink) FailAt(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failAt = n
}

// SetError sets the failure the sink returns.
func (s *Sink) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// Append keeps one record.
//
// The write is serialized, so two concurrent appends never interleave.
// The record is stamped with the sink's own sequence number and
// timestamp, as a store does.
func (s *Sink) Append(ctx context.Context, rec poo.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.appends++
	if s.failAt != 0 && s.appends == s.failAt {
		if s.err != nil {
			return s.err
		}
		return ErrSinkFailed
	}
	s.seq++
	poo.Stamp(rec, s.seq, time.Now())
	s.records = append(s.records, rec)
	return nil
}

// Flush does nothing: the records are already in memory.
func (s *Sink) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushed++
	return nil
}

// Records returns the records that were accepted, in order.
//
// The slice is a copy, but the records in it are the sink's own: they are
// live, unlike the requests a Transport returns. A test that changes a
// record it fetched changes what the sink holds, so it should read the
// records and build its expectations apart from them.
func (s *Sink) Records() []poo.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]poo.Record(nil), s.records...)
}

// Names returns the records' names, in order.
func (s *Sink) Names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.records))
	for _, rec := range s.records {
		out = append(out, rec.Meta().Record)
	}
	return out
}

// Appends returns how many appends the sink saw, including the failed
// ones.
func (s *Sink) Appends() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appends
}

// Flushed returns how many times the sink was flushed.
func (s *Sink) Flushed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushed
}

// Messages returns the message records the sink kept, in order.
//
// The records are live, as Records' are.
func (s *Sink) Messages() []*poo.MessageRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*poo.MessageRecord
	for _, rec := range s.records {
		if m, ok := rec.(*poo.MessageRecord); ok {
			out = append(out, m)
		}
	}
	return out
}

// Settled returns the run's settle record, or nil when the run did not
// settle.
//
// The last settle record is the one returned: a session may run more
// than once, and a consumer wants the outcome of the run that just
// finished.
//
// The record is live, as Records' are.
func (s *Sink) Settled() *poo.RunSettled {
	s.mu.Lock()
	defer s.mu.Unlock()
	var last *poo.RunSettled
	for _, rec := range s.records {
		if r, ok := rec.(*poo.RunSettled); ok {
			last = r
		}
	}
	return last
}
