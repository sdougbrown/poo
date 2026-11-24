package history_test

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/history"
)

// The history tool: a read over the turns the session's store retains,
// whole or by search.

// history is a fixed history a test answers with.
type fixedHistory struct{}

// Turns returns the turn numbers the records retain.
func (fixedHistory) Turns(context.Context) ([]int, error) { return []int{1, 2}, nil }

// Turn returns one turn's text.
func (fixedHistory) Turn(_ context.Context, turn int) (string, error) {
	if turn == 1 {
		return "[user] fix the panic in main.go\n[assistant] reading main.go", nil
	}
	return "[assistant] the panic is fixed", nil
}

// Search returns the retained lines that match.
func (fixedHistory) Search(_ context.Context, pattern string) ([]poo.HistoryMatch, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	var out []poo.HistoryMatch
	for _, m := range []poo.HistoryMatch{
		{Turn: 1, Text: "[user] fix the panic in main.go"},
		{Turn: 1, Text: "[assistant] reading main.go"},
		{Turn: 2, Text: "[assistant] the panic is fixed"},
	} {
		if re.MatchString(m.Text) {
			out = append(out, m)
		}
	}
	return out, nil
}

func call(t *testing.T, h poo.History, raw string) poo.ToolResult {
	t.Helper()
	tool := history.New()
	prepared, err := tool.Prepare(json.RawMessage(raw), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing %s: %v", raw, err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{History: h})
	if err != nil {
		t.Fatalf("calling %s: %v", raw, err)
	}
	return res
}

// TestATurnReadHoldsTheWholeText covers the turn read: the text comes
// back as it was said, under the turn's own number.
func TestATurnReadHoldsTheWholeText(t *testing.T) {
	res := call(t, fixedHistory{}, `{"turn":1}`)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	for _, want := range []string{"turn 1", "fix the panic in main.go", "reading main.go"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("the read is %q, want it to carry %q", res.Text, want)
		}
	}
}

// TestASearchNamesTheTurnEachMatchCameFrom covers the search: the
// matches name their turns, and an empty match says so.
func TestASearchNamesTheTurnEachMatchCameFrom(t *testing.T) {
	res := call(t, fixedHistory{}, `{"pattern":"panic"}`)
	if strings.Count(res.Text, "turn ") != 2 {
		t.Errorf("the search is %q, want two named matches", res.Text)
	}
	res = call(t, fixedHistory{}, `{"pattern":"deploy"}`)
	if !strings.Contains(res.Text, "no retained line matches") {
		t.Errorf("the search is %q, want the empty answer", res.Text)
	}
}

// TestTheToolRefusesACallThatReadsAndSearches covers the one question
// per call rule: a call that names a turn and a pattern is refused, and
// so is a call that names neither.
func TestTheToolRefusesACallThatReadsAndSearches(t *testing.T) {
	tool := history.New()
	if _, err := tool.Prepare(json.RawMessage(`{"turn":1,"pattern":"x"}`), poo.PrepareEnv{}); err == nil {
		t.Error("a call that reads and searches was accepted")
	}
	if _, err := tool.Prepare(json.RawMessage(`{}`), poo.PrepareEnv{}); err == nil {
		t.Error("a call that names neither was accepted")
	}
	if _, err := tool.Prepare(json.RawMessage(`{"turn":0}`), poo.PrepareEnv{}); err == nil {
		t.Error("turn 0 was accepted")
	}
	if _, err := tool.Prepare(json.RawMessage(`{"pattern":"("}`), poo.PrepareEnv{}); err == nil {
		t.Error("an invalid pattern was accepted")
	}
}

// TestTheToolSaysWhenTheStoreCannotBeRead covers the seam's absence: a
// tool call whose environment holds no history says so, and the run
// carries on.
func TestTheToolSaysWhenTheStoreCannotBeRead(t *testing.T) {
	res := call(t, nil, `{"turn":1}`)
	if !res.IsError || !strings.Contains(res.Text, "cannot be read") {
		t.Errorf("the read is %+v, want the no-history answer", res)
	}
}

// TestTheSearchTruncatesAtTheObservationCap covers the cap: a search
// whose matches overflow the settings' observation cap returns an
// answer bounded at the cap, naming the truncation.
func TestTheSearchTruncatesAtTheObservationCap(t *testing.T) {
	tool := history.New()
	prepared, err := tool.Prepare(json.RawMessage(`{"pattern":"y"}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	lines := make([]poo.HistoryMatch, 0, 40)
	for i := 0; i < 40; i++ {
		lines = append(lines, poo.HistoryMatch{Turn: i + 1, Text: strings.Repeat("y", 3000)})
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		History:  cappedHistory{lines: lines},
		Settings: poo.ToolSettings{ObservationCapChars: 20000},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if len(res.Text) > 20000+200 {
		t.Errorf("the answer is %d bytes, want it at the observation cap", len(res.Text))
	}
	if !strings.Contains(res.Text, "truncated at the observation cap") {
		t.Errorf("the answer does not say it was truncated: %q", res.Text[len(res.Text)-120:])
	}
}

// cappedHistory is a history that answers a search with the lines it
// was built with.
type cappedHistory struct {
	lines []poo.HistoryMatch
}

func (cappedHistory) Turns(context.Context) ([]int, error) { return nil, nil }

func (cappedHistory) Turn(_ context.Context, turn int) (string, error) {
	return "", nil
}

func (c cappedHistory) Search(_ context.Context, pattern string) ([]poo.HistoryMatch, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	var out []poo.HistoryMatch
	for _, m := range c.lines {
		if re.MatchString(m.Text) {
			out = append(out, m)
		}
	}
	return out, nil
}
