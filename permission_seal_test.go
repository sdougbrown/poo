package poo

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type permissionHandoffSink struct {
	mu         sync.Mutex
	records    []Record
	entered    chan struct{}
	release    chan struct{}
	storeFirst bool
}

func (s *permissionHandoffSink) Append(ctx context.Context, r Record) error {
	store := func() { s.mu.Lock(); s.records = append(s.records, r); s.mu.Unlock() }
	if r.Meta().Record != "permission.answered" {
		store()
		return nil
	}
	if s.storeFirst {
		store()
	}
	select {
	case <-s.entered:
	default:
		close(s.entered)
	}
	select {
	case <-s.release:
		if !s.storeFirst {
			store()
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (*permissionHandoffSink) Flush(context.Context) error { return nil }

func permissionHandoffSession(t *testing.T, storeFirst bool) (*Session, *runState, *permissionHandoffSink) {
	sink := &permissionHandoffSink{entered: make(chan struct{}), release: make(chan struct{}), storeFirst: storeFirst}
	ctx, cancel := context.WithCancelCause(context.Background())
	run := &runState{id: "run", ctx: ctx, cancel: cancel, pendingCalls: map[string]*issuedCall{}, workDone: make(chan struct{}, 1)}
	s := &Session{opts: Options{Sink: sink}, run: run, events: newEventBus(nil), recordWriteBudget: time.Second}
	t.Cleanup(s.events.stop)
	s.reportPermissionAsked(run, PermissionAsk{AskID: "a1", Tool: "bash", Reason: AskReasonPolicy})
	return s, run, sink
}

func checkPermissionHandoff(t *testing.T, s *Session, sink *permissionHandoffSink) {
	t.Helper()
	sink.mu.Lock()
	defer sink.mu.Unlock()
	answers, settled := 0, false
	for _, r := range sink.records {
		switch r.Meta().Record {
		case "permission.answered":
			if settled {
				t.Fatal("answer after settlement")
			}
			answers++
		case "run.settled":
			settled = true
		}
	}
	if answers != 1 || !settled {
		t.Fatalf("answers=%d settled=%v records=%v", answers, settled, sink.records)
	}
	events := 0
	for _, e := range s.Events() {
		if e.Name == EventPermissionAnswered {
			events++
		}
	}
	if events != 1 {
		t.Fatalf("answer events=%d", events)
	}
}

func TestPermissionSealJoinsUnstoredAnswer(t *testing.T) { testPermissionSealHandoff(t, false) }
func TestPermissionSealJoinsStoredAnswer(t *testing.T)   { testPermissionSealHandoff(t, true) }
func testPermissionSealHandoff(t *testing.T, storeFirst bool) {
	s, run, sink := permissionHandoffSession(t, storeFirst)
	done := make(chan struct{})
	go func() {
		s.reportPermissionAnswered(run, PermissionAnswer{AskID: "a1", Decision: "allow", AnsweredBy: "policy", Provenance: ProvenanceAnswered})
		close(done)
	}()
	<-sink.entered
	s.mu.Lock()
	s.sealLocked(run)
	s.mu.Unlock()
	close(sink.release)
	// Cleanup may acquire the writer before its original caller resumes.
	s.settlePendingAsks(run)
	<-done
	_ = s.write(context.Background(), &RunSettled{RecordMeta: NewRecordMeta("run.settled"), RunID: run.id})
	checkPermissionHandoff(t, s, sink)
}

func TestPermissionSealedReporterCannotResetNextRun(t *testing.T) {
	oldRun := &runState{id: "old", sealed: true, committed: true, repeats: newRepeatCounter()}
	newRun := &runState{id: "new", repeats: newRepeatCounter(), permissionHandle: "r2", permissionTool: "read"}
	s := &Session{run: newRun}
	newRun.repeats.count("read", "read:a", "read:a")
	newRun.repeats.count("read", "read:a", "read:a")
	reporter := &permissionReporter{session: s, run: oldRun}
	reporter.ResetRepeats("r1")
	got := newRun.repeats.count("read", "read:a", "read:a")
	if got.exact != 3 {
		t.Fatalf("old reporter changed new run counters: %+v", got)
	}
}

type mixedKeyPrepared struct {
	Kind    string `json:"kind"`
	command string
}

func (p mixedKeyPrepared) Summary() string { return p.command }
func (p mixedKeyPrepared) CanonicalKey() json.RawMessage {
	key, _ := json.Marshal(map[string]string{"command": p.command})
	return key
}
func TestCanonicalKeyOverridesPartialEncoding(t *testing.T) {
	a := mixedKeyPrepared{Kind: "shell", command: "echo a"}
	b := mixedKeyPrepared{Kind: "shell", command: "echo b"}
	ka := exactKeyOf("bash", a, []byte(`{"command":"echo a"}`))
	kb := exactKeyOf("bash", b, []byte(`{"command":"echo b"}`))
	if ka == kb {
		t.Fatalf("distinct prepared commands collapsed: %q", ka)
	}
}
