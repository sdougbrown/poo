package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/session"
)

// leaseMemory is a store whose ownership is a lease, for a session built
// over it: the memory is the store, and the lost channel is the lease's.
type leaseMemory struct {
	*session.Memory
	lost chan struct{}
}

// LeaseLost returns the lease's loss channel.
func (m *leaseMemory) LeaseLost() <-chan struct{} { return m.lost }

// TestAServedSessionWatchesTheLeaseItsSinkWraps covers the wire's sink:
// a served session writes through a recording that wraps its store, and
// the lease-loss watcher must see the lease the wrapped store holds.
// Without the recording's pass-through a served run keeps executing after
// its lease is taken over, and only a refused write stops it.
func TestAServedSessionWatchesTheLeaseItsSinkWraps(t *testing.T) {
	lost := make(chan struct{})
	sink := &session.Recording{Store: &leaseMemory{Memory: session.NewMemory(), lost: lost}}
	sess, err := poo.New(poo.Options{
		Transport:    stalledTransport{},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir:   t.TempDir(),
		SystemPrompt: "the system prompt",
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	if sess.Broken() {
		t.Fatal("the session started broken")
	}

	// The takeover: the lease the recording's store holds is lost.
	close(lost)
	waitFor(t, "the session to note the lost lease", sess.Broken)
}
