package poo

// The calls the records hold.
//
// A tool call's provider id lives in the assistant message that carries
// it, and the call's record carries the same id: the message is the
// model's view, and the view is what the provider matches a result
// against. A call that the records pair with no message was never in the
// model's view, and nothing refers to it.

// CallIDs returns the provider's ids of the calls the records hold, by
// the calls' handles.
//
// A call's record carries the id the model's message gave it, so the
// pairing is by id and never by the order the records landed in: a batch
// of read-only calls runs at the same time, and its records land in any
// order. A record written before the field existed falls back to the
// handle's place in the model's order, which the message blocks give. A
// call the model never saw has no id, and nothing refers to it.
func CallIDs(records []Record) map[string]string {
	out := make(map[string]string)
	for _, rec := range records {
		call, ok := rec.(*ToolCallRecord)
		if ok && call.CallID != "" {
			out[call.Handle] = call.CallID
		}
	}
	// A record written before the field existed carries no id. Its
	// handle is the call's place in the model's order, so the id is the
	// message block at that place.
	type turnKey struct {
		run  string
		turn int
	}
	blocks := map[turnKey][]string{}
	seen := map[turnKey]bool{}
	var order []turnKey
	for _, rec := range records {
		msg, ok := rec.(*MessageRecord)
		if !ok || msg.Role != RoleAssistant {
			continue
		}
		key := turnKey{run: msg.RunID, turn: msg.Turn}
		for _, block := range msg.Content {
			if block.Kind != ContentToolCall || block.ToolCallID == "" {
				continue
			}
			blocks[key] = append(blocks[key], block.ToolCallID)
			if !seen[key] {
				seen[key] = true
				order = append(order, key)
			}
		}
	}
	first := map[turnKey]int{}
	ordinal := 1
	for _, key := range order {
		first[key] = ordinal
		ordinal += len(blocks[key])
	}
	for _, rec := range records {
		call, ok := rec.(*ToolCallRecord)
		if !ok || call.CallID != "" {
			continue
		}
		key := turnKey{run: call.RunID, turn: call.Turn}
		ids := blocks[key]
		index := handleSeq(call.Handle) - first[key]
		if index >= 0 && index < len(ids) {
			out[call.Handle] = ids[index]
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
