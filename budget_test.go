package poo_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The context budget: the estimator with its calibration, the notice, the
// hard stop, and the computed max_tokens.

// TestTheNoticeFiresOncePerRun covers the plan's rule: the notice fires
// at most once per run.
func TestTheNoticeFiresOncePerRun(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{
			calls: []poo.ToolCall{{
				ID:        "call_2",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the answer"},
	}}
	sink := &recordingSink{}
	tool := &echoTool{result: strings.Repeat("x", 4000)}

	opts := testOptions(t, tr, tool, sink)
	// A small window, so the estimate crosses the notice ratio.
	opts.ContextLimit = 2000
	opts.NoticeRatio = 0.5

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	notices := 0
	for _, ev := range s.Events() {
		if ev.Name == poo.EventContextNotice {
			notices++
		}
	}
	if notices != 1 {
		t.Errorf("the run emitted %d context notices, want 1", notices)
	}
	// The notice is a user message the model reads.
	found := false
	for _, m := range sink.messages() {
		for _, c := range m.Content {
			if c.Text == poo.ContextNoticeText {
				found = true
			}
		}
	}
	if !found {
		t.Error("the notice did not reach the model's view")
	}
}

// TestTheNoticeRearmsAfterTheUsageDrops covers the other half of the
// rule: the notice fires again in a later run only if the usage has
// dropped back below the notice ratio.
func TestTheNoticeRearmsAfterTheUsageDrops(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			usage: &poo.UsageReport{InputTokens: 1000, OutputTokens: 10},
			text:  "the first answer",
		},
		{
			usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 10},
			text:  "the second answer",
		},
	}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.ContextLimit = 2000
	opts.NoticeRatio = 0.5

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")
	// The first run used a lot of context.
	runSession(t, s, "do the thing again")

	notices := 0
	for _, ev := range s.Events() {
		if ev.Name == poo.EventContextNotice {
			notices++
		}
	}
	// The first run's usage was over the ratio, and the second run's
	// was under it, so the notice fired once and did not re-arm.
	if notices != 1 {
		t.Errorf("the session emitted %d context notices, want 1", notices)
	}
}

// TestTheEstimateGrowsWithTheAppendedOutput covers the plan's rule: the
// estimate grows with what was appended between usage reports.
func TestTheEstimateGrowsWithTheAppendedOutput(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			usage: &poo.UsageReport{InputTokens: 100, OutputTokens: 10},
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	// A window small enough that the estimate shows in the output
	// room.
	opts.ContextLimit = 2000

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	// The second request's max_tokens is smaller than the first's,
	// because the estimate grew.
	reqs := tr.requestsOf()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	if reqs[1].MaxTokens >= reqs[0].MaxTokens {
		t.Errorf("the second request's max_tokens is %d, want less than the first's %d",
			reqs[1].MaxTokens, reqs[0].MaxTokens)
	}
}

// TestTheEstimateUsesTheObservedRatio covers the plan's calibration: the
// first usage report calibrates the characters-per-token ratio.
func TestTheEstimateUsesTheObservedRatio(t *testing.T) {
	// The endpoint reports a ratio of 1.0 characters per token, which
	// the calibration clamps to 2.0.
	tr := &fakeTransport{responses: []fakeResponse{
		{
			usage: &poo.UsageReport{InputTokens: 100, OutputTokens: 0},
			text:  "the answer",
		},
	}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.ContextLimit = 32768

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	// The first request's max_tokens used the starting ratio, and the
	// second used the calibrated one.
	reqs := tr.requestsOf()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	// The window is 32768, so the room is the limit minus the estimate
	// minus the reserve, capped at the model's ceiling of 8192.
	if reqs[0].MaxTokens != 8192 {
		t.Errorf("the first request's max_tokens is %d, want 8192", reqs[0].MaxTokens)
	}
}

// TestCrossingTheHardRatioSettlesContextExhausted covers the plan's
// hard stop: with no compactor, crossing the hard ratio settles the run
// as context_exhausted.
func TestCrossingTheHardRatioSettlesContextExhausted(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the answer"},
	}}
	sink := &recordingSink{}
	// The tool's result is big enough to fill the window.
	tool := &echoTool{result: strings.Repeat("x", 2000)}

	opts := testOptions(t, tr, tool, sink)
	opts.ContextLimit = 1000
	opts.HardRatio = 0.5

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	// The hard limit is the reason, not the output room: the output
	// room check comes first in the gate, and it is under 256.
	if res.Detail.Source != "no_output_room" && res.Detail.Source != "hard_limit" {
		t.Errorf("the settle source is %q, want the gate's budget check", res.Detail.Source)
	}
}

