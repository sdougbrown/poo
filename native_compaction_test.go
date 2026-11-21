package poo_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/native"
	"github.com/sdougbrown/poo/pootest"
)

// nativeOptions is a session that compacts natively when the next answer no
// longer fits.
//
// The window is small enough that the first turn's reported usage fills
// it, and the fake transport is the provider that compacts.
func nativeOptions(t *testing.T, tr *pootest.Transport, result string, sink poo.RecordSink) poo.Options {
	t.Helper()
	opts := testOptions(t, tr, &echoTool{result: result}, sink)
	opts.Model.Ops = []poo.Op{poo.OpChat, poo.OpCompact}
	opts.Model.ContextLimit = 4000
	// The answer's own limit is small enough that the window has room for
	// it until the usage fills most of the window.
	opts.Model.MaxOutput = 512
	opts.Compactor = native.New(native.Options{})
	return opts
}

// hasProviderBlock reports that the request carries a provider window.
func hasProviderBlock(req poo.ChatRequest) bool {
	for _, m := range req.Messages {
		for _, c := range m.Content {
			if c.Provider != nil {
				return true
			}
		}
	}
	return false
}

// compactionEndedError returns the failure the run's compaction reported.
func compactionEndedError(t *testing.T, observer *collectingObserver) string {
	t.Helper()
	waitFor(t, "the compaction to report its failure", func() bool {
		for _, ev := range observer.snapshot() {
			if ev.Name == poo.EventCompactionEnded {
				return true
			}
		}
		return false
	})
	for _, ev := range observer.snapshot() {
		if ev.Name != poo.EventCompactionEnded {
			continue
		}
		text, _ := ev.Payload["error"].(string)
		return text
	}
	return ""
}

// TestNativeCompactionRunsAtTheThresholdAndRestores covers the supported
// path. The window reaches the strategy's share, the transport produces a
// provider window, the next request carries it. A restored session holds the
// same compacted view.
func TestNativeCompactionRunsAtTheThresholdAndRestores(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 3600, OutputTokens: 5}},
		{text: "the answer"},
	})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	sink := &recordingSink{}
	s := newTestSession(t, nativeOptions(t, tr, "echoed", sink))

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if got := tr.Compactions(); got != 1 {
		t.Fatalf("the transport served %d native compactions, want 1", got)
	}
	requests := tr.Requests()
	if len(requests) != 2 {
		t.Fatalf("the transport got %d requests, want the turn and the answer", len(requests))
	}
	if hasProviderBlock(requests[0]) {
		t.Error("the first request carries a provider window, want none before the compaction")
	}
	if !hasProviderBlock(requests[1]) {
		t.Error("the request after the compaction carries no provider window")
	}

	var compaction *poo.CompactionRecord
	for _, rec := range sink.allRecords() {
		if c, ok := rec.(*poo.CompactionRecord); ok {
			compaction = c
		}
	}
	if compaction == nil {
		t.Fatal("the log holds no compaction record")
	}
	if compaction.Strategy != native.Strategy {
		t.Errorf("the recorded strategy is %q, want %q", compaction.Strategy, native.Strategy)
	}

	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	restoredOpts := restoreOptions(t, scripted(nil), &recordingSink{})
	restoredOpts.Compactor = native.New(native.Options{})
	restored, resumed, err := poo.Restore(restoredOpts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { restored.Close() })
	if resumed.Compactions != 1 {
		t.Fatalf("the restore applied %d compactions, want 1", resumed.Compactions)
	}
	found := false
	for _, m := range poo.SessionView(restored).Messages() {
		for _, c := range m.Content {
			if c.Provider != nil {
				found = true
			}
		}
	}
	if !found {
		t.Error("the restored view holds no provider window")
	}
}

// TestANativeCompactionNamesTheSessionItCompactsFor covers the identity the
// core hands the transport. A compaction runs inside one session, and only
// the core knows which: the transport can name the session's own id as the
// endpoint's cache key only when the compaction carries it.
func TestANativeCompactionNamesTheSessionItCompactsFor(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 3600, OutputTokens: 5}},
		{text: "the answer"},
	})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	opts := nativeOptions(t, tr, "echoed", &recordingSink{})
	opts.SessionID = "the-session-being-compacted"
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	native := tr.NativeCompactions()
	if len(native) != 1 {
		t.Fatalf("the transport was given %d native compactions, want 1", len(native))
	}
	if got := native[0].SessionID; got != opts.SessionID {
		t.Errorf("the compaction was given the session %q, want the session's own id %q", got, opts.SessionID)
	}
}

