package poo_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// TestASettledRunUnderAReservationDoesNotStrandTheQueue covers the
// strand the fault-report waits caught in CI: a prompt's run settles
// while the prompt's own reservation still stands, and a result that
// queues in that window must be acted on once the reservation is
// released, not left on the queue with nothing to pump it.
//
// The seam is the transport's second request: the session holds it back
// until the settle has landed, so the wake the release owes sees a
// settled session. Without the pump-after-release the second run never
// starts and the wait below times out.
func TestASettledRunUnderAReservationDoesNotStrandTheQueue(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: "the first answer"},
		pootest.Turn{Text: "the second answer"},
	)
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	// The prompt returns with its reservation standing; the run is on
	// its way to settling.
	if _, err := s.Prompt(context.Background(), "the prompt"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The release clears the reservation after the run has settled.
	// The settle has to land first: the wait holds until run.settled is
	// in the sink, and the prompt's own wait inside runSession would
	// clear the reservation before that only if the runner scheduled it
	// so — this test pins the window where it does not.
	if err := tr.WaitRequests(context.Background(), 1); err != nil {
		t.Fatalf("waiting for the first request: %v", err)
	}
	waitForSink(t, sink, "the run to settle", func() bool { return sink.Settled() != nil })

	// A result the reservation released into: queued while the session
	// showed idle-with-reservation, and the release must act on it.
	s.QueueJobResult(poo.JobResult{
		JobID:   "j1",
		Handle:  "r1",
		Tool:    "job",
		Text:    "the job's output",
		Outcome: poo.OutcomeOK,
	})

	// The released queue wakes a run, and the run delivers the result
	// the model reads.
	if err := tr.WaitRequests(context.Background(), 2); err != nil {
		t.Fatalf("the queued result never woke a second run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the woken run: %v", err)
	}
	_ = res
	waitForSink(t, sink, "the job's result to reach the chat model", func() bool {
		for _, m := range sink.messages() {
			if len(m.Content) > 0 && m.Origin == poo.OriginJob {
				return true
			}
		}
		return false
	})
}
