package poo_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
)

// The settle reasons are a closed enum, and each one has a way the run
// reaches it. The table below drives the run into each reason and checks
// what the settle record says.

// TestEverySettleReasonHasAWayToReachIt covers the enum: every reason
// the core can produce is produced by a run, and the settle record says
// which one it was.
func TestEverySettleReasonHasAWayToReachIt(t *testing.T) {
	cases := []struct {
		// name is the case's name.
		name string
		// want is the reason the run settles with.
		want poo.Reason
		// source is the settle detail's source.
		source string
		// build sets up the session.
		build func(t *testing.T, opts *poo.Options) *poo.Session
		// act drives the run into the reason.
		act func(t *testing.T, s *poo.Session)
	}{
		{
			name:   "end_turn",
			want:   poo.ReasonEndTurn,
			source: "end_turn",
			build:  plainSession,
			act:    func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "output_truncated",
			want:   poo.ReasonOutputTruncated,
			source: "output_truncated",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.Transport = scripted([]fakeResponse{{
					text:   "half an ans",
					finish: poo.FinishLength,
				}})
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "max_turns",
			want:   poo.ReasonMaxTurns,
			source: "max_turns",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.Transport = scripted([]fakeResponse{{
					calls: []poo.ToolCall{{
						ID:        "call_1",
						Name:      "echo",
						Arguments: []byte(`{"text":"hello"}`),
					}},
				}})
				opts.MaxTurns = 1
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "budget_exhausted",
			want:   poo.ReasonBudgetExhausted,
			source: "max_run_tokens",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.Transport = scripted([]fakeResponse{{
					usage: &poo.UsageReport{InputTokens: 100, OutputTokens: 20},
					calls: []poo.ToolCall{{
						ID:        "call_1",
						Name:      "echo",
						Arguments: []byte(`{"text":"hello"}`),
					}},
				}})
				opts.MaxRunTokens = 50
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "stop_requested",
			want:   poo.ReasonStopRequested,
			source: "stop_condition",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.Transport = scripted([]fakeResponse{{
					calls: []poo.ToolCall{{
						ID:        "call_1",
						Name:      "echo",
						Arguments: []byte(`{"text":"hello"}`),
					}},
				}})
				opts.StopConditions = []poo.StopCondition{poo.HasToolCall("echo")}
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "hook_error",
			want:   poo.ReasonHookError,
			source: "after_turn:failingAfterTurn",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.AfterTurn = []poo.AfterTurnHook{&failingAfterTurn{}}
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "hook_continue_limit",
			want:   poo.ReasonHookContinueLimit,
			source: "before_settle",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.BeforeSettle = []poo.BeforeSettleHook{&continuingBeforeSettle{}}
				opts.MaxContinuations = 1
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "tool_fatal",
			want:   poo.ReasonToolFatal,
			source: "tool:echo",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.Transport = scripted([]fakeResponse{{
					calls: []poo.ToolCall{{
						ID:        "call_1",
						Name:      "echo",
						Arguments: []byte(`{"text":"hello"}`),
					}},
				}})
				opts.Tools["echo"] = &fatalTool{}
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "context_exhausted",
			want:   poo.ReasonContextExhausted,
			source: "no_output_room",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.ContextLimit = 300
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "provider_error",
			want:   poo.ReasonProviderError,
			source: "provider:server",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.Transport = scripted([]fakeResponse{{
					err: &poo.ProviderError{Kind: poo.ProviderKindServer, Message: "the endpoint is down"},
				}})
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "provider_limit",
			want:   poo.ReasonProviderLimit,
			source: "provider:usage_limit",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.Transport = scripted([]fakeResponse{{
					err: &poo.ProviderError{Kind: poo.ProviderKindUsageLimit, Message: "you are out of credits"},
				}})
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "cancelled",
			want:   poo.ReasonCancelled,
			source: "cancel:test",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.Transport = newStallingTransport()
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {
				s.Cancel("cancel:test")
			},
		},
		{
			name:   "store_error",
			want:   poo.ReasonStoreError,
			source: "store",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				// The sink refuses the run's settle record, so the
				// outcome cannot be stored: the reason changes to
				// store_error.
				opts.Sink = &failingSink{failRecord: "run.settled"}
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
		{
			name:   "finished",
			want:   poo.ReasonFinished,
			source: "finish",
			build: func(t *testing.T, opts *poo.Options) *poo.Session {
				opts.Transport = scripted([]fakeResponse{{
					calls: []poo.ToolCall{{
						ID:        "call_1",
						Name:      "finish",
						Arguments: []byte(`{"result":{"answer":"42"}}`),
					}},
				}})
				opts.Tools["finish"] = newFinishTool(t, `{"type":"object"}`)
				opts.ToolOrder = []string{"echo", "finish"}
				return newTestSession(t, *opts)
			},
			act: func(t *testing.T, s *poo.Session) {},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := scripted([]fakeResponse{{text: "the answer"}})
			sink := &recordingSink{}
			tool := &echoTool{result: "echoed"}
			opts := testOptions(t, tr, tool, sink)
			s := c.build(t, &opts)

			if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			c.act(t, s)
			res, err := s.Wait(context.Background())
			if err != nil {
				t.Fatalf("waiting for the run: %v", err)
			}
			if res.Reason != c.want {
				t.Fatalf("the run settled %q, want %q", res.Reason, c.want)
			}
			if res.Detail.Source != c.source {
				t.Errorf("the settle source is %q, want %q", res.Detail.Source, c.source)
			}
			// The settle record says the same thing, unless the
			// store refused it.
			if settle := sink.settled(); settle != nil {
				if settle.Reason != c.want && c.want != poo.ReasonStoreError {
					t.Errorf("the settle record says %q, want %q", settle.Reason, c.want)
				}
			} else if c.want != poo.ReasonStoreError {
				t.Fatal("the log holds no settle record")
			}
			// The exit code follows the plan's table.
			if got := poo.ExitCode(res.Reason); got < 0 {
				t.Errorf("the exit code is %d, want a plan code", got)
			}
		})
	}
}

