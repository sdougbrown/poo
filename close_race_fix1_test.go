package poo_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// fix1OrderedSink records the order of the writes and the flush, so a
// test can see a record written after the close's flush.
type fix1OrderedSink struct {
	mu    sync.Mutex
	order []string
}

func (s *fix1OrderedSink) Append(ctx context.Context, rec poo.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, "append:"+rec.Meta().Record)
	return nil
}

func (s *fix1OrderedSink) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, "flush")
	return nil
}

func (s *fix1OrderedSink) afterFlush() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := false
	var out []string
	for _, entry := range s.order {
		if entry == "flush" {
			seen = true
			continue
		}
		if seen {
			out = append(out, entry)
		}
	}
	return out
}

// fix1StalledTransport waits until its context ends and answers
// nothing.
type fix1StalledTransport struct{}

func (fix1StalledTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	<-ctx.Done()
	return nil, poo.Rendered{}, ctx.Err()
}

func (fix1StalledTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// newFix1Session builds a session whose store records the order of the
// writes.
func newFix1Session(t *testing.T, sink poo.RecordSink) *poo.Session {
	t.Helper()
	sess, err := poo.New(poo.Options{
		Transport:    fix1StalledTransport{},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	return sess
}

// TestACloseThatBeatsAPromptReservationWritesNothing covers the
// admission boundary: a prompt whose reservation the close found is
// refused before it installs a run, so nothing appends after the
// close's flush.
func TestACloseThatBeatsAPromptReservationWritesNothing(t *testing.T) {
	sink := &fix1OrderedSink{}
	sess := newFix1Session(t, sink)
	entered := make(chan struct{})
	release := make(chan struct{})
	restore := sess.SetJobRaceHooks(&poo.JobRaceHooks{
		BeforeRunStart: func() {
			close(entered)
			<-release
		},
	})
	defer restore()

	prompted := make(chan error, 1)
	go func() {
		_, err := sess.Prompt(context.Background(), "hi")
		prompted <- err
	}()

	<-entered
	if err := sess.Close(); err != nil {
		t.Fatalf("the close failed: %v", err)
	}
	close(release)

	if err := <-prompted; !errors.Is(err, poo.ErrSessionClosed) {
		t.Fatalf("the prompt failed with %v, want ErrSessionClosed", err)
	}
	if late := sink.afterFlush(); len(late) > 0 {
		t.Fatalf("the session wrote %v after the close's flush", late)
	}
}

// TestAConcurrentCloseAndPromptWriteNothingAfterTheFlush proves the
// close ordering's fence: a prompt that races the close either loses
// the race or is finalized by it, so nothing appends after the flush.
func TestAConcurrentCloseAndPromptWriteNothingAfterTheFlush(t *testing.T) {
	for i := 0; i < 1000; i++ {
		sink := &fix1OrderedSink{}
		sess := newFix1Session(t, sink)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = sess.Prompt(context.Background(), "hi")
		}()
		go func() {
			defer wg.Done()
			_ = sess.Close()
		}()
		wg.Wait()
		if late := sink.afterFlush(); len(late) > 0 {
			t.Fatalf("iteration %d: the session wrote %v after the close's flush", i, late)
		}
		if err := sess.Close(); err != nil {
			t.Fatalf("iteration %d: the second close failed: %v", i, err)
		}
	}
}

// TestCloseRacingAJobCompletionEndsItOnce covers the plan's close with
// jobs: a close that races a job's completion leaves exactly one
// job.ended record per job.
func TestCloseRacingAJobCompletionEndsItOnce(t *testing.T) {
	for i := 0; i < 50; i++ {
		tr := scripted([]fakeResponse{
			{calls: []poo.ToolCall{jobCall("one", true)}},
			{text: "the answer"},
		})
		sink := &recordingSink{}
		tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
		tool.SetResult("the job's output")
		sess, err := poo.New(jobOptions(t, tr, tool, sink))
		if err != nil {
			t.Fatalf("iteration %d: building the session: %v", i, err)
		}
		if _, err := sess.Prompt(context.Background(), "start the job"); err != nil {
			t.Fatalf("iteration %d: starting the run: %v", i, err)
		}
		<-tool.Started()
		waitFor(t, "the run to wait", func() bool {
			return sess.State() == poo.StateWaiting
		})

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			tool.ReleaseAll()
		}()
		go func() {
			defer wg.Done()
			_ = sess.Close()
		}()
		wg.Wait()

		if got := sink.count("job.ended"); got != 1 {
			t.Fatalf("iteration %d: the log holds %d job.ended record(s), want 1; records: %v", i, got, sink.names())
		}
	}
}
