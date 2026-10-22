package poo

import (
	"sync"

	"github.com/sdougbrown/poo/prompt"
)

// The recorded prompts.
//
// A session that is replayed or restored takes the prompt the records
// hold: the prompt is never rendered from the sections again, so a
// change in the sections cannot reach a session that was recorded under
// another one.

// RecordedPrompts installs a session's recorded prompts, in order.
//
// The first run takes the first recorded prompt, the second run takes
// the second, and a run past the last one takes the last. A recording
// with no prompt changes nothing.
type RecordedPrompts struct {
	// mu guards the index.
	mu sync.Mutex
	// prompts are the recorded prompts, in run order.
	prompts []string
	// index is the prompt the next run takes.
	index int
}

// NewRecordedPrompts returns the hook that installs the prompts.
func NewRecordedPrompts(prompts []string) *RecordedPrompts {
	return &RecordedPrompts{prompts: prompts}
}

// BeforeRun installs the run's recorded prompt.
//
// The prompt replaces the whole one: the recorded text is the prompt,
// and the sections are not rendered again.
func (h *RecordedPrompts) BeforeRun(ctx HookContext, trigger RunTrigger) (StartDecision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.prompts) == 0 {
		return StartDecision{}, nil
	}
	text := h.prompts[len(h.prompts)-1]
	if h.index < len(h.prompts) {
		text = h.prompts[h.index]
		h.index++
	}
	return StartDecision{SystemPrompt: func(s prompt.Sections) {
		s.Replace(text)
	}}, nil
}
