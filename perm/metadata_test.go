package perm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type metadataAsker struct {
	panicAt string
	calls   atomic.Int32
}

func (a *metadataAsker) Available() bool {
	if a.panicAt == "available" {
		panic("available")
	}
	return true
}
func (a *metadataAsker) Budget() time.Duration {
	if a.panicAt == "budget" {
		panic("budget")
	}
	return time.Minute
}
func (a *metadataAsker) AskerName() string {
	if a.panicAt == "name" {
		panic("name")
	}
	return "metadata"
}
func (a *metadataAsker) CanEverAnswer() bool {
	if a.panicAt == "ever" {
		panic("ever")
	}
	return true
}
func (a *metadataAsker) Ask(context.Context, Ask) (Decision, error) {
	a.calls.Add(1)
	if a.panicAt == "ask" {
		panic("ask")
	}
	return DecisionAllow, nil
}

func TestAskerPanicsDeferAtEveryBoundary(t *testing.T) {
	for _, method := range []string{"available", "budget", "name", "ask"} {
		t.Run(method, func(t *testing.T) {
			a := &metadataAsker{panicAt: method}
			c := &chain{askers: []Asker{a}, timeout: time.Hour, fallback: ActionDeny, clock: systemClock{}}
			if got := c.ask(context.Background(), Ask{}); got.decision != DecisionDeny || got.answeredBy != fallbackName {
				t.Fatalf("answer=%+v", got)
			}
			if method != "ask" && a.calls.Load() != 0 {
				t.Fatal("panicking metadata still reached Ask")
			}
		})
	}
	if canEverAnswer(&metadataAsker{panicAt: "ever"}) {
		t.Fatal("panicking availability claimed it could answer")
	}
}

type blockedMetadataAsker struct{ entered, release, exited chan struct{} }

func (a *blockedMetadataAsker) Available() bool {
	close(a.entered)
	<-a.release
	close(a.exited)
	return true
}
func (*blockedMetadataAsker) Budget() time.Duration                      { return time.Minute }
func (*blockedMetadataAsker) Ask(context.Context, Ask) (Decision, error) { return DecisionAllow, nil }

func TestBlockedAskerMetadataObeysChainDeadline(t *testing.T) {
	a := &blockedMetadataAsker{entered: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
	c := &chain{askers: []Asker{a}, timeout: 5 * time.Millisecond, fallback: ActionDeny, clock: systemClock{}}
	done := make(chan resolution, 1)
	go func() { done <- c.ask(context.Background(), Ask{}) }()
	<-a.entered
	select {
	case got := <-done:
		if got.decision != DecisionDeny || got.provenance != "timeout" {
			t.Errorf("answer=%+v", got)
		}
	case <-time.After(time.Second):
		t.Error("metadata blocked chain deadline")
	}
	close(a.release)
	<-a.exited
}
