package poo_test

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
)

// The compaction seam is frozen in this stage, and the store records what
// a compactor did, so every type the seam carries has to reach a record
// under the plan's field names.

// TestTheCompactionUsesTheRecordVocabulary covers the compaction's own
// field names: a compaction record is snake_case, and the forms are named
// after what they do.
func TestTheCompactionUsesTheRecordVocabulary(t *testing.T) {
	compaction := poo.Compaction{
		Strategy: "handoff",
		Replace: &poo.RangeReplacement{
			Cut:      poo.TurnRange{First: 2, Last: 3},
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
		},
	}
	raw, err := json.Marshal(compaction)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if decoded["strategy"] != "handoff" {
		t.Errorf("the compaction's strategy is %v, want handoff", decoded["strategy"])
	}
	replace, ok := decoded["replace"].(map[string]any)
	if !ok {
		t.Fatalf("the compaction's replace is %v, want an object", decoded["replace"])
	}
	cut, ok := replace["cut"].(map[string]any)
	if !ok {
		t.Fatalf("the replacement's cut is %v, want an object", replace["cut"])
	}
	if cut["first"] != float64(2) || cut["last"] != float64(3) {
		t.Errorf("the cut is %v, want the turns 2 to 3", cut)
	}
	if _, ok := replace["replacement"]; !ok {
		t.Errorf("the replacement has no replacement field: %v", replace)
	}
}

// TestTheCompactionFormsRoundTrip covers each of the four forms: a
// compaction that reaches a record and comes back holds the same form.
func TestTheCompactionFormsRoundTrip(t *testing.T) {
	forms := []poo.Compaction{
		{
			Strategy: "summarize",
			Replace: &poo.RangeReplacement{
				Cut:      poo.TurnRange{First: 1, Last: 2},
				Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
			},
		},
		{
			Strategy: "rewrite",
			Rewrite: []poo.ToolResultRewrite{{
				Handle:  "r1",
				Content: "[r1 · bash · compacted] the shorter result",
			}},
		},
		{
			Strategy: "window",
			Window: &poo.ProviderWindow{Items: []poo.ContentBlock{{
				Kind: poo.ContentProvider,
				Provider: &poo.ProviderBlock{
					API:      poo.APIResponses,
					Type:     "compaction",
					Data:     json.RawMessage(`{"encrypted_content":"abc"}`),
					Required: true,
				},
			}}},
		},
		{
			Strategy: "handoff",
			End:      &poo.EndRun{Handoff: "the note the next agent picks up"},
		},
	}

	for _, want := range forms {
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("encoding a %s compaction: %v", want.Strategy, err)
		}
		var back poo.Compaction
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("decoding a %s compaction: %v", want.Strategy, err)
		}
		if back.Strategy != want.Strategy {
			t.Errorf("the strategy is %q, want %q", back.Strategy, want.Strategy)
		}
		switch {
		case want.Replace != nil:
			if back.Replace == nil {
				t.Fatalf("the %s compaction came back without its replacement", want.Strategy)
			}
			if back.Replace.Cut != want.Replace.Cut {
				t.Errorf("the cut is %+v, want %+v", back.Replace.Cut, want.Replace.Cut)
			}
			if len(back.Replace.Messages) != len(want.Replace.Messages) {
				t.Errorf("the replacement holds %d messages, want %d",
					len(back.Replace.Messages), len(want.Replace.Messages))
			}
		case want.Rewrite != nil:
			if len(back.Rewrite) != len(want.Rewrite) {
				t.Fatalf("the %s compaction came back with %d rewrites, want %d",
					want.Strategy, len(back.Rewrite), len(want.Rewrite))
			}
			if back.Rewrite[0].Handle != want.Rewrite[0].Handle {
				t.Errorf("the rewrite's handle is %q, want %q",
					back.Rewrite[0].Handle, want.Rewrite[0].Handle)
			}
			if back.Rewrite[0].Content != want.Rewrite[0].Content {
				t.Errorf("the rewrite's content is %q, want %q",
					back.Rewrite[0].Content, want.Rewrite[0].Content)
			}
		case want.Window != nil:
			if back.Window == nil {
				t.Fatalf("the %s compaction came back without its window", want.Strategy)
			}
			if len(back.Window.Items) != len(want.Window.Items) {
				t.Fatalf("the window holds %d items, want %d",
					len(back.Window.Items), len(want.Window.Items))
			}
			if back.Window.Items[0].Provider == nil {
				t.Fatal("the window's item came back without its provider block")
			}
			if string(back.Window.Items[0].Provider.Data) != `{"encrypted_content":"abc"}` {
				t.Errorf("the window's payload is %s, want the original",
					back.Window.Items[0].Provider.Data)
			}
		case want.End != nil:
			if back.End == nil {
				t.Fatalf("the %s compaction came back without its end", want.Strategy)
			}
			if back.End.Handoff != want.End.Handoff {
				t.Errorf("the handoff is %q, want %q", back.End.Handoff, want.End.Handoff)
			}
		}
	}
}
