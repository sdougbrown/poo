package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The close's write fence: every record write registers before it calls
// the sink, and the close waits for the registrations before it flushes.
//
// These tests make one record's sink call slow, and the sink ignores the
// write's context, which is what the real log does once its write has
// started. The write outlives the close's own bounds, so only the fence
// can keep it from landing after the close's flush.

// The two bounds these tests run at: the record-write budget, and how
// long a held write sleeps.
//
// The held write sleeps half again as long as the budget, so it is past
// the close's own bounds but still inside the fence's wait.
const (
	fenceWriteBudget = 120 * time.Millisecond
	fenceHold        = 180 * time.Millisecond
)

// fenceSlowSink records the order of the sink calls, and holds the first
// write of one record name past the close's bounds.
type fenceSlowSink struct {
	start time.Time
	hold  string
	delay time.Duration
	// slow is how long every other write sleeps. It keeps a close that
	// runs at a random point meeting writes that are in flight.
	slow time.Duration

	mu    sync.Mutex
	order []string
	// held is closed when the held write reaches the sink.
	held chan struct{}
	once sync.Once
}

// newFenceSlowSink builds a sink that holds the first write of a record.
func newFenceSlowSink(hold string, holdDelay time.Duration) *fenceSlowSink {
	return &fenceSlowSink{
		start: time.Now(),
		hold:  hold,
		delay: holdDelay,
		held:  make(chan struct{}),
	}
}

// Append records one record, holding the first held write.
func (s *fenceSlowSink) Append(ctx context.Context, rec poo.Record) error {
	name := rec.Meta().Record
	s.mu.Lock()
	hold := name == s.hold
	s.mu.Unlock()
	if hold {
		s.once.Do(func() {
			close(s.held)
			// The real log's write is not cancellable once it started.
			time.Sleep(s.delay)
		})
	} else if s.slow > 0 {
		time.Sleep(s.slow)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, fmt.Sprintf("append:%s@%.2f", name, time.Since(s.start).Seconds()))
	return nil
}

// Flush records the flush.
func (s *fenceSlowSink) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, fmt.Sprintf("flush@%.2f", time.Since(s.start).Seconds()))
	return nil
}

// afterTheLastFlush returns the records that landed after the close's
// own flush.
func (s *fenceSlowSink) afterTheLastFlush() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	last := -1
	for i, entry := range s.order {
		if len(entry) >= 5 && entry[:5] == "flush" {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	return append([]string(nil), s.order[last+1:]...)
}

// count returns how many records carry a name.
func (s *fenceSlowSink) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, entry := range s.order {
		if entry == "append:"+name || len(entry) > len("append:"+name) && entry[:len("append:"+name)+1] == "append:"+name+"@" {
			n++
		}
	}
	return n
}

// order returns the recorded order.
func (s *fenceSlowSink) orderCopy() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.order...)
}

// waitForLanded waits until the held record's append was recorded.
func (s *fenceSlowSink) waitForLanded(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if s.count(s.hold) > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the %s write never landed; order: %v", s.hold, s.orderCopy())
}

// fenceHoldOptions builds options whose idle results are held.
func fenceHoldOptions(t *testing.T, tr poo.ChatTransport, tool poo.Tool, sink poo.RecordSink) poo.Options {
	t.Helper()
	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	return opts
}

