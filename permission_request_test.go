package poo

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPermissionRequestRejectsStaleAttempts(t *testing.T) {
	now := time.Unix(0, 0)
	p := NewPermissionRequest(PermissionAsk{AskID: "a", Reason: ReasonPolicy}, PermissionRequestOptions{Now: func() time.Time { return now }, Deadline: now.Add(time.Hour), Fallback: "deny"})
	drops := 0
	p.logDrop = func(PermissionInput) { drops++ }
	firstDeadline := now.Add(time.Minute)
	first := p.Send(PermissionInput{Kind: PermissionAttempt, Deadline: firstDeadline})
	now = firstDeadline
	p.Send(PermissionInput{Kind: PermissionExplicitAnswer, Attempt: first, Deadline: firstDeadline, Answer: PermissionAnswer{Decision: "allow"}})
	if p.Result().Decision != "" {
		t.Fatal("expired answer resolved the ask")
	}
	secondDeadline := now.Add(time.Minute)
	second := p.Send(PermissionInput{Kind: PermissionAttempt, Deadline: secondDeadline})
	p.Send(PermissionInput{Kind: PermissionBudget, Attempt: first, Deadline: now.Add(-time.Minute)})
	p.Send(PermissionInput{Kind: PermissionAttemptExpired, Attempt: first})
	p.Send(PermissionInput{Kind: PermissionExplicitAnswer, Attempt: first, Deadline: firstDeadline, Answer: PermissionAnswer{Decision: "deny"}})
	p.Send(PermissionInput{Kind: PermissionExplicitAnswer, Attempt: second, Deadline: secondDeadline, Answer: PermissionAnswer{Decision: "allow", AnsweredBy: "second"}})
	p.Send(PermissionInput{Kind: PermissionSeal})
	if got := p.Result(); got.Decision != "allow" || got.AnsweredBy != "second" || got.AskID != "a" {
		t.Fatalf("answer=%+v", got)
	}
	if drops != 5 {
		t.Fatalf("dropped inputs=%d", drops)
	}
}

func TestPermissionRequestCancellationWinsReadyAnswer(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		ctx, cancel := context.WithCancelCause(context.Background())
		deadline := time.Now().Add(time.Hour)
		p := NewPermissionRequest(PermissionAsk{}, PermissionRequestOptions{Context: ctx, Deadline: deadline, Fallback: "allow"})
		token := p.Send(PermissionInput{Kind: PermissionAttempt, Deadline: deadline})
		cause := context.Canceled
		if interrupted {
			cause = errInterrupted
		}
		cancel(cause)
		p.Send(PermissionInput{Kind: PermissionExplicitAnswer, Attempt: token, Deadline: deadline, Answer: PermissionAnswer{Decision: "allow"}})
		want := ProvenanceCancelled
		if interrupted {
			want = ProvenanceInterrupted
		}
		if got := p.Result(); got.Decision != "deny" || got.Provenance != want {
			t.Fatalf("answer=%+v", got)
		}
	}
}

func TestPermissionRequestConcurrentResolution(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	p := NewPermissionRequest(PermissionAsk{AskID: "one"}, PermissionRequestOptions{Deadline: deadline})
	var drops atomic.Int32
	p.logDrop = func(PermissionInput) { drops.Add(1) }
	token := p.Send(PermissionInput{Kind: PermissionAttempt, Deadline: deadline})
	inputs := []PermissionInput{
		{Kind: PermissionExplicitAnswer, Attempt: token, Deadline: deadline, Answer: PermissionAnswer{Decision: "allow", AnsweredBy: "client"}},
		{Kind: PermissionCancel}, {Kind: PermissionInterrupt}, {Kind: PermissionSeal},
		{Kind: PermissionDeferred, Attempt: token}, {Kind: PermissionAttemptExpired, Attempt: token},
		{Kind: PermissionChainExpired}, {Kind: PermissionExhausted},
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, input := range inputs {
		wg.Add(1)
		go func(input PermissionInput) { defer wg.Done(); <-start; p.Send(input) }(input)
	}
	close(start)
	wg.Wait()
	select {
	case <-p.Done():
	default:
		t.Fatal("request unresolved")
	}
	got := p.Result()
	if got.Decision != "allow" && got.Decision != "deny" {
		t.Fatalf("answer=%+v", got)
	}
	if got.Decision == "allow" && got.AnsweredBy != "client" {
		t.Fatalf("non-explicit allow: %+v", got)
	}
	p.Send(PermissionInput{Kind: PermissionSeal})
	if p.Result() != got || drops.Load() == 0 {
		t.Fatal("terminal answer was not stable")
	}
}

func TestPermissionSealResolvesPendingBeforeSettlement(t *testing.T) {
	s, run, sink := permissionHandoffSession(t, false)
	close(sink.release)
	s.recordWriteBudget = time.Millisecond
	s.finalize(run, newCandidate(ReasonCancelled, SettleDetail{Source: "test"}))
	checkPermissionHandoff(t, s, sink)
	for _, record := range sink.records {
		if answer, ok := record.(*PermissionAnsweredRecord); ok {
			if answer.Decision != "deny" || answer.Provenance != ProvenanceCancelled {
				t.Fatalf("answer=%+v", answer)
			}
		}
	}
	s.reportPermissionAnswered(run, PermissionAnswer{AskID: "a1", Decision: "allow", AnsweredBy: "policy"})
	checkPermissionHandoff(t, s, sink)
}
