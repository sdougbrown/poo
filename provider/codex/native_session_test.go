package codex_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/native"
	"github.com/sdougbrown/poo/pootest"
)

// TestASessionCompactsNativelyOverTheCodexEndpoint covers the strategy
// through the real transport and a fake endpoint. The turn fills the window,
// the compaction request asks the endpoint for its own state. The next request
// gives the state back verbatim.
func TestASessionCompactsNativelyOverTheCodexEndpoint(t *testing.T) {
	srv := newServer(t,
		frame(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_1","name":"echo"}}`)+
			frame(`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"text\":\"ping\"}"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":900,"output_tokens":5}}}`),
		compactionStream(compactionItem),
		frame(`{"type":"response.output_text.delta","output_index":0,"delta":"the answer"}`)+
			frame(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":20,"output_tokens":3}}}`),
	)
	sink := &pootest.Sink{}
	opts := sessionOptions(t, srv.URL, sink)
	opts.Model.ContextLimit = 1000
	opts.Model.Ops = []poo.Op{poo.OpChat, poo.OpCompact}
	opts.Compactor = native.New(native.Options{})
	session, err := poo.New(opts)
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer session.Close()

	if _, err := session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if result.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", result.Reason)
	}
	if result.Turns != 2 {
		t.Errorf("the run made %d turns, want 2: the compaction is not a turn", result.Turns)
	}
	if n := srv.count(); n != 3 {
		t.Fatalf("the endpoint received %d requests, want the turn, the compaction and the answer", n)
	}

	compactionReq := srv.request(t, 1)
	input := field[[]map[string]any](t, compactionReq, "input")
	if len(input) == 0 {
		t.Fatalf("the compaction request carries no input: %s", compactionReq.body)
	}
	if got := input[len(input)-1]["type"]; got != "compaction_trigger" {
		t.Errorf("the compaction request ends with %v, want the compaction trigger", got)
	}

	answerReq := srv.request(t, 2)
	if !strings.Contains(string(answerReq.body), compactionItem) {
		t.Errorf("the request after the compaction does not carry the window verbatim: %s", answerReq.body)
	}
	items := field[[]map[string]any](t, answerReq, "input")
	if len(items) == 0 || items[0]["type"] != "compaction" {
		t.Errorf("the request after the compaction starts with %v, want the provider window", items)
	}
}