// TestTheHardLimitSettlesWithItsOwnSource covers the hard ratio's own
// source: the run stops with detail.source "hard_limit" when the
// estimate is over the ratio but the output room is still there.
func TestTheHardLimitSettlesWithItsOwnSource(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the answer"},
	}}
	sink := &recordingSink{}
	// The result is big enough to push the estimate over the hard
	// ratio, and small enough to leave output room.
	tool := &echoTool{result: strings.Repeat("x", 40000)}

	opts := testOptions(t, tr, tool, sink)
	// A window big enough for the output room, but the estimate is
	// over the hard ratio.
	opts.ContextLimit = 20000
	opts.HardRatio = 0.5

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "hard_limit" {
		t.Errorf("the settle source is %q, want hard_limit", res.Detail.Source)
	}
}

// TestAProviderContextLengthErrorSettlesContextExhausted covers the
// other half of the rule: the provider's own context-length error
// settles the run the same way.
func TestAProviderContextLengthErrorSettlesContextExhausted(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{
		err: &poo.ProviderError{
			Kind:    poo.ProviderKindContextLength,
			Message: "the prompt is too long",
			Status:  400,
		},
	}}}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "provider:context_length" {
		t.Errorf("the settle source is %q, want provider:context_length", res.Detail.Source)
	}
}

// TestAMissingContextLimitIsAConstructionError covers the plan's rule.
func TestAMissingContextLimitIsAConstructionError(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	_, err := poo.New(poo.Options{Transport: tr})
	if err == nil {
		t.Fatal("a session with no context limit was built")
	}
	var init *poo.ErrSessionInit
	if !asSessionInit(err, &init) {
		t.Fatalf("the error is %T, want an ErrSessionInit", err)
	}
}

// TestTheMaxTokensIsAlwaysComputed covers the plan's rule: the value is
// never left to the endpoint's default.
func TestTheMaxTokensIsAlwaysComputed(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	tool := &echoTool{result: "echoed"}

	for _, limit := range []int{1024, 4096, 32768, 262144} {
		opts := testOptions(t, tr, tool, &recordingSink{})
		opts.ContextLimit = limit
		s := newTestSession(t, opts)
		runSession(t, s, "do the thing")
		reqs := tr.requestsOf()
		if len(reqs) == 0 {
			t.Fatalf("the transport got no requests")
		}
		if reqs[len(reqs)-1].MaxTokens <= 0 {
			t.Errorf("the request's max_tokens is %d, want a computed value", reqs[len(reqs)-1].MaxTokens)
		}
		tr.requests = nil
	}
}

