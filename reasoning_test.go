package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The session's reasoning level is the session's, not the model's: the
// requests that follow the level send it, and a level the model does not
// take is refused before a request is made.

// thinkingModel is a model that takes the three levels its endpoint knows.
func thinkingModel(name string) poo.ModelInfo {
	info := chatModel(name, 32768)
	info.Reasoning = poo.ReasoningControl{Mode: poo.ReasoningEffort, Param: poo.ReasoningPathEffort}
	info.ReasoningLevels = []string{"off", "low", "high"}
	return info
}

// TestTheReasoningLevelTravelsWithTheRequests covers the level: the run's
// request carries it, and a level set between the runs is the one the next
// request carries.
func TestTheReasoningLevelTravelsWithTheRequests(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "one"}, {text: "two"}})
	s := newTestSession(t, poo.Options{
		Transport:  tr,
		Sink:       &recordingSink{},
		SessionID:  "s1",
		Model:      thinkingModel("m"),
		WorkingDir: t.TempDir(),
	})

	if err := s.SetReasoning("high"); err != nil {
		t.Fatalf("setting the level: %v", err)
	}
	if got := s.ReasoningLevel(); got != "high" {
		t.Errorf("the session runs at %q, want high", got)
	}
	runSession(t, s, "one")

	// A level named between the runs is the one the next run sends.
	if err := s.SetReasoning("Low"); err != nil {
		t.Fatalf("setting the level: %v", err)
	}
	if got := s.ReasoningLevel(); got != "low" {
		t.Errorf("the session runs at %q, want the level spelled low", got)
	}
	runSession(t, s, "two")

	requests := tr.Requests()
	if len(requests) < 2 {
		t.Fatalf("the transport got %d requests, want two", len(requests))
	}
	if requests[0].Reasoning != "high" {
		t.Errorf("the first request carries the level %q, want high", requests[0].Reasoning)
	}
	if requests[len(requests)-1].Reasoning != "low" {
		t.Errorf("the last request carries the level %q, want low", requests[len(requests)-1].Reasoning)
	}
}

// TestAReasoningLevelTheModelDoesNotTakeIsRefused covers the check: a level
// outside the model's own is refused with the levels it does take, and the
// session keeps the level it had.
func TestAReasoningLevelTheModelDoesNotTakeIsRefused(t *testing.T) {
	s := newTestSession(t, poo.Options{
		Transport:  scripted([]fakeResponse{{text: "one"}}),
		SessionID:  "s1",
		Model:      thinkingModel("m"),
		WorkingDir: t.TempDir(),
	})
	if err := s.SetReasoning("low"); err != nil {
		t.Fatalf("setting the level: %v", err)
	}

	err := s.SetReasoning("minimal")
	if err == nil {
		t.Fatal("the level was taken, want the model's own levels to refuse it")
	}
	if !strings.Contains(err.Error(), "off, low, high") {
		t.Errorf("the refusal is %q, want it to name the model's levels", err)
	}
	// The refusal a client sees is the session API's, not the settings'
	// configuration error: SetReasoning re-maps it, and a regression that
	// stopped doing so would leak a ConfigError to callers.
	var invalid *poo.ErrInvalidInput
	if !errors.As(err, &invalid) {
		t.Errorf("the refusal is %T, want an ErrInvalidInput", err)
	}
	if got := s.ReasoningLevel(); got != "low" {
		t.Errorf("the refused level left the session at %q, want low", got)
	}
}

// TestAModelThatCannotThinkTakesNoLevel covers the model without a
// reasoning control: it takes no level but off, and it is cycled through
// no level at all.
func TestAModelThatCannotThinkTakesNoLevel(t *testing.T) {
	s := newTestSession(t, poo.Options{
		Transport:  scripted([]fakeResponse{{text: "one"}}),
		SessionID:  "s1",
		Model:      chatModel("plain", 32768),
		WorkingDir: t.TempDir(),
	})

	err := s.SetReasoning("low")
	if err == nil {
		t.Fatal("the model took a level, want a refusal")
	}
	if !strings.Contains(err.Error(), "does not control its reasoning") {
		t.Errorf("the refusal is %q, want it to name the reason", err)
	}
	if err := s.SetReasoning("off"); err != nil {
		t.Errorf("the model refused to be told off: %v", err)
	}
}

