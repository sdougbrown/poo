package poo_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/prompt"
)

// The start hooks run at the start of a session and at the start of
// every run. They may append to the history and change the system
// prompt, and the prompt they change is the one the run sends.

// recordingStartHook is a session-start or before-run hook that records
// what it was asked and answers with a fixed decision.
type recordingStartHook struct {
	mu sync.Mutex
	// calls counts the calls the hook saw.
	calls int
	// decision is what the hook answers.
	decision poo.StartDecision
	// triggers holds the triggers the hook was given, in order.
	triggers []poo.RunTrigger
	// histories holds the message counts the hook's view held, in
	// order.
	histories []int
	// err is the failure the hook returns, when it is set.
	err error
	// wait blocks the hook until the channel is closed.
	wait chan struct{}
}

// SessionStart records the call and answers.
func (h *recordingStartHook) SessionStart(ctx poo.HookContext) (poo.StartDecision, error) {
	h.record(ctx)
	return h.decision, h.err
}

// BeforeRun records the call and answers.
func (h *recordingStartHook) BeforeRun(ctx poo.HookContext, trigger poo.RunTrigger) (poo.StartDecision, error) {
	h.mu.Lock()
	h.triggers = append(h.triggers, trigger)
	h.mu.Unlock()
	h.record(ctx)
	return h.decision, h.err
}

// record counts the call and waits when the hook is told to.
func (h *recordingStartHook) record(ctx poo.HookContext) {
	h.mu.Lock()
	h.calls++
	h.histories = append(h.histories, len(ctx.History.Messages()))
	wait := h.wait
	h.mu.Unlock()
	if wait != nil {
		<-wait
	}
}

// Calls returns how many times the hook ran.
func (h *recordingStartHook) Calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

// Triggers returns the triggers the hook was given, in order.
func (h *recordingStartHook) Triggers() []poo.RunTrigger {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]poo.RunTrigger(nil), h.triggers...)
}

// Histories returns the message counts the hook's view held, in order.
func (h *recordingStartHook) Histories() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int(nil), h.histories...)
}