// TestNativeCompactionWaitsBelowTheThreshold covers the other side of the
// boundary: a window under the strategy's share is not compacted.
func TestNativeCompactionWaitsBelowTheThreshold(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 3200, OutputTokens: 5}},
		{text: "the answer"},
	})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	s := newTestSession(t, nativeOptions(t, tr, "echoed", &recordingSink{}))

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if got := tr.Compactions(); got != 0 {
		t.Errorf("the transport served %d native compactions below the ratio, want none", got)
	}
	requests := tr.Requests()
	if len(requests) != 2 {
		t.Fatalf("the transport got %d requests, want the turn and the answer", len(requests))
	}
	if hasProviderBlock(requests[1]) {
		t.Error("the request carries a provider window below the threshold")
	}
}

// TestNativeCompactionRefusesAModelWithoutTheCompactOperation covers a model
// that does not declare the operation. The run does not ask the transport to
// compact, and the failure names the missing declaration.
func TestNativeCompactionRefusesAModelWithoutTheCompactOperation(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 3400, OutputTokens: 5}},
		{text: "the answer"},
	})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	opts := nativeOptions(t, tr, "echoed", &recordingSink{})
	opts.Model.Ops = []poo.Op{poo.OpChat}
	observer := &collectingObserver{}
	s := newTestSession(t, opts, poo.WithObserver(observer))

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn with room for the next request", res.Reason)
	}
	if got := tr.Compactions(); got != 0 {
		t.Errorf("the transport served %d native compactions for a model without the operation, want none", got)
	}
	text := compactionEndedError(t, observer)
	if !strings.Contains(text, "compact") || !strings.Contains(text, "test-model") {
		t.Errorf("the compaction failed with %q, want it to name the missing compact operation", text)
	}
	if len(tr.Requests()) != 2 || hasProviderBlock(tr.Requests()[1]) {
		t.Error("the unsupported compaction changed the view")
	}
}

// TestNativeCompactionRefusesATransportWithoutNativeCompaction covers a model
// that declares the operation on a transport that cannot carry it. The failure
// names the unsupported transport and what to use instead. The transport is
// never asked.
func TestNativeCompactionRefusesATransportWithoutNativeCompaction(t *testing.T) {
	inner := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 3400, OutputTokens: 5}},
		{text: "the answer"},
	})
	opts := nativeOptions(t, inner, "echoed", &recordingSink{})
	opts.Transport = &plainTransport{inner: inner}
	observer := &collectingObserver{}
	s := newTestSession(t, opts, poo.WithObserver(observer))

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn with room for the next request", res.Reason)
	}
	if got := inner.Compactions(); got != 0 {
		t.Errorf("the transport served %d native compactions, want none", got)
	}
	text := compactionEndedError(t, observer)
	if !strings.Contains(text, "native") || !strings.Contains(text, "summary") {
		t.Errorf("the compaction failed with %q, want it to name native support and the summary strategy", text)
	}
	if len(inner.Requests()) != 2 || hasProviderBlock(inner.Requests()[1]) {
		t.Error("the unsupported compaction changed the view")
	}
}

// TestANativeSessionDoesNotCompactASwitchedModelWithoutTheOperation covers
// the model switch: the new model serves chat but not compaction, so the
// strategy must not use native for it.
func TestANativeSessionDoesNotCompactASwitchedModelWithoutTheOperation(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 3400, OutputTokens: 5}},
		{text: "the answer"},
	})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	observer := &collectingObserver{}
	s := newTestSession(t, nativeOptions(t, tr, "echoed", &recordingSink{}), poo.WithObserver(observer))

	if _, err := s.SetModel(poo.ModelInfo{
		Name: "plain", ModelID: "test-model", ContextLimit: 4000,
		Ops: []poo.Op{poo.OpChat},
	}); err != nil {
		t.Fatalf("switching to a chat-only model: %v", err)
	}
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn with room for the next request", res.Reason)
	}
	if got := tr.Compactions(); got != 0 {
		t.Errorf("the transport served %d native compactions for the switched model, want none", got)
	}
	text := compactionEndedError(t, observer)
	if !strings.Contains(text, "compact") || !strings.Contains(text, "plain") {
		t.Errorf("the compaction failed with %q, want it to name the switched model's missing operation", text)
	}
}