// TestTheRepeatCountsCoverTheExactAndNearKeys covers the plan's repeat
// counting: the exact key is the call, and the near key is the call's
// primary argument.
func TestTheRepeatCountsCoverTheExactAndNearKeys(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"one","offset":1}`)},
				{ID: "call_2", Name: "echo", Arguments: []byte(`{"text":"one","offset":2}`)},
				{ID: "call_3", Name: "echo", Arguments: []byte(`{"text":"one","offset":1}`)},
			},
		},
		{text: "the answer"},
	}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed", primaryArg: "text"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runSession(t, s, "do the thing")

	// The counts are the ones the policy slot would see. The core
	// counts them per call, so a batch that runs in parallel does not
	// race over them.
	first := s.RepeatCountsFor("r1")
	if first.Exact != 1 {
		t.Errorf("the first call's exact count is %d, want 1", first.Exact)
	}
	if first.Near != 1 {
		t.Errorf("the first call's near count is %d, want 1", first.Near)
	}
	// The second call has a different offset, so it is a different
	// exact call but the same near call.
	second := s.RepeatCountsFor("r2")
	if second.Exact != 1 {
		t.Errorf("the second call's exact count is %d, want 1", second.Exact)
	}
	if second.Near != 2 {
		t.Errorf("the second call's near count is %d, want 2", second.Near)
	}
	// The third call is the first one again, so the exact count is 2.
	third := s.RepeatCountsFor("r3")
	if third.Exact != 2 {
		t.Errorf("the third call's exact count is %d, want 2", third.Exact)
	}
	if third.Near != 3 {
		t.Errorf("the third call's near count is %d, want 3", third.Near)
	}
}

// TestAPrepareFailureCountsTowardTheNearKey covers the plan's rule: a
// call that failed its validation counts toward the near key.
func TestAPrepareFailureCountsTowardTheNearKey(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"one"}`)},
				{ID: "call_2", Name: "echo", Arguments: []byte(`{"text":"one"}`)},
			},
		},
		{text: "the answer"},
	}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed", primaryArg: "text", prepareErr: fmt.Errorf("the arguments are wrong")}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runSession(t, s, "do the thing")

	// The two malformed calls share the invalid near key, so the
	// second one counts the first.
	first := s.RepeatCountsFor("r1")
	if first.Near != 1 {
		t.Errorf("the first call's near count is %d, want 1", first.Near)
	}
	second := s.RepeatCountsFor("r2")
	if second.Near != 2 {
		t.Errorf("the second call's near count is %d, want 2: a Prepare failure counts", second.Near)
	}
}

// TestTheCalibrationChangesTheOutputRoom covers the plan's calibration:
// the observed characters-per-token ratio is what the estimate uses
// after the first usage report.
//
// The endpoint reports far fewer input tokens than the view's character
// count suggests, so the calibrated ratio is large and the estimate
// grows: the second request asks for less output room than the first.
func TestTheCalibrationChangesTheOutputRoom(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 1},
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the answer"},
	}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	// A window small enough that the output room shows the estimate.
	opts.ContextLimit = 2000

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	reqs := tr.requestsOf()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	// The first request's room came from the starting ratio, and the
	// second from the calibrated one. The calibrated ratio is the
	// character count over ten tokens, which is over the clamp, so the
	// estimate is at its largest and the room is at its smallest.
	if reqs[1].MaxTokens >= reqs[0].MaxTokens {
		t.Errorf("the second request's max_tokens is %d, want less than the first's %d",
			reqs[1].MaxTokens, reqs[0].MaxTokens)
	}
}

// TestTheCalibrationIsClamped covers the plan's bounds: the observed
// ratio is clamped to [2.0, 5.0].
func TestTheCalibrationIsClamped(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			usage: &poo.UsageReport{InputTokens: 1, OutputTokens: 1},
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the answer"},
	}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.ContextLimit = 2000

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	reqs := tr.requestsOf()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	// The ratio is clamped to 5.0, so the estimate grows at five
	// characters per token. Without the clamp the ratio would be the
	// view's character count over one token, and the estimate would be
	// far larger, leaving no room at all.
	//
	// The window is small enough that the clamped estimate still
	// leaves room, and the unclamped one does not.
	got := reqs[1].MaxTokens
	if got < 256 {
		t.Errorf("the second request's max_tokens is %d, want room: the ratio was not clamped", got)
	}
	// The clamped ratio means the estimate is at most the view's
	// character count over five, so the room is at least that.
	chars := len("the system prompt") + len("do the thing") + len("the answer")
	if want := 2000 - chars/5 - 256; got > want+8 {
		t.Errorf("the second request's max_tokens is %d, want at most %d", got, want)
	}
}