// TestTheSessionStartHookAppendsAndEditsThePrompt covers the plan's
// rule: a session-start hook may append to the history and change the
// system prompt, and the change is in the prompt the run sends.
func TestTheSessionStartHookAppendsAndEditsThePrompt(t *testing.T) {
	hook := &recordingStartHook{decision: poo.StartDecision{
		Append: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the session's context")},
		SystemPrompt: func(s prompt.Sections) {
			s.Set("base", "the edited prompt")
		},
	}}
	tr := scripted([]fakeResponse{{text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SessionStart = []poo.SessionStartHook{hook}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	requests := tr.Requests()
	if len(requests) == 0 {
		t.Fatal("the model was never asked")
	}
	messages := requests[0].Messages
	if got := systemText(messages); got != "the edited prompt\n" {
		t.Errorf("the system message is %q, want the edited prompt", got)
	}
	if !historyHas(messages, "the session's context") {
		t.Errorf("the model's view is %+v, want the hook's message in it", messages)
	}
	// The record says which hook put the message there.
	found := false
	for _, rec := range sink.messages() {
		for _, c := range rec.Content {
			if c.Text == "the session's context" && rec.Origin == poo.HookOrigin(hookNameOf(hook)) {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("the store holds no message from the session-start hook: %+v", sink.messages())
	}
}

// TestTheBeforeRunHookAppendsAndEditsThePrompt covers the plan's rule:
// a before-run hook may append to the history and change the system
// prompt of the run it fires in.
func TestTheBeforeRunHookAppendsAndEditsThePrompt(t *testing.T) {
	hook := &recordingStartHook{decision: poo.StartDecision{
		Append: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the run's context")},
		SystemPrompt: func(s prompt.Sections) {
			s.Set("base", "the run's prompt")
		},
	}}
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.BeforeRun = []poo.BeforeRunHook{hook}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	requests := tr.Requests()
	if len(requests) == 0 {
		t.Fatal("the model was never asked")
	}
	messages := requests[0].Messages
	if got := systemText(messages); got != "the run's prompt\n" {
		t.Errorf("the system message is %q, want the run's prompt", got)
	}
	if !historyHas(messages, "the run's context") {
		t.Errorf("the model's view is %+v, want the hook's message in it", messages)
	}
	if hook.Calls() != 1 {
		t.Errorf("the hook ran %d time(s), want 1", hook.Calls())
	}
}

// TestTheSessionStartHooksEditIsInEveryRunsPrompt covers the plan's
// rule: a change the session start made is still there in the next
// run's prompt.
func TestTheSessionStartHooksEditIsInEveryRunsPrompt(t *testing.T) {
	hook := &recordingStartHook{decision: poo.StartDecision{
		SystemPrompt: func(s prompt.Sections) {
			s.Set("base", "the session's prompt")
		},
	}}
	tr := scripted([]fakeResponse{{text: "done"}, {text: "done again"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.SessionStart = []poo.SessionStartHook{hook}
	s := newTestSession(t, opts)

	runSession(t, s, "first")
	runSession(t, s, "second")
	requests := tr.Requests()
	if len(requests) < 2 {
		t.Fatalf("the model was asked %d time(s), want 2", len(requests))
	}
	if got := systemText(requests[1].Messages); got != "the session's prompt\n" {
		t.Errorf("the second run's system message is %q, want the session's prompt", got)
	}
}

// TestTheBeforeRunHookSeesTheJobsThatWokeTheRun covers the plan's
// rule: a woken run's before-run hook is told what woke it, and the
// job results are already in the history.
func TestTheBeforeRunHookSeesTheJobsThatWokeTheRun(t *testing.T) {
	hook := &recordingStartHook{}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the woken answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")
	opts := jobOptions(t, tr, tool, sink)
	opts.BeforeRun = []poo.BeforeRunHook{hook}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the job to start", func() bool { return tool.Calls() == 1 })
	// The run is cancelled, and the job keeps running. The session is
	// idle when the job finishes, so the result wakes a run.
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	tool.ReleaseAll()
	waitFor(t, "the woken run to settle", func() bool {
		settled := sink.settled()
		return settled != nil && settled.RunID != ""
	})
	waitFor(t, "the woken run's hook to run", func() bool { return hook.Calls() >= 2 })
	waitFor(t, "the woken run's request", func() bool {
		for _, req := range tr.Requests() {
			if historyHas(req.Messages, "the job's output") {
				return true
			}
		}
		return false
	})

	triggers := hook.Triggers()
	if len(triggers) < 2 {
		t.Fatalf("the hook saw %d trigger(s), want 2", len(triggers))
	}
	woken := triggers[1]
	if woken.Kind != poo.TriggerJob {
		t.Errorf("the woken run's trigger is %q, want job", woken.Kind)
	}
	if len(woken.Jobs) != 1 {
		t.Fatalf("the woken run's trigger carries %d job(s), want 1", len(woken.Jobs))
	}
	if woken.Jobs[0].Text != "the job's output" {
		t.Errorf("the woken run's job result is %q, want the job's output", woken.Jobs[0].Text)
	}
	// The job result is already in the history when the hook runs: the
	// hook's view holds more messages than the first run's view did.
	histories := hook.Histories()
	if len(histories) < 2 {
		t.Fatalf("the hook saw %d view(s), want 2", len(histories))
	}
	if histories[1] <= histories[0] {
		t.Errorf("the woken run's view holds %d messages, want more than the first run's %d", histories[1], histories[0])
	}
	// The job results are ahead of the system message in the history,
	// and the model reads the system prompt first anyway.
	var wokenMessages []poo.Message
	for _, req := range tr.Requests() {
		if historyHas(req.Messages, "the job's output") {
			wokenMessages = req.Messages
			break
		}
	}
	if len(wokenMessages) == 0 {
		t.Fatal("no request carries the job result")
	}
	if wokenMessages[0].Role != poo.RoleSystem {
		t.Errorf("the woken run's request starts with %q, want the system message", wokenMessages[0].Role)
	}
}

// TestTheSessionStartHookThatMissesItsDeadlineFailsTheConstruction
// covers the plan's rule: a session start that misses its deadline
// leaves no session behind.
func TestTheSessionStartHookThatMissesItsDeadlineFailsTheConstruction(t *testing.T) {
	hook := &recordingStartHook{wait: make(chan struct{})}
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.SessionStart = []poo.SessionStartHook{hook}
	opts.HookTimeout = 30 * time.Millisecond

	_, err := poo.New(opts)
	if err == nil {
		t.Fatal("the session was built, want the construction to fail")
	}
	var init *poo.ErrSessionInit
	if !errors.As(err, &init) {
		t.Fatalf("the construction failed with %v, want an ErrSessionInit", err)
	}
	close(hook.wait)
}

// TestTheBeforeRunHookThatFailsSettlesTheRun covers the plan's rule: a
// hook that fails settles the run with hook_error.
func TestTheBeforeRunHookThatFailsSettlesTheRun(t *testing.T) {
	hook := &recordingStartHook{err: errors.New("the hook is broken")}
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.BeforeRun = []poo.BeforeRunHook{hook}
	s := newTestSession(t, opts)

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonHookError {
		t.Fatalf("the run settled %q, want hook_error", res.Reason)
	}
	if res.Detail.Source != "before_run:recordingStartHook" {
		t.Errorf("the settle detail's source is %q, want the hook's name", res.Detail.Source)
	}
}

// systemText returns the text of the system message in a view.
func systemText(messages []poo.Message) string {
	for _, m := range messages {
		if m.Role == poo.RoleSystem {
			return m.Text()
		}
	}
	return ""
}

// historyHas reports whether a view holds a text.
//
// Every content block counts, not only the text ones: a tool result
// carries its text in a block of its own.
func historyHas(messages []poo.Message, text string) bool {
	for _, m := range messages {
		if strings.Contains(m.Text(), text) {
			return true
		}
		for _, c := range m.Content {
			if strings.Contains(c.Text, text) {
				return true
			}
		}
	}
	return false
}

// hookNameOf names a hook the way the core names it.
func hookNameOf(h any) string {
	type named interface{ HookName() string }
	if n, ok := h.(named); ok {
		return n.HookName()
	}
	return "recordingStartHook"
}
