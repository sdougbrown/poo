package poo_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
)

// attached records what one session's attach saw, and what its closer
// did.
type attached struct {
	mu sync.Mutex
	// calls are the session ids and working directories the attach was
	// called with, one entry per call.
	calls []string
	// closes counts how many times the attach's closer ran.
	closes int
	// err makes the attach itself fail, so the session is never built.
	err error
	// closeErr is the error the closer returns.
	closeErr error
	// options is the last options pointer the attach was handed.
	options *poo.Options
}

// attach is the attach function a test's options carry.
func (a *attached) attach(_ context.Context, at *poo.Attach) (func() error, error) {
	a.mu.Lock()
	a.calls = append(a.calls, at.SessionID+"@"+at.WorkingDir)
	a.options = at.Options
	err := a.err
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return a.close, nil
}

// close is the closer the attach hands back.
func (a *attached) close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closes++
	return a.closeErr
}

// attachments is what one attach recorded.
func (a *attached) attachments() (calls []string, closes int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...), a.closes
}

// TestSessionAttachRunsOnceWhenTheSessionIsBuilt covers the attach's
// place: it runs once, inside the construction, after the id and the
// working directory are final, and its closer runs when the session
// closes.
func TestSessionAttachRunsOnceWhenTheSessionIsBuilt(t *testing.T) {
	recorder := &attached{}
	opts := testOptions(t, scripted([]fakeResponse{{text: "ok"}}), &echoTool{}, &recordingSink{})
	opts.SessionAttach = recorder.attach
	s := newTestSession(t, opts)

	calls, _ := recorder.attachments()
	if len(calls) != 1 || calls[0] != "s1@"+opts.WorkingDir {
		t.Fatalf("the attach saw %q, want one call with the session's own id and directory", calls)
	}
	// The attach may change the options it was handed, so the handover
	// is the options the session is built from.
	if recorder.options == nil {
		t.Error("the attach was handed no options")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	if _, closes := recorder.attachments(); closes != 1 {
		t.Errorf("the attach's closer ran %d times, want once on the close", closes)
	}
	// The close is idempotent, and so is the closer: a second close
	// starts nothing again.
	if err := s.Close(); err != nil {
		t.Errorf("the second close returned %v, want none", err)
	}
	if _, closes := recorder.attachments(); closes != 1 {
		t.Errorf("the second close ran the closer again: %d runs", closes)
	}
}

// TestSessionAttachRunsOnARestoreWithARecordedPrompt covers the
// restore: a resumed session attaches what a new one does, and the
// recorded prompt skips nothing — the attach is not the prompt loader,
// so it runs even when the records hold the prompt the session keeps.
func TestSessionAttachRunsOnARestoreWithARecordedPrompt(t *testing.T) {
	// The original session runs once, so the records hold a prompt.
	original := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"hi"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, original, &echoTool{}, sink)
	first := &attached{}
	opts.SessionAttach = first.attach
	s := newTestSession(t, opts)
	runSession(t, s, "hello")
	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	if err := s.Close(); err != nil {
		t.Fatalf("closing the original: %v", err)
	}
	if calls, _ := first.attachments(); len(calls) != 1 {
		t.Fatalf("the original's attach ran %d times, want once", len(calls))
	}

	// The restored session is built from those records, and it attaches
	// once with the recorded id and directory.
	restored := &attached{}
	ropts := restoreOptions(t, scripted([]fakeResponse{{text: "ok"}}), &recordingSink{})
	// The working directory is the recorded one: the restore hands the
	// attach the session's own directory, not the build's.
	ropts.WorkingDir = ""
	ropts.SessionAttach = restored.attach
	session, _, err := poo.Restore(ropts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	defer session.Close()
	calls, _ := restored.attachments()
	if len(calls) != 1 {
		t.Fatalf("the restored session's attach ran %d times, want once", len(calls))
	}
	if want := "s1@/tmp"; calls[0] != want {
		t.Errorf("the restored session's attach saw %q, want %q", calls[0], want)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("closing the restored session: %v", err)
	}
	if _, closes := restored.attachments(); closes != 1 {
		t.Errorf("the restored session's closer ran %d times, want once", closes)
	}
}

// TestSessionAttachFailureStartsNothing covers the attach's failure:
// the construction returns it, and nothing the session would have
// written exists.
func TestSessionAttachFailureStartsNothing(t *testing.T) {
	sink := &recordingSink{}
	opts := testOptions(t, scripted([]fakeResponse{{text: "ok"}}), &echoTool{}, sink)
	recorder := &attached{err: errors.New("the attached subsystem failed")}
	opts.SessionAttach = recorder.attach

	_, err := poo.New(opts)
	if err == nil {
		t.Fatal("the session was built with a failing attach")
	}
	var init *poo.ErrSessionInit
	if !asSessionInit(err, &init) {
		t.Fatalf("the construction failure is %T, want an ErrSessionInit", err)
	}
	if !errors.Is(init.Cause, recorder.err) {
		t.Errorf("the construction failure is %v, want the attach's own error", init.Cause)
	}
	if !strings.Contains(err.Error(), "the attached subsystem failed") {
		t.Errorf("the failure says %q, want the attach's own words", err)
	}
	if len(sink.Records()) != 0 {
		t.Errorf("the failed attach left %d records, want none", len(sink.Records()))
	}
	if _, closes := recorder.attachments(); closes != 0 {
		t.Errorf("the failed attach's closer ran %d times, want none", closes)
	}
}

// TestSessionAttachCloserRunsWhenAConstructionFailsAfterIt covers the
// rollback: whatever the attach started is stopped on the way out of a
// construction that fails after it, and the failure is still the
// session's own.
func TestSessionAttachCloserRunsWhenAConstructionFailsAfterIt(t *testing.T) {
	recorder := &attached{}
	opts := testOptions(t, scripted([]fakeResponse{{text: "ok"}}), &echoTool{}, &recordingSink{})
	opts.SessionAttach = recorder.attach
	opts.SessionStart = []poo.SessionStartHook{&failingSessionStart{}}

	if _, err := poo.New(opts); err == nil {
		t.Fatal("the session was built with a failing session_start hook")
	}
	if _, closes := recorder.attachments(); closes != 1 {
		t.Errorf("the attach's closer ran %d times, want once on the failed construction", closes)
	}
}

// TestSessionAttachCloserErrorStillClosesTheSession covers the close:
// a closer that fails says so, and the session still closes — the
// fence, the writes and the flush all run, so the records the session
// wrote are on the store whatever its attached processes did.
func TestSessionAttachCloserErrorStillClosesTheSession(t *testing.T) {
	recorder := &attached{closeErr: errors.New("the closer failed")}
	sink := &recordingSink{}
	opts := testOptions(t, scripted([]fakeResponse{{text: "ok"}}), &echoTool{}, sink)
	opts.SessionAttach = recorder.attach
	s := newTestSession(t, opts)
	runSession(t, s, "hello")
	waitRecords(t, sink, "run.settled", 1)

	err := s.Close()
	if err == nil || !strings.Contains(err.Error(), "the closer failed") {
		t.Fatalf("the close returned %v, want the closer's own failure joined in", err)
	}
	if sink.Flushed() < 1 {
		t.Error("the close never flushed the store")
	}
	if _, closes := recorder.attachments(); closes != 1 {
		t.Errorf("the attach's closer ran %d times, want once", closes)
	}
}
