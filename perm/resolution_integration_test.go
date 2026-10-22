package perm_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

type sol2ResetPolicy struct {
	p                             *perm.Policy
	third, fourth, release, reset chan struct{}
}

func (p *sol2ResetPolicy) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	switch info.Handle {
	case "r3":
		close(p.third)
		<-p.release
		d, e := p.p.BeforeTool(ctx, info)
		close(p.reset)
		return d, e
	case "r4":
		close(p.fourth)
		<-p.reset
	}
	return p.p.BeforeTool(ctx, info)
}
func TestParallelPermissionResetUsesModelOrder(t *testing.T) {
	p := reviewPolicy(t, perm.Options{DoomLoop: perm.DoomLoop{Action: perm.ActionAllow}})
	wrapper := &sol2ResetPolicy{p: p, third: make(chan struct{}), fourth: make(chan struct{}), release: make(chan struct{}), reset: make(chan struct{})}
	tool := &readTool{}
	tr := pootest.NewTransport(pootest.Turn{Calls: []pootest.Call{readCall("1", "a", 0), readCall("2", "a", 0), readCall("3", "a", 0), readCall("4", "a", 0)}}, pootest.Turn{Calls: []pootest.Call{readCall("5", "a", 0)}}, pootest.Turn{Text: "done"})
	sink := pootest.NewSink()
	s, err := poo.New(poo.Options{Transport: tr, Tools: map[string]poo.Tool{"read": tool}, ToolOrder: []string{"read"}, Sink: sink, Model: poo.ModelInfo{ModelID: "test", ContextLimit: 32768}}, poo.WithPolicy(wrapper))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-wrapper.third
	select {
	case <-wrapper.fourth:
	case <-time.After(time.Millisecond):
	}
	close(wrapper.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = s.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	answers := 0
	for _, e := range s.Events() {
		if e.Name == poo.EventPermissionAnswered {
			answers++
		}
	}
	if answers != 1 || s.RepeatCountsFor("r5").Exact != 2 {
		t.Fatalf("4 parallel reads + one next turn: doom-loop answers=%d, fourth=%+v fifth=%+v; want one answer, fourth=1 fifth=2", answers, s.RepeatCountsFor("r4"), s.RepeatCountsFor("r5"))
	}
}

type sol2MixedTool struct{ readTool }
type sol2MixedPrepared struct {
	Version int `json:"version"`
	path    string
}

func (p sol2MixedPrepared) Summary() string { return p.path }
func (p sol2MixedPrepared) CanonicalKey() json.RawMessage {
	key, _ := json.Marshal(map[string]string{"path": p.path})
	return key
}
func (*sol2MixedTool) Prepare(raw json.RawMessage, _ poo.PrepareEnv) (poo.Prepared, error) {
	var a struct{ Path string }
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	return sol2MixedPrepared{Version: 1, path: a.Path}, nil
}
func TestCanonicalKeyWithExportedMetadataKeepsCallsDistinct(t *testing.T) {
	p := reviewPolicy(t, perm.Options{DoomLoop: perm.DoomLoop{Action: perm.ActionDeny}})
	tool := &sol2MixedTool{}
	s := newSession(t, []pootest.Turn{{Calls: []pootest.Call{readCall("1", "a", 0), readCall("2", "b", 0), readCall("3", "c", 0)}}, {Text: "done"}}, map[string]poo.Tool{"read": tool}, []string{"read"}, p)
	s.run(t)
	if tool.Calls() != 3 {
		t.Fatalf("different CanonicalKey paths a,b,c: executed=%d, third counts=%+v, result=%q; want 3 executions", tool.Calls(), s.session.RepeatCountsFor("r3"), s.lastResult(t))
	}
}