// TestANativeSessionDoesNotCompactASwitchedTransportWithoutTheCapability
// covers a switch to a model that declares compaction on a transport that
// cannot produce a window.
func TestANativeSessionDoesNotCompactASwitchedTransportWithoutTheCapability(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 3400, OutputTokens: 5}},
		{text: "the answer"},
	})
	inner := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 3400, OutputTokens: 5}},
		{text: "the answer"},
	})
	observer := &collectingObserver{}
	s := newTestSession(t, nativeOptions(t, tr, "echoed", &recordingSink{}), poo.WithObserver(observer))

	plain := &plainTransport{inner: inner}
	if _, err := s.SetModel(poo.ModelInfo{
		Name: "native-elsewhere", ModelID: "test-model", ContextLimit: 4000,
		Ops: []poo.Op{poo.OpChat, poo.OpCompact},
	}, plain); err != nil {
		t.Fatalf("switching to the other provider: %v", err)
	}
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn with room for the next request", res.Reason)
	}
	if got := inner.Compactions(); got != 0 {
		t.Errorf("the switched transport served %d native compactions, want none", got)
	}
	if got := tr.Compactions(); got != 0 {
		t.Errorf("the original transport served %d native compactions, want none", got)
	}
	text := compactionEndedError(t, observer)
	if !strings.Contains(text, "native") || !strings.Contains(text, "summary") {
		t.Errorf("the compaction failed with %q, want it to name native support and the summary strategy", text)
	}
}

// TestANativeSessionCompactsASwitchedModelThatDeclaresTheOperation covers the
// supported switch: the new model declares compaction, so the strategy still
// runs natively on it.
func TestANativeSessionCompactsASwitchedModelThatDeclaresTheOperation(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 3400, OutputTokens: 5}},
		{text: "the answer"},
	})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	s := newTestSession(t, nativeOptions(t, tr, "echoed", &recordingSink{}))

	if _, err := s.SetModel(poo.ModelInfo{
		Name: "native-again", ModelID: "test-model", ContextLimit: 4000,
		Ops: []poo.Op{poo.OpChat, poo.OpCompact},
	}); err != nil {
		t.Fatalf("switching to the other native model: %v", err)
	}
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if got := tr.Compactions(); got != 1 {
		t.Errorf("the transport served %d native compactions, want 1", got)
	}
}

// TestANativeCompactionDeclinesWhileTheCompactedInputIsShort covers the
// strategy's own limit: a compaction that left the input short of the window
// must not repeat every turn, so the run settles at the gate instead.
//
// The report a compaction made for itself is not the view's input, so it does
// not arm the limit: only the first turn's report after a compaction does.
func TestANativeCompactionDeclinesWhileTheCompactedInputIsShort(t *testing.T) {
	tr := scripted([]fakeResponse{
		// The first turn fills the window, and the strategy compacts.
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}, usage: &poo.UsageReport{InputTokens: 3600, OutputTokens: 5}},
		// The report after the compaction is still short of the window...
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}, usage: &poo.UsageReport{InputTokens: 3300, OutputTokens: 5}},
		// ...and the last turn leaves the window without the room a request
		// needs.
		{calls: []poo.ToolCall{{CallID: "c3", Name: "echo", Arguments: []byte(`{"text":"three"}`)}}, usage: &poo.UsageReport{InputTokens: 3500, OutputTokens: 5}},
	})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	// The compaction's own request reported usage. It is the compaction's, not
	// the view's, and a guard armed by it would let the strategy compact again.
	tr.SetWindowUsage(&poo.UsageReport{InputTokens: 50, OutputTokens: 5})
	sink := &recordingSink{}
	s := newTestSession(t, nativeOptions(t, tr, "echoed", sink))

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q (%s), want context_exhausted", res.Reason, res.Detail.Text)
	}
	if res.Detail.Source != "no_output_room" {
		t.Errorf("the settle source is %q, want no_output_room from the gate", res.Detail.Source)
	}
	if got := tr.Compactions(); got != 1 {
		t.Errorf("the transport served %d native compactions, want the one the first turn bought", got)
	}
	// Two turns followed the compaction and the run kept going through both
	// gates, so the guard was the only thing that stopped the second one.
	if got := len(tr.Requests()); got != 3 {
		t.Errorf("the transport got %d requests, want three turns", got)
	}
	var compactionUsage bool
	for _, rec := range sink.allRecords() {
		usage, ok := rec.(*poo.UsageRecord)
		if ok && usage.Origin == poo.OriginCompaction {
			compactionUsage = true
		}
	}
	if !compactionUsage {
		t.Error("the log holds no usage record for the compaction's own request")
	}
}
