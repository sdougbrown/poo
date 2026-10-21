package poo

import "fmt"

// MessageIDs hands out the session-monotonic message ids.
//
// The ids are the `m<n>` the plan names: an assistant message's id is
// what a delivery note refers to, and the ids are stable across
// compaction and reload. A session owns one counter, so the ids never
// repeat within a session.
//
// The zero value is ready to use.
type MessageIDs struct {
	// next is the number the next id gets.
	next int
}

// Next returns the next message id.
//
// The first id is "m1".
func (ids *MessageIDs) Next() string {
	ids.next++
	return fmt.Sprintf("m%d", ids.next)
}

// Peek returns the id the next call to Next will return, without
// handing it out.
func (ids *MessageIDs) Peek() string {
	return fmt.Sprintf("m%d", ids.next+1)
}

// Set moves the counter to the given number.
//
// A session that reloads its records uses this to carry on after the
// ids it already assigned. A number below the current one is ignored, so
// the ids never go backwards.
func (ids *MessageIDs) Set(n int) {
	if n > ids.next {
		ids.next = n
	}
}
