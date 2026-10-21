package poo

import (
	"fmt"
	"sync"
)

// MessageIDs hands out the session-monotonic message ids.
//
// The ids are the `m<n>` the plan names: an assistant message's id is
// what a delivery note refers to, and the ids are stable across
// compaction and reload. A session owns one counter, so the ids never
// repeat within a session.
//
// The counter is safe for concurrent use: a run's streaming goroutine
// and a job's completion can both ask for an id.
//
// The zero value is ready to use.
type MessageIDs struct {
	mu sync.Mutex
	// next is the number the next id gets.
	next int
}

// Next returns the next message id.
//
// The first id is "m1".
func (ids *MessageIDs) Next() string {
	ids.mu.Lock()
	defer ids.mu.Unlock()
	ids.next++
	return fmt.Sprintf("m%d", ids.next)
}

// Peek returns the id the next call to Next will return, without
// handing it out.
func (ids *MessageIDs) Peek() string {
	ids.mu.Lock()
	defer ids.mu.Unlock()
	return fmt.Sprintf("m%d", ids.next+1)
}

// Set moves the counter to the given number.
//
// A session that reloads its records uses this to carry on after the
// ids it already assigned. A number below the current one is ignored, so
// the ids never go backwards.
func (ids *MessageIDs) Set(n int) {
	ids.mu.Lock()
	defer ids.mu.Unlock()
	if n > ids.next {
		ids.next = n
	}
}