// TestASwitchTakesTheLevelBack covers the switch: a level the new model
// does not take leaves the session at one the new model does, and a model
// that cannot be told off takes it to the lowest level it declares.
func TestASwitchTakesTheLevelBack(t *testing.T) {
	narrow := chatModel("narrow", 32768)
	narrow.Reasoning = poo.ReasoningControl{Mode: poo.ReasoningEffort, Param: poo.ReasoningPathEffort}
	narrow.ReasoningLevels = []string{"medium", "high"}

	// A model that can be told off but not told to think hard.
	shallow := chatModel("shallow", 32768)
	shallow.Reasoning = poo.ReasoningControl{Mode: poo.ReasoningEffort, Param: poo.ReasoningPathEffort}
	shallow.ReasoningLevels = []string{"off", "medium"}

	plain := chatModel("plain", 32768)

	cases := []struct {
		name  string
		to    poo.ModelInfo
		want  string
		level string
	}{
		{name: "a model that takes off", to: shallow, level: "high", want: "off"},
		{name: "a model that cannot be told off", to: narrow, level: "low", want: "medium"},
		{name: "a model that controls no reasoning", to: plain, level: "high", want: ""},
		{name: "a level the new model takes stays", to: thinkingModel("other"), level: "low", want: "low"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			s := newTestSession(t, poo.Options{
				Transport:  scripted([]fakeResponse{{text: "one"}}),
				SessionID:  "s1",
				Model:      thinkingModel("m"),
				WorkingDir: t.TempDir(),
			})
			if err := s.SetReasoning(test.level); err != nil {
				t.Fatalf("setting the level: %v", err)
			}
			if _, err := s.SetModel(test.to); err != nil {
				t.Fatalf("switching the model: %v", err)
			}
			if got := s.ReasoningLevel(); got != test.want {
				t.Errorf("the session runs at %q after the switch, want %q", got, test.want)
			}
		})
	}
}

// TestTheLevelIsEmptyUntilItIsSet covers the session that never named a
// level: it sends none, and the request carries none.
func TestTheLevelIsEmptyUntilItIsSet(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "one"}})
	s := newTestSession(t, poo.Options{
		Transport:  tr,
		SessionID:  "s1",
		Model:      thinkingModel("m"),
		WorkingDir: t.TempDir(),
	})
	if got := s.ReasoningLevel(); got != "" {
		t.Errorf("the session runs at %q, want no level", got)
	}
	runSession(t, s, "one")
	if got := tr.Requests()[0].Reasoning; got != "" {
		t.Errorf("the request carries the level %q, want none", got)
	}
}

// TestTheReasoningSettingReachesTheTransportWhenItIsAnOption covers the
// option: a caller that names the level on the session's options, as the
// one-shot does with its flag, sends it.
func TestTheReasoningSettingReachesTheTransportWhenItIsAnOption(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "one"}})
	s := newTestSession(t, poo.Options{
		Transport:       tr,
		SessionID:       "s1",
		Model:           thinkingModel("m"),
		ReasoningEffort: "high",
		WorkingDir:      t.TempDir(),
	})
	runSession(t, s, "one")
	if got := tr.Requests()[0].Reasoning; got != "high" {
		t.Errorf("the request carries the level %q, want high", got)
	}
}

// TestTheSessionRefusesAReasoningLevelWhileARunIsActive covers the timing:
// a level takes effect between the runs, so every request of a run carries
// the level the run started at.
func TestTheSessionRefusesAReasoningLevelWhileARunIsActive(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	tool := &echoTool{call: func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-release
		return poo.ToolResult{Text: "done"}, nil
	}}
	tr := pootest.NewTransport(pootest.Turn{
		Text:  "working",
		Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`"x"`), ID: "c1"}},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Model = thinkingModel("m")
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	err := s.SetReasoning("high")
	close(release)
	waitRun(t, s)
	if !errors.Is(err, poo.ErrBusy) {
		t.Errorf("the level change failed with %v, want it to be refused", err)
	}
	if got := s.ReasoningLevel(); got != "" {
		t.Errorf("the refused level left the session at %q, want no level", got)
	}
}

// TestAReasoningSettingOnASpentSessionIsRefused covers the guards a
// closed and a broken session make before a level is set: neither
// accepts a level, whatever the level is.
func TestAReasoningSettingOnASpentSessionIsRefused(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "one"}})
	s := newTestSession(t, poo.Options{
		Transport:  tr,
		Sink:       &recordingSink{},
		SessionID:  "s1",
		Model:      thinkingModel("m"),
		WorkingDir: t.TempDir(),
	})
	if err := s.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	if err := s.SetReasoning("high"); !errors.Is(err, poo.ErrSessionClosed) {
		t.Errorf("a closed session took the level %v, want ErrSessionClosed", err)
	}
}