// plainSession builds a session that answers with one text response.
func plainSession(t *testing.T, opts *poo.Options) *poo.Session {
	t.Helper()
	opts.Transport = scripted([]fakeResponse{{text: "the answer"}})
	return newTestSession(t, *opts)
}

// fatalTool is a tool that fails with a fatal error.
type fatalTool struct {
	echoTool
}

// Call fails fatally.
func (t *fatalTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{}, poo.Fatalf("the working directory is gone")
}

// TestTheInputEventsAreEmitted covers the plan's event list: the input
// records have their events.
func TestTheInputEventsAreEmitted(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the answer"},
	})
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	if _, err := s.Send("and this", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	hold.finish()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	queued := 0
	delivered := 0
	for _, ev := range s.Events() {
		switch ev.Name {
		case poo.EventInputQueued:
			queued++
		case poo.EventInputDelivered:
			delivered++
		}
	}
	if queued != 1 {
		t.Errorf("the run emitted %d input.queued events, want 1", queued)
	}
	if delivered != 1 {
		t.Errorf("the run emitted %d input.delivered events, want 1", delivered)
	}
}

// TestTheRunAndTurnEventsAreEmitted covers the plan's event list for
// the run and the turn.
func TestTheRunAndTurnEventsAreEmitted(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the answer"},
	})
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	runSession(t, s, "do the thing")

	want := map[poo.EventName]int{
		poo.EventRunStarted:  1,
		poo.EventRunSettling: 1,
		poo.EventRunSettled:  1,
		poo.EventTurnStarted: 2,
		poo.EventTurnEnded:   2,
		poo.EventToolCall:    1,
		poo.EventToolResult:  1,
	}
	got := map[poo.EventName]int{}
	for _, ev := range s.Events() {
		got[ev.Name]++
	}
	for name, n := range want {
		if got[name] != n {
			t.Errorf("the run emitted %d %s events, want %d", got[name], name, n)
		}
	}
}

// TestTheUsageEventIsEmitted covers the plan's event list for the
// accounting.
func TestTheUsageEventIsEmitted(t *testing.T) {
	tr := scripted([]fakeResponse{{
		text:  "the answer",
		usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 5},
	}})
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	runSession(t, s, "do the thing")

	found := false
	for _, ev := range s.Events() {
		if ev.Name == poo.EventUsage {
			found = true
		}
	}
	if !found {
		t.Error("the run emitted no usage event")
	}
}
