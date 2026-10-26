package poo

import (
	"testing"
	"time"
)

func TestPermissionFinalizerJoinsUnstoredAnswer(t *testing.T) {
	testPermissionFinalizerHandoff(t, false)
}
func TestPermissionFinalizerJoinsStoredAnswer(t *testing.T) { testPermissionFinalizerHandoff(t, true) }
func testPermissionFinalizerHandoff(t *testing.T, stored bool) {
	s, run, sink := permissionHandoffSession(t, stored)
	s.recordWriteBudget = 50 * time.Millisecond
	s.state = StateExecuting
	_, releaseGuard, ok := s.beginRunWrite(run)
	if !ok {
		t.Fatal("guard refused")
	}
	answerDone := make(chan struct{})
	s.SetSealRaceHooks(&SealRaceHooks{AfterSeal: func() {
		close(sink.release)
		<-answerDone
		releaseGuard()
	}})
	finalized := make(chan struct{})
	go func() {
		s.finalize(run, newCandidate(ReasonCancelled, SettleDetail{Source: "cancel:test"}))
		close(finalized)
	}()
	<-run.ctx.Done()
	// The answer append has a later cleanup deadline than sealRun's
	// bounded wait. The seal must join it, not cancel or replace it.
	time.Sleep(10 * time.Millisecond)
	go func() {
		s.reportPermissionAnswered(run, PermissionAnswer{AskID: "a1", Decision: "deny", AnsweredBy: "cancelled", Provenance: ProvenanceCancelled})
		close(answerDone)
	}()
	select {
	case <-finalized:
	case <-time.After(3 * time.Second):
		t.Fatal("finalizer did not return")
	}
	checkPermissionHandoff(t, s, sink)
}
