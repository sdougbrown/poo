package cliregistry_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/provider/openai"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// searcher is a decision model that answers by entry name.
//
// It answers every question it is asked, and it can fail on demand.
type searcher struct {
	// err is the failure the searcher returns, when it is set.
	err error
	// probability is the probability the searcher answers for a name.
	probability func(name string) float64
}

// Decide answers every question.
func (s *searcher) Decide(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
	if s.err != nil {
		return nil, s.err
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

// systemOneServer is a decision endpoint that answers every question it
// is asked.
//
// It records how many questions each request carried, so a test can see
// how the decider split the search.
type systemOneServer struct {
	*httptest.Server
	mu sync.Mutex
	// sizes holds the number of questions of every request, in order.
	sizes []int
}

// newSystemOneServer starts the endpoint.
func newSystemOneServer(t *testing.T) *systemOneServer {
	t.Helper()
	s := &systemOneServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Questions map[string]json.RawMessage `json:"questions"`
		}
		_ = json.Unmarshal(raw, &req)
		s.mu.Lock()
		s.sizes = append(s.sizes, len(req.Questions))
		s.mu.Unlock()

		answers := map[string]any{}
		for id := range req.Questions {
			p := indexProbability(id)
			answers[id] = map[string]any{
				"probabilities": map[string]float64{"yes": p, "no": 1 - p},
			}
		}
		body, _ := json.Marshal(map[string]any{
			"object":  "structured_decision",
			"id":      "decision-1",
			"created": 1730000000,
			"answers": answers,
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

// requestSizes returns the number of questions of every request.
func (s *systemOneServer) requestSizes() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.sizes...)
}

// recordingSink keeps the decision records a search wrote.
type recordingSink struct {
	mu sync.Mutex
	// recs holds the records, in the order they arrived.
	recs []decide.Decision
}

// Record keeps one record.
func (s *recordingSink) Record(ctx context.Context, rec decide.Decision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = append(s.recs, rec)
	return nil
}

// records returns the records written.
func (s *recordingSink) records() []decide.Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]decide.Decision(nil), s.recs...)
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

