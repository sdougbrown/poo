package pootest

import (
	"context"
	"fmt"
	"sync"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
)

// Decider is a stub decision model.
//
// It answers every question with the same probability for its first
// option, so a test can drive a decision without a model.
//
// A question with more than one option gets a distribution that sums to
// one: the first option takes the configured probability and the rest
// share what is left.
type Decider struct {
	mu sync.Mutex
	// probability is the probability of the first option.
	probability float64
	// choice is the option name the decider chose, when it names one.
	choice string
	// err is the failure the decider returns, when it is set.
	err error
	// calls counts the calls the decider served.
	calls int
	// questions holds the questions of every call, in order.
	questions [][]decide.Question
	// states holds the state of every call, in order.
	states []any
}

// NewDecider returns a decider that answers with the given probability
// for the first option.
func NewDecider(probability float64) *Decider {
	return &Decider{probability: probability}
}

// SetChoice makes the decider name an option as its choice.
func (d *Decider) SetChoice(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.choice = name
}

// SetError makes the decider fail.
func (d *Decider) SetError(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err
}

// Calls returns how many calls the decider served.
func (d *Decider) Calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// Questions returns the questions of every call, in order.
func (d *Decider) Questions() [][]decide.Question {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([][]decide.Question(nil), d.questions...)
}

// States returns the state of every call, in order.
func (d *Decider) States() []any {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]any(nil), d.states...)
}

// Decide answers the questions.
func (d *Decider) Decide(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.calls++
	d.questions = append(d.questions, qs)
	d.states = append(d.states, state)
	if d.err != nil {
		return nil, d.err
	}

	out := make([]decide.Answer, 0, len(qs))
	for _, q := range qs {
		keys := q.Keys()
		if len(keys) == 0 {
			continue
		}
		probs := map[string]float64{}
		first := d.probability
		if first < 0 {
			first = 0
		}
		if first > 1 {
			first = 1
		}
		rest := 1 - first
		if len(keys) == 1 {
			// One option is certain: a probability of anything else does
			// not sum to one.
			first = 1
		}
		for i, k := range keys {
			if i == 0 {
				probs[k] = first
				continue
			}
			probs[k] = rest / float64(len(keys)-1)
		}
		answer := decide.Answer{ID: q.ID, Probabilities: probs}
		if d.choice != "" {
			answer.Choice = d.choice
		}
		out = append(out, answer)
	}
	return out, nil
}

// RecordingSink is a decide sink that keeps the records it received.
type RecordingSink struct {
	mu sync.Mutex
	// records holds the records, in order.
	records []decide.Record
}

// NewRecordingSink returns an empty sink.
func NewRecordingSink() *RecordingSink { return &RecordingSink{} }

// Record keeps one record.
func (s *RecordingSink) Record(ctx context.Context, rec decide.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	return nil
}

// Records returns the records the sink kept, in order.
func (s *RecordingSink) Records() []decide.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]decide.Record(nil), s.records...)
}

// Compactor is a scripted compactor.
//
// It answers with a fixed decision for every view, and it records the
// calls it served.
type Compactor struct {
	mu sync.Mutex
	// should is what ShouldCompact answers.
	should bool
	// compaction is what Compact answers.
	compaction poo.Compaction
	// err is the failure Compact returns, when it is set.
	err error
	// calls counts the calls the decider served.
	calls int
	// views holds the views of every call, in order.
	views []poo.View
	// forks holds the fork arguments of every call, in order.
	forks []ForkCall
}

// ForkCall is one fork a compactor asked for.
type ForkCall struct {
	// Appended are the messages the compactor appended.
	Appended []poo.Message
	// MaxTokens is the output ceiling the compactor asked for.
	MaxTokens int
}

// NewCompactor returns a compactor that answers with the given decision.
func NewCompactor(should bool, compaction poo.Compaction) *Compactor {
	return &Compactor{should: should, compaction: compaction}
}

// SetError makes the compactor fail.
func (c *Compactor) SetError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
}

// Calls returns how many times the compactor compacted.
func (c *Compactor) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// Views returns the views of every call, in order.
func (c *Compactor) Views() []poo.View {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]poo.View(nil), c.views...)
}

// Forks returns the forks of every call, in order.
func (c *Compactor) Forks() []ForkCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ForkCall(nil), c.forks...)
}

// ShouldCompact answers the scripted decision.
func (c *Compactor) ShouldCompact(view poo.View, budget poo.Budget) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.should
}

// Compact answers the scripted compaction.
func (c *Compactor) Compact(ctx context.Context, view poo.View, fork poo.ForkFunc) (poo.Compaction, error) {
	c.mu.Lock()
	c.calls++
	c.views = append(c.views, view)
	err := c.err
	compaction := c.compaction
	c.mu.Unlock()

	if err != nil {
		return poo.Compaction{}, err
	}
	return compaction, nil
}

// Fork calls the fork function and records the call.
//
// It is a helper for a compactor that wants to reach the model.
func (c *Compactor) Fork(ctx context.Context, fork poo.ForkFunc, appended []poo.Message, maxTokens int) (poo.Message, poo.UsageReport, error) {
	c.mu.Lock()
	c.forks = append(c.forks, ForkCall{Appended: appended, MaxTokens: maxTokens})
	c.mu.Unlock()
	if fork == nil {
		return poo.Message{}, poo.UsageReport{}, fmt.Errorf("pootest: there is no fork to call")
	}
	return fork(ctx, appended, maxTokens)
}
