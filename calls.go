package poo

import "sort"

// The calls the records hold.
//
// A tool call's provider id lives in the assistant message that carries
// it: the message is the model's view, and the view is what the
// provider matches a result against. A call that the records pair with
// no message was never in the model's view, and nothing refers to it.

// callTurn names one turn of one run.
type callTurn struct {
	// run is the run's identifier.
	run string
	// turn is the turn's number in its run.
	turn int
}

// CallIDs returns the provider's ids of the calls the records hold, by
// the calls' handles.
//
// The calls of a turn are the ones the model made, in that order: the
// k-th call record of a turn is the k-th call block of that turn's
// messages. The records are ordered by their handles rather than by the
// order they were written in, because a batch of read-only calls runs
// at the same time and its records land in any order.
//
// A call the records pair with no message has no id: the model never
// saw the call, so nothing refers to it.
func CallIDs(records []Record) map[string]string {
	handles := map[callTurn][]string{}
	ids := map[callTurn][]string{}
	for _, rec := range records {
		switch r := rec.(type) {
		case *ToolCallRecord:
			key := callTurn{run: r.RunID, turn: r.Turn}
			handles[key] = append(handles[key], r.Handle)
		case *MessageRecord:
			if r.Role != RoleAssistant {
				continue
			}
			key := callTurn{run: r.RunID, turn: r.Turn}
			for _, block := range r.Content {
				if block.Kind == ContentToolCall {
					ids[key] = append(ids[key], block.ToolCallID)
				}
			}
		}
	}
	out := make(map[string]string)
	for key, calls := range handles {
		ordered := append([]string(nil), calls...)
		sort.SliceStable(ordered, func(i, j int) bool {
			return handleSeq(ordered[i]) < handleSeq(ordered[j])
		})
		for i, handle := range ordered {
			if i >= len(ids[key]) {
				break
			}
			out[handle] = ids[key][i]
		}
	}
	return out
}

// handleSeq returns the number a handle carries.
//
// The handles are numbered from one and never repeat, so the number
// names the call in the order the session made it. A handle that is not
// one of the session's has no number.
func handleSeq(handle string) int {
	return seqOf(handle, 'r')
}

// messageSeq returns the number a message id carries.
//
// The message ids are numbered from one and never repeat, so the number
// names the message in the order the session recorded it. A message id
// that is not one of the session's has no number.
func messageSeq(id string) int {
	return seqOf(id, 'm')
}

// seqOf returns the number a prefixed number carries.
//
// The prefix is the letter the ids start with, and the number is the
// rest. An id that is not a number has none.
func seqOf(id string, prefix byte) int {
	if len(id) < 2 || id[0] != prefix {
		return 0
	}
	return digits(id[1:])
}

// digits returns the number a string of digits carries.
//
// A string that holds anything but digits has no number.
func digits(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
		if n > 1<<40 {
			return 0
		}
	}
	return n
}