// TestFindToolAsksFourRequests covers the split a large catalog needs:
// one question per entry, in requests of at most sixty-four, and the
// best entries first.
//
// The split belongs to the decision model, so the test asks a real
// systemone decider and counts the requests its endpoint receives.
func TestFindToolAsksFourRequests(t *testing.T) {
	catalog := wideCatalog(t)
	server := newSystemOneServer(t)
	model := openai.NewSystemOneDecider(openai.SystemOneConfig{
		BaseURL: server.URL,
		Model:   "test-model",
	})

	res, err := preparedCall(t, cliregistry.NewFind(catalog), `{"intent":"print the words I give it"}`, poo.ToolEnv{Decider: model})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}

	// Two hundred entries in requests of sixty-four: four requests.
	sizes := server.requestSizes()
	if len(sizes) != 4 {
		t.Fatalf("the search made %d requests, want 4", len(sizes))
	}
	total := 0
	for i, size := range sizes {
		if size > decide.MaxQuestionsPerRequest {
			t.Errorf("the request %d holds %d questions, the most is %d", i, size, decide.MaxQuestionsPerRequest)
		}
		total += size
	}
	if total != catalog.Len() {
		t.Errorf("the search asked %d questions for %d entries", total, catalog.Len())
	}
	// The decider's own count agrees: one call, four requests.
	if got := model.Requests(); got != len(sizes) {
		t.Errorf("the decider reports %d requests, and the endpoint saw %d", got, len(sizes))
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

// TestOneSearchWritesOneDecisionRecord covers the record: a search is one
// logical decision, so it leaves one record behind, and the record names
// every question the search asked.
func TestOneSearchWritesOneDecisionRecord(t *testing.T) {
	sink := &recordingSink{}
	model := decide.Recorded(&searcher{probability: indexProbability}, sink, "session")

	if _, err := preparedCall(t, cliregistry.NewFind(wideCatalog(t)), `{"intent":"print the words"}`, poo.ToolEnv{Decider: model}); err != nil {
		t.Fatalf("searching: %v", err)
	}

	records := sink.records()
	if len(records) != 1 {
		t.Fatalf("one search wrote %d decision records, want 1", len(records))
	}
	if got := len(records[0].Questions); got != 200 {
		t.Errorf("the record holds %d questions, want the catalog's 200", got)
	}
}

// malformedDecider answers every question, then breaks the answer set
// the way the kind names.
func malformedDecider(kind string) decide.Decider {
	return decide.DeciderFunc(func(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
		answers := make([]decide.Answer, 0, len(qs))
		for _, q := range qs {
			answers = append(answers, decide.Answer{ID: q.ID, Probabilities: map[string]float64{"yes": 0.5, "no": 0.5}})
		}
		if len(answers) == 0 {
			return answers, nil
		}
		switch kind {
		case "missing":
			return answers[:len(answers)-1], nil
		case "duplicate":
			answers[len(answers)-1] = answers[0]
		case "bad-distribution":
			answers[0].Probabilities = map[string]float64{"yes": 0.9, "no": 0.9}
		}
		return answers, nil
	})
}

// TestMalformedAnswersAreRecoverable covers the search's own answer check:
// answers that do not match the questions make the search fail the
// recoverable way, not return a list built from an answer set that does
// not add up.
func TestMalformedAnswersAreRecoverable(t *testing.T) {
	for _, kind := range []string{"missing", "duplicate", "bad-distribution"} {
		t.Run(kind, func(t *testing.T) {
			res, err := preparedCall(t, cliregistry.NewFind(fixtureCatalog(t)), `{"intent":"say hello"}`, poo.ToolEnv{Decider: malformedDecider(kind)})
			if err != nil || !res.IsError || res.Text != cliregistry.UnavailableText || len(res.Structured) != 0 {
				t.Fatalf("bad answers produced result=%+v err=%v", res, err)
			}
		})
	}
}

// TestMalformedAnswersKeepDiagnostic covers the record: the session's
// recording wrapper writes the reason the answers did not add up, so a
// search a decider answered badly is not recorded as a successful
// decision.
func TestMalformedAnswersKeepDiagnostic(t *testing.T) {
	sink := pootest.NewSink()
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{Name: cliregistry.FindToolName, Arguments: json.RawMessage(`{"intent":"say hello"}`)}}},
		pootest.Turn{Text: "recovered"},
	)
	session, err := poo.New(poo.Options{
		Transport: transport, Sink: sink, SessionID: "s1",
		Model:     poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		Tools:     map[string]poo.Tool{cliregistry.FindToolName: cliregistry.NewFind(fixtureCatalog(t))},
		ToolOrder: []string{cliregistry.FindToolName},
		Decider:   malformedDecider("bad-distribution"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Prompt(context.Background(), "find a tool"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := lastToolResult(t, transport); got != poo.HandleLine("r1", cliregistry.FindToolName)+"\n"+cliregistry.UnavailableText {
		t.Fatalf("wrong model result %q", got)
	}
	var records []*poo.DecisionRecord
	for _, record := range sink.Records() {
		if decision, ok := record.(*poo.DecisionRecord); ok {
			records = append(records, decision)
		}
	}
	if len(records) != 1 {
		t.Fatalf("got %d decision records, want 1", len(records))
	}
	if records[0].Error == "" || records[0].Outcome == decide.OutcomeOK {
		t.Fatalf("search failed but decision record says outcome=%q error=%q", records[0].Outcome, records[0].Error)
	}
}

// TestTheSearchRanksByProbability covers the ranking: the entries come
// back by the probability the model gave them, not by their names.
func TestTheSearchRanksByProbability(t *testing.T) {
	catalog := wideCatalog(t)
	scores := map[string]float64{wideName(3): 0.95, wideName(117): 0.85, wideName(42): 0.75}
	model := &searcher{probability: func(name string) float64 {
		if p, ok := scores[name]; ok {
			return p
		}
		return 0.1
	}}

	res, err := preparedCall(t, cliregistry.NewFind(catalog).WithTopK(3), `{"intent":"print the words"}`, poo.ToolEnv{Decider: model})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	var body found
	if err := json.Unmarshal(res.Structured, &body); err != nil {
		t.Fatalf("decoding the result %q: %v", res.Structured, err)
	}
	want := []string{wideName(3), wideName(117), wideName(42)}
	if len(body.Results) != len(want) {
		t.Fatalf("the search returned %d entries, want %d", len(body.Results), len(want))
	}
	for i, name := range want {
		if body.Results[i].Name != name || body.Results[i].Probability != scores[name] {
			t.Errorf("the entry %d is %q at %v, want %q at %v",
				i, body.Results[i].Name, body.Results[i].Probability, name, scores[name])
		}
		entry, ok := catalog.Lookup(name)
		if !ok {
			t.Fatalf("the catalog has no entry named %q", name)
		}
		if body.Results[i].Description != entry.Description {
			t.Errorf("the entry %q carries the description %q, want %q",
				name, body.Results[i].Description, entry.Description)
		}
		if !reflect.DeepEqual(body.Results[i].Schema, entry.Schema) {
			t.Errorf("the entry %q carries the schema %v, want %v",
				name, body.Results[i].Schema, entry.Schema)
		}
	}
}

// TestFindToolFailsRecoverably covers a search the decision model could
// not answer: the model is told the search is unavailable and how to go
// on, in exactly those words, and no list of entries takes its place.
func TestFindToolFailsRecoverably(t *testing.T) {
	tool := cliregistry.NewFind(wideCatalog(t))
	model := &searcher{err: errors.New("the decision model is down")}

	res, err := preparedCall(t, tool, `{"intent":"print something"}`, poo.ToolEnv{Decider: model})
	if err != nil {
		t.Fatalf("the failure came back as an error: %v", err)
	}
	if !res.IsError {
		t.Error("the failure is not an error result")
	}
	if res.Text != cliregistry.UnavailableText {
		t.Errorf("the model reads %q, want %q", res.Text, cliregistry.UnavailableText)
	}
	if strings.Contains(res.Text, wideName(0)) || strings.Contains(string(res.Structured), wideName(0)) {
		t.Errorf("the failure carries a fallback list: %q", res.Text)
	}
}

// TestFindToolNeedsADecider covers a session that offers the search
// without a decision model: the search says so rather than answering
// with a made-up ranking.
func TestFindToolNeedsADecider(t *testing.T) {
	tool := cliregistry.NewFind(fixtureCatalog(t))
	res, err := preparedCall(t, tool, `{"intent":"say hello"}`, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("the failure came back as an error: %v", err)
	}
	if !res.IsError || res.Text != cliregistry.UnavailableText {
		t.Errorf("the model reads %q (an error: %v), want %q", res.Text, res.IsError, cliregistry.UnavailableText)
	}
}

// TestAFailedSearchIsNotFatal covers the session: the failure a search
// reports cannot settle a run, even when the decider's own error would
// have. The model reads the search's text and the run goes on.
func TestAFailedSearchIsNotFatal(t *testing.T) {
	model := &searcher{err: fmt.Errorf("%w: %w", decide.ErrDeferred, poo.ErrFatal)}
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{
			Name:      cliregistry.FindToolName,
			Arguments: json.RawMessage(`{"intent":"print the words"}`),
		}}},
		pootest.Turn{Text: "recovered"},
	)
	session, err := poo.New(poo.Options{
		Transport: transport,
		Tools:     map[string]poo.Tool{cliregistry.FindToolName: cliregistry.NewFind(wideCatalog(t))},
		ToolOrder: []string{cliregistry.FindToolName},
		Sink:      pootest.NewSink(),
		SessionID: "s1",
		Model:     poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		Decider:   model,
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer session.Close()

	if _, err := session.Prompt(context.Background(), "find a tool"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if result.Reason == poo.ReasonToolFatal {
		t.Errorf("a failed search settled %q", result.Reason)
	}
	// The model saw the search's own text and nothing of the cause.
	got := lastToolResult(t, transport)
	if want := poo.HandleLine("r1", cliregistry.FindToolName) + "\n" + cliregistry.UnavailableText; got != want {
		t.Errorf("the model reads %q, want %q", got, want)
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
