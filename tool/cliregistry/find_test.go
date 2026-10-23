package cliregistry_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// searcher is a decision model that answers by entry name.
//
// It records the batches it is asked, so a test can see how a search is
// split, and it can fail on demand.
type searcher struct {
	mu sync.Mutex
	// calls holds the questions of every request, in the order they
	// arrived.
	calls [][]decide.Question
	// err is the failure the searcher returns, when it is set.
	err error
	// probability is the probability the searcher answers for a name.
	probability func(name string) float64
}

// Decide answers every question of a batch.
func (s *searcher) Decide(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
	s.mu.Lock()
	s.calls = append(s.calls, qs)
	err := s.err
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	answers := make([]decide.Answer, 0, len(qs))
	for _, q := range qs {
		p := 0.5
		if s.probability != nil {
			p = s.probability(q.ID)
		}
		answers = append(answers, decide.Answer{
			ID:            q.ID,
			Probabilities: map[string]float64{"yes": p, "no": 1 - p},
		})
	}
	return answers, nil
}

// batches returns the questions of every request.
func (s *searcher) batches() [][]decide.Question {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]decide.Question(nil), s.calls...)
}

// found is the search's structured result.
type found struct {
	Total   int `json:"total"`
	Results []struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Probability float64        `json:"probability"`
		Schema      map[string]any `json:"schema"`
	} `json:"results"`
}

// TestFindToolAsksFourRequests covers the search over a large catalog:
// one question per entry, in batches of at most sixty-four, with the
// best entries first.
func TestFindToolAsksFourRequests(t *testing.T) {
	catalog := wideCatalog(t)
	tool := cliregistry.NewFind(catalog)
	model := &searcher{probability: indexProbability}

	res, err := preparedCall(t, tool, `{"intent":"print the words I give it"}`, poo.ToolEnv{Decider: model})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}

	// Two hundred entries in batches of sixty-four: four requests.
	calls := model.batches()
	if len(calls) != 4 {
		t.Fatalf("the search made %d requests, want 4", len(calls))
	}
	total := 0
	for i, batch := range calls {
		if len(batch) > decide.MaxQuestionsPerRequest {
			t.Errorf("the request %d holds %d questions, the most is %d", i, len(batch), decide.MaxQuestionsPerRequest)
		}
		total += len(batch)
	}
	if total != catalog.Len() {
		t.Errorf("the search asked %d questions for %d entries", total, catalog.Len())
	}

	// The best eight, best first.
	var body found
	if err := json.Unmarshal(res.Structured, &body); err != nil {
		t.Fatalf("decoding the result %q: %v", res.Structured, err)
	}
	if body.Total != catalog.Len() {
		t.Errorf("the result says %d entries, want %d", body.Total, catalog.Len())
	}
	if len(body.Results) != cliregistry.DefaultTopK {
		t.Fatalf("the search returned %d entries, want %d", len(body.Results), cliregistry.DefaultTopK)
	}
	for i := 1; i < len(body.Results); i++ {
		if body.Results[i-1].Probability < body.Results[i].Probability {
			t.Fatalf("the entries are not ordered by p(yes): %v", body.Results)
		}
	}
	// tool-199 has the highest probability, so it leads the list, and
	// its schema comes with it.
	if got := body.Results[0].Name; got != wideName(199) {
		t.Errorf("the first entry is %q, want %q", got, wideName(199))
	}
	if body.Results[0].Schema == nil {
		t.Error("the first entry carries no argument schema")
	}
	// The text the model reads lists the entries in the same order.
	first := strings.Index(res.Text, wideName(199))
	second := strings.Index(res.Text, wideName(198))
	if first < 0 || second < 0 || first > second {
		t.Errorf("the text %q does not list the best entries first", res.Text)
	}
	if !strings.Contains(res.Text, "args: ") {
		t.Errorf("the text %q does not carry the argument schemas", res.Text)
	}
}

// TestFindToolFailsRecoverably covers a search the decision model could
// not answer: the model is told the search is unavailable and how to go
// on, and no list of entries takes its place.
func TestFindToolFailsRecoverably(t *testing.T) {
	tool := cliregistry.NewFind(wideCatalog(t))
	model := &searcher{err: errors.New("the decision model is down")}

	res, err := preparedCall(t, tool, `{"intent":"print something"}`, poo.ToolEnv{Decider: model})
	if err == nil {
		t.Fatalf("the search returned %q, want a failure", res.Text)
	}
	if !strings.Contains(err.Error(), cliregistry.UnavailableText) {
		t.Errorf("the failure is %q, want %q", err, cliregistry.UnavailableText)
	}
	if strings.Contains(res.Text, wideName(0)) {
		t.Errorf("the failure carries a fallback list: %q", res.Text)
	}
}

// TestFindToolNeedsADecider covers a session that offers the search
// without a decision model: the search says so rather than answering
// with a made-up ranking.
func TestFindToolNeedsADecider(t *testing.T) {
	tool := cliregistry.NewFind(fixtureCatalog(t))
	_, err := preparedCall(t, tool, `{"intent":"say hello"}`, poo.ToolEnv{})
	if err == nil {
		t.Fatal("a search without a decision model answered")
	}
	if !strings.Contains(err.Error(), cliregistry.UnavailableText) {
		t.Errorf("the failure is %q, want %q", err, cliregistry.UnavailableText)
	}
}

// TestFindToolRefusesAnEmptyIntent covers the arguments.
func TestFindToolRefusesAnEmptyIntent(t *testing.T) {
	tool := cliregistry.NewFind(fixtureCatalog(t))
	for _, args := range []string{`{"intent":""}`, `{"intent":"  "}`, `{}`, `{"intent":5}`, `{"nope":"x"}`} {
		if _, err := tool.Prepare(json.RawMessage(args), poo.PrepareEnv{}); err == nil {
			t.Errorf("%s prepared a search", args)
		}
	}
}

// TestCallStillWorksForANameTheSearchMissed covers the pair: a search
// narrows the catalog, and it does not gate the call. An entry the search
// did not return is still callable.
func TestCallStillWorksForANameTheSearchMissed(t *testing.T) {
	catalog := wideCatalog(t)
	find := cliregistry.NewFind(catalog)
	model := &searcher{probability: indexProbability}
	res, err := preparedCall(t, find, `{"intent":"print the words"}`, poo.ToolEnv{Decider: model})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if strings.Contains(res.Text, wideName(0)+":") {
		t.Fatalf("the search returned %q, so the test proves nothing", wideName(0))
	}

	callTool := cliregistry.NewCall(catalog)
	prepared, err := callTool.Prepare(json.RawMessage(fmt.Sprintf(`{"name":%q,"args":{}}`, wideName(0))), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing an entry the search did not return: %v", err)
	}
	called, err := callTool.Call(context.Background(), prepared, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("calling an entry the search did not return: %v", err)
	}
	if called.IsError {
		t.Errorf("the call failed: %q", called.Text)
	}
}

// preparedCall validates a find_tool call and runs it.
func preparedCall(t *testing.T, tool *cliregistry.FindTool, args string, env poo.ToolEnv) (poo.ToolResult, error) {
	t.Helper()
	prepared, err := tool.Prepare(json.RawMessage(args), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing %s: %v", args, err)
	}
	return tool.Call(context.Background(), prepared, env)
}

// indexProbability makes the later entries fit better, so the ranking is
// predictable from the name.
func indexProbability(name string) float64 {
	index, err := strconv.Atoi(strings.TrimPrefix(name, "tool-"))
	if err != nil {
		return 0.5
	}
	return float64(index) / 199
}