// TestAJobEndedRecordDoesNotFollowTheClose covers a job that ends while
// the close runs: the close sees the job ended and writes nothing for it,
// so only the fence keeps the job's own job.ended from landing after the
// flush.
func TestAJobEndedRecordDoesNotFollowTheClose(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")
	sink := newFenceSlowSink("job.ended", fenceHold)
	sess := newTestSession(t, fenceHoldOptions(t, tr, tool, sink))
	poo.SetRecordWriteBudget(sess, fenceWriteBudget)

	if _, err := sess.Prompt(context.Background(), "start the job"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the run to wait", func() bool { return sess.State() == poo.StateWaiting })

	// The job completes, and its job.ended write is in flight.
	go tool.ReleaseAll()
	select {
	case <-sink.held:
	case <-time.After(10 * time.Second):
		t.Fatal("the job.ended write never reached the sink")
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("the close failed: %v", err)
	}
	sink.waitForLanded(t)
	if late := sink.afterTheLastFlush(); len(late) > 0 {
		t.Fatalf("the session wrote %v after the close's flush; order: %v", late, sink.orderCopy())
	}
	if got := sink.count("job.ended"); got != 1 {
		t.Fatalf("the log holds %d job.ended record(s), want 1; order: %v", got, sink.orderCopy())
	}
}

// TestAQueuedInputRecordDoesNotFollowTheClose covers a message whose
// acceptance the seal took while its record was being written: the close
// flushes, and the acceptance lands afterwards unless the fence holds it.
func TestAQueuedInputRecordDoesNotFollowTheClose(t *testing.T) {
	sink := newFenceSlowSink("input.queued", fenceHold)
	sess, err := poo.New(poo.Options{
		Transport:    fix1StalledTransport{},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir:   t.TempDir(),
		SystemPrompt: "the system prompt",
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	poo.SetRecordWriteBudget(sess, fenceWriteBudget)

	if _, err := sess.Prompt(context.Background(), "hi"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	waitFor(t, "the run to request", func() bool { return sess.State() == poo.StateRequesting })

	sent := make(chan error, 1)
	go func() {
		_, err := sess.Send("a message", poo.Steer)
		sent <- err
	}()
	select {
	case <-sink.held:
	case <-time.After(10 * time.Second):
		t.Fatal("the message's write never reached the sink")
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("the close failed: %v", err)
	}
	sink.waitForLanded(t)
	if late := sink.afterTheLastFlush(); len(late) > 0 {
		t.Fatalf("the session wrote %v after the close's flush; order: %v", late, sink.orderCopy())
	}
	// The input's fate is recorded once: the seal reported it dropped,
	// and the message's own late report is refused by the fence.
	if got := sink.count("input.dropped"); got != 1 {
		t.Fatalf("the log holds %d input.dropped record(s), want 1; order: %v", got, sink.orderCopy())
	}
	if err := <-sent; err != nil {
		t.Fatalf("the send failed with %v", err)
	}
}

// TestAHeldRecordDoesNotFollowTheClose covers the idle path's held
// record: the wake holds the job's result and its input.held write is in
// flight while the close runs.
func TestAHeldRecordDoesNotFollowTheClose(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")
	sink := newFenceSlowSink("input.held", fenceHold)
	sess := newTestSession(t, fenceHoldOptions(t, tr, tool, sink))
	poo.SetRecordWriteBudget(sess, fenceWriteBudget)

	if _, err := sess.Prompt(context.Background(), "start the job"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the run to wait", func() bool { return sess.State() == poo.StateWaiting })

	// The run goes away, so the job's result finds an idle session.
	sess.Cancel("the test")
	waitFor(t, "the session to go idle", func() bool {
		return sess.State() == poo.StateSettled && !sess.Running()
	})

	// The job completes: the idle path holds the result, and its
	// input.held write is in flight while the close runs.
	go tool.ReleaseAll()
	select {
	case <-sink.held:
	case <-time.After(10 * time.Second):
		t.Fatal("the held write never reached the sink")
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("the close failed: %v", err)
	}
	sink.waitForLanded(t)
	if late := sink.afterTheLastFlush(); len(late) > 0 {
		t.Fatalf("the session wrote %v after the close's flush; order: %v", late, sink.orderCopy())
	}
}

// fenceReadTool is a read-only tool whose call the policy asks about.
type fenceReadTool struct{}

// Spec describes the tool.
func (*fenceReadTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "read",
		Description: "reads a file",
		Schema:      json.RawMessage(`{"type": "object"}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts the call.
func (*fenceReadTool) Prepare(json.RawMessage, poo.PrepareEnv) (poo.Prepared, error) {
	return fenceReadPrepared{}, nil
}

// Call answers at once.
func (*fenceReadTool) Call(context.Context, poo.Prepared, poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "the file's text"}, nil
}

// fenceReadPrepared is the prepared read call.
type fenceReadPrepared struct{}

// Summary describes the call.
func (fenceReadPrepared) Summary() string { return "reads a file" }

// fenceAskPolicy publishes an ask for the read call and holds it until
// the run ends or the test releases it.
type fenceAskPolicy struct {
	release chan struct{}
	asked   chan struct{}
	once    sync.Once
}

// BeforeTool asks about the read call and allows the rest.
func (p *fenceAskPolicy) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	if info.Spec.Name != "read" {
		return poo.Allow(), nil
	}
	ask := poo.PermissionAsk{
		AskID:   "ask-1",
		Tool:    info.Spec.Name,
		Summary: "reads a file",
		Class:   info.Spec.Class,
		Reason:  poo.ReasonPolicy,
	}
	ctx.Permissions.Asked(ask)
	p.once.Do(func() { close(p.asked) })
	select {
	case <-p.release:
	case <-ctx.Done():
	}
	ctx.Permissions.Answered(poo.PermissionAnswer{
		AskID:      ask.AskID,
		Decision:   "allow",
		AnsweredBy: "policy",
		Provenance: "answered",
	})
	return poo.Allow(), nil
}

// TestNoRecordFollowsTheCloseOverRandomClosePoints is the property: a
// session with a run, a job finishing, a message in flight, an ask
// pending and a configure is closed at a random point, and no record
// appends after the close's flush, and each job has exactly one
// job.ended.
//
// A close that reports the fence's bound is the one case the property
// does not hold: the close names the write that held it up and carries
// on.
func TestNoRecordFollowsTheCloseOverRandomClosePoints(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for i := 0; i < 30; i++ {
		tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
		tool.SetResult("the job's output")
		sink := newFenceSlowSink("input.queued", 2*time.Millisecond)
		sink.slow = 2 * time.Millisecond
		policy := &fenceAskPolicy{release: make(chan struct{}), asked: make(chan struct{})}
		tr := scripted([]fakeResponse{
			{calls: []poo.ToolCall{jobCall("one", true), {
				ID: "read-1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`),
			}}},
			{text: "the answer"},
		})
		opts := jobOptions(t, tr, tool, sink)
		opts.Tools["read"] = &fenceReadTool{}
		opts.ToolOrder = []string{"job", "read"}
		opts.Policy = policy
		sess := newTestSession(t, opts)
		poo.SetRecordWriteBudget(sess, 10*time.Millisecond)

		if _, err := sess.Prompt(context.Background(), "start the work"); err != nil {
			t.Fatalf("iteration %d: starting the run: %v", i, err)
		}
		select {
		case <-tool.Started():
		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: the job never started", i)
		}
		select {
		case <-policy.asked:
		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: the ask was never published", i)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			tool.ReleaseAll()
		}()
		go func() {
			defer wg.Done()
			_, _ = sess.Send("a message", poo.Steer)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = sess.SetModel(poo.ModelInfo{ModelID: "other-model", ContextLimit: 32768})
		}()

		time.Sleep(time.Duration(rng.Intn(15)) * time.Millisecond)
		err := sess.Close()
		wg.Wait()

		if err != nil && !errors.Is(err, poo.ErrCloseStalled) {
			t.Fatalf("iteration %d: the close failed: %v", i, err)
		}
		if err == nil {
			if late := sink.afterTheLastFlush(); len(late) > 0 {
				t.Fatalf("iteration %d: the session wrote %v after the close's flush; order: %v", i, late, sink.orderCopy())
			}
			if got := sink.count("job.ended"); got != 1 {
				t.Fatalf("iteration %d: the log holds %d job.ended record(s), want 1; order: %v", i, got, sink.orderCopy())
			}
		}
	}
}
