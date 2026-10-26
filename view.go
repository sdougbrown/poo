package poo

// HistoryView is a concrete read-only view of a model's history.
//
// The view hands out copies: a caller that mutates what it got back
// changes nothing, and neither the model's history nor another caller's
// copy changes. Nested content and tool payloads are deep-copied on
// access.
type HistoryView struct {
	// messages is the model's history, oldest first.
	messages []Message
	// turns summarises each turn.
	turns []TurnSummary
	// results holds the tool results, by handle.
	results map[string]ToolResult
}

// NewHistoryView returns a view over a set of messages.
//
// The messages, the tool results and the turn summaries are copied, so the
// caller cannot change the view by changing what it passed in.
func NewHistoryView(messages []Message, results map[string]ToolResult, turns []TurnSummary) *HistoryView {
	h := &HistoryView{
		messages: CloneMessages(messages),
		results:  map[string]ToolResult{},
	}
	for handle, r := range results {
		h.results[handle] = copyResult(r)
	}
	h.turns = make([]TurnSummary, len(turns))
	for i, t := range turns {
		h.turns[i] = t
		h.turns[i].Calls = append([]ToolCallSummary(nil), t.Calls...)
	}
	return h
}

// Messages returns the model's history, in model order.
func (h *HistoryView) Messages() []Message {
	return CloneMessages(h.messages)
}

// Turns returns a summary of each turn.
func (h *HistoryView) Turns() []TurnSummary {
	out := make([]TurnSummary, len(h.turns))
	for i, t := range h.turns {
		out[i] = t
		out[i].Calls = append([]ToolCallSummary(nil), t.Calls...)
	}
	return out
}

// Result returns the tool result with the given handle.
func (h *HistoryView) Result(handle string) (ToolResult, bool) {
	r, ok := h.results[handle]
	if !ok {
		return ToolResult{}, false
	}
	return copyResult(r), true
}

// CloneMessages copies a list of messages, and everything under them.
//
// The copy is deep: the content blocks, the raw arguments, the opaque
// provider payloads and the delivery note are all copied, so a caller
// that changes one cannot reach the original.
func CloneMessages(in []Message) []Message {
	if in == nil {
		return nil
	}
	out := make([]Message, len(in))
	for i, m := range in {
		out[i] = m
		if m.Content != nil {
			out[i].Content = make([]ContentBlock, len(m.Content))
			for j, c := range m.Content {
				out[i].Content[j] = c
				if c.Arguments != nil {
					out[i].Content[j].Arguments = append([]byte(nil), c.Arguments...)
				}
				if c.Provider != nil {
					p := *c.Provider
					if p.Data != nil {
						p.Data = append([]byte(nil), p.Data...)
					}
					out[i].Content[j].Provider = &p
				}
			}
		}
		if m.DeliveryNote != nil {
			note := *m.DeliveryNote
			out[i].DeliveryNote = &note
		}
	}
	return out
}

// copyResult copies a tool result, and its structured payload.
func copyResult(r ToolResult) ToolResult {
	out := r
	if r.Structured != nil {
		out.Structured = append([]byte(nil), r.Structured...)
	}
	return out
}
