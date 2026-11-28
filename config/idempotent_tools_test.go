// A tool's own spec is the only place its idempotency is declared, and the
// durable planner reads it from the session header. These tests pin the
// flag through the two conversions a session header is built from — the
// runtime's session tools and the profile listing — and through the
// header to a planned replay. The other spec-to-info copies keep the
// remaining listings consistent with the header.
package config_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/session"
)

// specTool is a tool whose spec the test writes, so a conversion has a
// declared idempotency to carry.
type specTool struct {
	spec poo.ToolSpec
}

// Spec returns the spec the test declared.
func (t specTool) Spec() poo.ToolSpec { return t.spec }

// Prepare never runs: these tests only read the spec.
func (specTool) Prepare(json.RawMessage, poo.PrepareEnv) (poo.Prepared, error) {
	return nil, nil
}

// Call never runs: these tests only read the spec.
func (specTool) Call(context.Context, poo.Prepared, poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{}, nil
}

// idempotentToolSpecs are the linked tools the fixture profile offers: a
// side-effecting tool that declares its repetition safe, and one that does
// not.
func idempotentToolSpecs() map[string]poo.ToolSpec {
	return map[string]poo.ToolSpec{
		"idem_write": {
			Name:        "idem_write",
			Description: "A write that is safe to repeat.",
			Schema:      json.RawMessage(`{"type":"object"}`),
			Class:       poo.ClassSideEffecting,
			Idempotent:  true,
		},
		"plain_write": {
			Name:        "plain_write",
			Description: "A write that is not safe to repeat.",
			Schema:      json.RawMessage(`{"type":"object"}`),
			Class:       poo.ClassSideEffecting,
		},
	}
}

// idempotentRuntime builds a runtime whose binary links the fixture tools
// and whose only profile offers them.
func idempotentRuntime(t *testing.T) *config.Runtime {
	t.Helper()
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	specs := idempotentToolSpecs()
	tools := make(map[string]config.ToolFactory, len(specs))
	for name, spec := range specs {
		tools[name] = func(config.ToolOptions) poo.Tool { return specTool{spec: spec} }
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "idem",
		Tools:          tools,
		Profiles: []profile.Profile{{
			Name:  "idem",
			Tools: []string{"idem_write", "plain_write"},
		}},
		Executors: theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	return runtime
}

// toolNamed returns the named tool's info.
func toolNamed(tools []poo.ToolInfo, name string) (poo.ToolInfo, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return poo.ToolInfo{}, false
}

// TestASpecsIdempotencyReachesEveryToolInfo covers the conversions: a
// tool that declares its repetition safe carries the flag into the header
// the session records and into the profile the build lists.
func TestASpecsIdempotencyReachesEveryToolInfo(t *testing.T) {
	runtime := idempotentRuntime(t)

	cases := []struct {
		name  string
		tools []poo.ToolInfo
	}{
		{name: "the session header", tools: runtime.SessionTools()},
		{name: "the profile listing", tools: runtime.Profile.Tools},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idem, ok := toolNamed(tc.tools, "idem_write")
			if !ok {
				t.Fatalf("%s has no idem_write: %+v", tc.name, tc.tools)
			}
			if !idem.Idempotent {
				t.Errorf("%s records idem_write as %+v, want it idempotent", tc.name, idem)
			}
			plain, ok := toolNamed(tc.tools, "plain_write")
			if !ok {
				t.Fatalf("%s has no plain_write: %+v", tc.name, tc.tools)
			}
			if plain.Idempotent {
				t.Errorf("%s records plain_write as %+v, want it not idempotent", tc.name, plain)
			}
		})
	}
}

// TestAnInterruptedIdempotentCallReplaysFromTheHeader covers the whole
// path: the runtime writes a header with the tool's flag, and the durable
// planner reads that header as its only tool source, so the interrupted
// call of an idempotent tool is replayed instead of settled.
func TestAnInterruptedIdempotentCallReplaysFromTheHeader(t *testing.T) {
	runtime := idempotentRuntime(t)
	created := runtime.CreatedRecord(string(session.ModeDurable), t.TempDir(), "s1")

	info, ok := toolNamed(created.Tools, "idem_write")
	if !ok {
		t.Fatalf("the header has no idem_write: %+v", created.Tools)
	}
	if !info.Idempotent {
		t.Errorf("the header records idem_write as %+v, want it idempotent", info)
	}

	recs := []poo.Record{
		created,
		&poo.RunStartedRecord{
			RecordMeta: poo.NewRecordMeta("run.started"),
			RunID:      "r1",
			StartedAt:  "2026-10-20T00:00:00Z",
			ModelID:    created.Model.ModelID,
			ToolsMode:  poo.ToolsMode(created.ToolsMode),
		},
		&poo.MessageRecord{
			RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
			RunID:      "r1",
			Turn:       1,
			Role:       poo.RoleAssistant,
			MessageID:  "m1",
			Content: []poo.ContentBlock{{
				Kind:       poo.ContentToolCall,
				ToolCallID: "call_1",
				Name:       "idem_write",
				Arguments:  json.RawMessage(`{}`),
			}},
		},
		&poo.ToolCallRecord{
			RecordMeta: poo.NewRecordMeta("tool.call"),
			RunID:      "r1",
			Turn:       1,
			Handle:     "r1",
			CallID:     "call_1",
			Tool:       "idem_write",
			Arguments:  json.RawMessage(`{}`),
		},
		&poo.ToolStartedRecord{
			RecordMeta: poo.NewRecordMeta("tool.started"),
			RunID:      "r1",
			Turn:       1,
			Handle:     "r1",
			CallID:     "call_1",
			Tool:       "idem_write",
		},
	}

	// The header is the planner's only tool source: no ToolLookup is
	// supplied, exactly as the stores' own openers supply none.
	plan, err := (session.Recovery{}).Resolve(
		context.Background(),
		pootest.NewSink(),
		session.PendingOf("s1", recs),
		session.ModeDurable,
		session.MetadataOfRecord(created),
	)
	if err != nil {
		t.Fatalf("resolving the pending session: %v", err)
	}
	if plan == nil {
		t.Fatal("the resolve planned nothing, want a continuation")
	}
	if len(plan.ReplayCalls) != 1 {
		t.Fatalf("the plan replays %d calls, want the idempotent tool's: %+v", len(plan.ReplayCalls), plan.ReplayCalls)
	}
	call := plan.ReplayCalls[0]
	if call.Reason != session.PlanReasonIdempotent {
		t.Errorf("the call is replayed because %q, want %q", call.Reason, session.PlanReasonIdempotent)
	}
}
