package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/provider/openai"
)

// systemOneServer is a scripted systemone endpoint.
type systemOneServer struct {
	*httptest.Server
	// mu guards the requests.
	mu sync.Mutex
	// bodies holds the request bodies the server received, in order.
	bodies []string
	// answers is the answer the server sends, keyed by the question's
	// id. A question the map does not hold is not answered.
	answers map[string]any
	// delay is how long the server waits before it answers.
	delay time.Duration
	// status is the HTTP status the server answers with.
	status int
	// active is how many requests the server is answering now.
	active int
	// peak is the most requests the server answered at once.
	peak int
}

// newSystemOneServer starts a server that answers the questions it is
// given.
func newSystemOneServer(t *testing.T) *systemOneServer {
	t.Helper()
	s := &systemOneServer{answers: map[string]any{}, status: http.StatusOK}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(raw))
		s.active++
		if s.active > s.peak {
			s.peak = s.active
		}
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.active--
			s.mu.Unlock()
		}()

		if s.delay > 0 {
			select {
			case <-time.After(s.delay):
			case <-r.Context().Done():
				return
			}
		}
		if s.status != http.StatusOK {
			w.WriteHeader(s.status)
			_, _ = io.WriteString(w, `{"error":{"message":"refused"}}`)
			return
		}
		var req struct {
			Questions map[string]struct {
				Type string `json:"type"`
			} `json:"questions"`
		}
		_ = json.Unmarshal(raw, &req)
		answers := map[string]any{}
		diagnostics := map[string]any{}
		for id := range req.Questions {
			answer, ok := s.answers[id]
			if !ok {
				continue
			}
			answers[id] = answer
			diagnostics[id] = map[string]any{"label_mass": 0.9, "argmax_is_label": true}
		}
		body, _ := json.Marshal(map[string]any{
			"object":      "structured_decision",
			"id":          "decision-1",
			"created":     1730000000,
			"answers":     answers,
			"diagnostics": diagnostics,
			"usage":       map[string]any{"input_tokens": 120, "output_tokens": 0},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

// count returns how many requests the server received.
func (s *systemOneServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

// peakConcurrency returns the most requests the server answered at once.
func (s *systemOneServer) peakConcurrency() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak
}

// body returns the request body at the given index.
func (s *systemOneServer) body(i int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[i]
}

// decider returns a decider that talks to the server.
func (s *systemOneServer) decider(t *testing.T, timeout time.Duration) *openai.SystemOneDecider {
	t.Helper()
	return openai.NewSystemOneDecider(openai.SystemOneConfig{
		BaseURL: s.URL,
		Model:   "gemma4",
		Timeout: timeout,
	})
}

// choiceQuestion is a question with two options.
func choiceQuestion(id string) decide.Question {
	return decide.Question{
		ID:           id,
		Instructions: "does the change look right?",
		Kind: decide.Choice{Options: []decide.Option{
			{Name: "approve", Desc: "the change is right"},
			{Name: "deny", Desc: "the change is wrong"},
		}},
	}
}

// boolQuestion is a yes-or-no question.
func boolQuestion(id string) decide.Question {
	return decide.Question{
		ID:           id,
		Instructions: "is the test failing?",
		Kind:         decide.Bool{True: "the test fails", False: "the test passes"},
	}
}

// scoreQuestion is a question with a scale.
func scoreQuestion(id string) decide.Question {
	return decide.Question{
		ID:           id,
		Instructions: "how bad is it?",
		Kind:         decide.Score{Levels: []string{"fine", "broken"}},
	}
}

// TestTheSystemOneDeciderAnswersAChoiceQuestion covers the choice
// question: the request's shape and the answer that comes back.
func TestTheSystemOneDeciderAnswersAChoiceQuestion(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.answers["q1"] = map[string]any{
		"probabilities": map[string]float64{"approve": 0.8, "deny": 0.2},
		"choice":        "approve",
		"confidence":    0.72,
	}
	dec := srv.decider(t, 0)

	answers, err := dec.Decide(context.Background(), map[string]any{"cwd": "/tmp"}, "judge the change", []decide.Question{choiceQuestion("q1")})
	if err != nil {
		t.Fatalf("asking the question: %v", err)
	}
	if len(answers) != 1 {
		t.Fatalf("the call returned %d answers, want 1", len(answers))
	}
	answer := answers[0]
	if answer.ID != "q1" {
		t.Errorf("the answer is for %q, want q1", answer.ID)
	}
	if answer.Choice != "approve" {
		t.Errorf("the choice is %q, want approve", answer.Choice)
	}
	if got := answer.Probabilities["approve"]; got != 0.8 {
		t.Errorf("the probability of approve is %v, want 0.8", got)
	}
	if answer.Confidence == nil || *answer.Confidence != 0.72 {
		t.Errorf("the confidence is %v, want 0.72", answer.Confidence)
	}
	if answer.LabelMass == nil || *answer.LabelMass != 0.9 {
		t.Errorf("the label mass is %v, want 0.9", answer.LabelMass)
	}

	// The request carries the model, the state, the instructions and the
	// question.
	var body struct {
		Model        string `json:"model"`
		State        any    `json:"state"`
		Instructions string `json:"instructions"`
		Questions    map[string]struct {
			Type         string         `json:"type"`
			Instructions string         `json:"instructions"`
			Criteria     map[string]any `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(srv.body(0)), &body); err != nil {
		t.Fatalf("decoding the request: %v", err)
	}
	if body.Model != "gemma4" {
		t.Errorf("the request names the model %q, want gemma4", body.Model)
	}
	if body.Instructions != "judge the change" {
		t.Errorf("the request carries the instructions %q, want judge the change", body.Instructions)
	}
	question, ok := body.Questions["q1"]
	if !ok {
		t.Fatalf("the request does not carry the question: %s", srv.body(0))
	}
	if question.Type != "choice" {
		t.Errorf("the question is a %q, want choice", question.Type)
	}
	if question.Instructions != "does the change look right?" {
		t.Errorf("the question carries the instructions %q", question.Instructions)
	}
	if question.Criteria["approve"] != "the change is right" {
		t.Errorf("the criteria are %v, want the option's description", question.Criteria)
	}
}

// TestTheSystemOneDeciderAnswersANoulQuestion covers the bool question:
// it is a noul on the wire, and the probabilities are keyed yes and no.
func TestTheSystemOneDeciderAnswersANoulQuestion(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.answers["q1"] = map[string]any{
		"probabilities": map[string]float64{"yes": 0.7, "no": 0.3},
		"noul":          0.7,
		"confidence":    0.63,
	}
	dec := srv.decider(t, 0)

	answers, err := dec.Decide(context.Background(), "the state", "", []decide.Question{boolQuestion("q1")})
	if err != nil {
		t.Fatalf("asking the question: %v", err)
	}
	if len(answers) != 1 {
		t.Fatalf("the call returned %d answers, want 1", len(answers))
	}
	if got := answers[0].Probabilities["yes"]; got != 0.7 {
		t.Errorf("the probability of yes is %v, want 0.7", got)
	}
	if got := answers[0].Probabilities["no"]; got != 0.3 {
		t.Errorf("the probability of no is %v, want 0.3", got)
	}

	var body struct {
		Questions map[string]struct {
			Type     string         `json:"type"`
			Criteria map[string]any `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(srv.body(0)), &body); err != nil {
		t.Fatalf("decoding the request: %v", err)
	}
	question := body.Questions["q1"]
	if question.Type != "noul" {
		t.Errorf("the question is a %q, want noul", question.Type)
	}
	if question.Criteria["true"] != "the test fails" {
		t.Errorf("the criteria are %v, want the true description", question.Criteria)
	}
	if question.Criteria["false"] != "the test passes" {
		t.Errorf("the criteria are %v, want the false description", question.Criteria)
	}
}

// TestTheSystemOneDeciderAnswersAScoreQuestion covers the score question:
// the levels are the criteria, and the probabilities are keyed by the
// level's index.
func TestTheSystemOneDeciderAnswersAScoreQuestion(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.answers["q1"] = map[string]any{
		"probabilities": map[string]float64{"0": 0.2, "1": 0.8},
		"score":         0.8,
		"legend":        map[string]string{"0": "fine", "1": "broken"},
		"confidence":    0.64,
	}
	dec := srv.decider(t, 0)

	answers, err := dec.Decide(context.Background(), "the state", "", []decide.Question{scoreQuestion("q1")})
	if err != nil {
		t.Fatalf("asking the question: %v", err)
	}
	if len(answers) != 1 {
		t.Fatalf("the call returned %d answers, want 1", len(answers))
	}
	if got := answers[0].Probabilities["1"]; got != 0.8 {
		t.Errorf("the probability of the second level is %v, want 0.8", got)
	}
	if answers[0].Score == nil || *answers[0].Score != 0.8 {
		t.Errorf("the score is %v, want 0.8", answers[0].Score)
	}

	var body struct {
		Questions map[string]struct {
			Type     string   `json:"type"`
			Criteria []string `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(srv.body(0)), &body); err != nil {
		t.Fatalf("decoding the request: %v", err)
	}
	question := body.Questions["q1"]
	if question.Type != "score" {
		t.Errorf("the question is a %q, want score", question.Type)
	}
	if len(question.Criteria) != 2 || question.Criteria[0] != "fine" {
		t.Errorf("the criteria are %v, want the two levels", question.Criteria)
	}
}

// TestTheSystemOneDeciderKeepsFourRequestsInFlight covers the ceiling: a
// call whose questions need ten requests opens no more than four of them
// at once.
func TestTheSystemOneDeciderKeepsFourRequestsInFlight(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.delay = 5 * time.Millisecond
	questions := make([]decide.Question, 0, 600)
	for i := 0; i < 600; i++ {
		id := fmt.Sprintf("q%03d", i)
		questions = append(questions, choiceQuestion(id))
		srv.answers[id] = map[string]any{
			"probabilities": map[string]float64{"approve": 0.5, "deny": 0.5},
			"choice":        "approve",
		}
	}
	dec := srv.decider(t, 30*time.Second)

	if _, err := dec.Decide(context.Background(), "the state", "", questions); err != nil {
		t.Fatalf("asking the questions: %v", err)
	}
	if srv.count() != 10 {
		t.Errorf("the call made %d requests, want 10", srv.count())
	}
	if got := srv.peakConcurrency(); got > decide.MaxRequestsInFlight {
		t.Errorf("the call had %d requests in flight, the most is %d", got, decide.MaxRequestsInFlight)
	}
}

// TestTheSystemOneDeciderSplitsALongCall covers the split: a call with
// more than sixty-four questions goes out as three requests, and the
// answers come back in the order the questions were asked.
func TestTheSystemOneDeciderSplitsALongCall(t *testing.T) {
	srv := newSystemOneServer(t)
	questions := make([]decide.Question, 0, 150)
	for i := 0; i < 150; i++ {
		id := fmt.Sprintf("q%03d", i)
		questions = append(questions, choiceQuestion(id))
		srv.answers[id] = map[string]any{
			"probabilities": map[string]float64{"approve": 0.5, "deny": 0.5},
			"choice":        "approve",
		}
	}
	dec := srv.decider(t, 0)

	answers, err := dec.Decide(context.Background(), "the state", "", questions)
	if err != nil {
		t.Fatalf("asking the questions: %v", err)
	}
	if srv.count() != 3 {
		t.Errorf("the call made %d requests, want 3", srv.count())
	}
	if dec.Requests() != 3 {
		t.Errorf("the decider reports %d requests, want 3", dec.Requests())
	}
	if len(answers) != 150 {
		t.Fatalf("the call returned %d answers, want 150", len(answers))
	}
	for i, answer := range answers {
		want := fmt.Sprintf("q%03d", i)
		if answer.ID != want {
			t.Fatalf("the answer %d is for %q, want %q", i, answer.ID, want)
		}
	}
}

// TestTheSystemOneDeciderRefusesAnInvalidQuestion covers the validation:
// a question with twenty-seven options is the caller's bug, and no
// request goes out for it.
func TestTheSystemOneDeciderRefusesAnInvalidQuestion(t *testing.T) {
	srv := newSystemOneServer(t)
	dec := srv.decider(t, 0)

	options := make([]decide.Option, 0, 27)
	for i := 0; i < 27; i++ {
		options = append(options, decide.Option{Name: fmt.Sprintf("o%02d", i)})
	}
	question := decide.Question{
		ID:           "q1",
		Instructions: "which one?",
		Kind:         decide.Choice{Options: options},
	}
	answers, err := dec.Decide(context.Background(), "the state", "", []decide.Question{question})
	if !errors.Is(err, decide.ErrInvalidQuestion) {
		t.Fatalf("the call failed with %v, want an invalid question", err)
	}
	if answers != nil {
		t.Errorf("the call returned %d answers, want none", len(answers))
	}
	if srv.count() != 0 {
		t.Errorf("the call made %d requests, want none", srv.count())
	}
}

// TestTheSystemOneDeciderDefusesAMissingAnswer covers the answer
// validation: a question the endpoint did not answer is a deferral.
func TestTheSystemOneDeciderDefusesAMissingAnswer(t *testing.T) {
	srv := newSystemOneServer(t)
	// The server answers nothing: the question is missing.
	dec := srv.decider(t, 0)

	_, err := dec.Decide(context.Background(), "the state", "", []decide.Question{choiceQuestion("q1")})
	if !errors.Is(err, decide.ErrDeferred) {
		t.Fatalf("the call failed with %v, want a deferral", err)
	}
}

// TestTheSystemOneDeciderDefusesADistributionThatDoesNotAddUp covers the
// answer validation: a distribution off by more than a thousandth is a
// deferral.
func TestTheSystemOneDeciderDefusesADistributionThatDoesNotAddUp(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.answers["q1"] = map[string]any{
		"probabilities": map[string]float64{"approve": 0.8, "deny": 0.3},
		"choice":        "approve",
	}
	dec := srv.decider(t, 0)

	_, err := dec.Decide(context.Background(), "the state", "", []decide.Question{choiceQuestion("q1")})
	if !errors.Is(err, decide.ErrDeferred) {
		t.Fatalf("the call failed with %v, want a deferral", err)
	}
}

// TestTheSystemOneDeciderDefusesAFailedRequest covers the failure: an
// endpoint that refuses the request is a deferral, not an error the
// caller has to read.
func TestTheSystemOneDeciderDefusesAFailedRequest(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.status = http.StatusInternalServerError
	dec := srv.decider(t, 0)

	_, err := dec.Decide(context.Background(), "the state", "", []decide.Question{choiceQuestion("q1")})
	if !errors.Is(err, decide.ErrDeferred) {
		t.Fatalf("the call failed with %v, want a deferral", err)
	}
}

// TestTheSystemOneDeciderDefusesATimeout covers the timeout: a call that
// runs out of its budget is a deferral.
func TestTheSystemOneDeciderDefusesATimeout(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.delay = 300 * time.Millisecond
	dec := srv.decider(t, 50*time.Millisecond)

	_, err := dec.Decide(context.Background(), "the state", "", []decide.Question{choiceQuestion("q1")})
	if !errors.Is(err, decide.ErrDeferred) {
		t.Fatalf("the call failed with %v, want a deferral", err)
	}
}

// TestTheSystemOneDeciderHonoursTheLatencyBudget covers the budget: it
// covers the whole call, the split requests included, so a call that is
// split into three requests has one budget and not three.
func TestTheSystemOneDeciderHonoursTheLatencyBudget(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.delay = 200 * time.Millisecond
	questions := make([]decide.Question, 0, 150)
	for i := 0; i < 150; i++ {
		questions = append(questions, choiceQuestion(fmt.Sprintf("q%03d", i)))
	}
	dec := srv.decider(t, 100*time.Millisecond)

	started := time.Now()
	_, err := dec.Decide(context.Background(), "the state", "", questions)
	elapsed := time.Since(started)
	if !errors.Is(err, decide.ErrDeferred) {
		t.Fatalf("the call failed with %v, want a deferral", err)
	}
	// The budget is one budget: the call gives up long before the three
	// requests could have finished.
	if elapsed > 400*time.Millisecond {
		t.Errorf("the call took %v, want it to give up on the budget", elapsed)
	}
}

// TestTheSystemOneDeciderPassesTheLabelMassThrough covers the label
// mass: it is passed through untouched, so a caller can treat a low
// mass as a deferral of its own.
func TestTheSystemOneDeciderPassesTheLabelMassThrough(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.answers["q1"] = map[string]any{
		"probabilities": map[string]float64{"approve": 0.8, "deny": 0.2},
		"choice":        "approve",
	}
	dec := srv.decider(t, 0)

	answers, err := dec.Decide(context.Background(), "the state", "", []decide.Question{choiceQuestion("q1")})
	if err != nil {
		t.Fatalf("asking the question: %v", err)
	}
	if answers[0].LabelMass == nil {
		t.Fatal("the answer holds no label mass")
	}
	if *answers[0].LabelMass != 0.9 {
		t.Errorf("the label mass is %v, want 0.9", *answers[0].LabelMass)
	}
}

// TestTheSystemOneDeciderWorksWithoutAChatModel covers the decider on
// its own: a model that serves the systemone operation and nothing else
// is enough, and no chat model is needed.
func TestTheSystemOneDeciderWorksWithoutAChatModel(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.answers["q1"] = map[string]any{
		"probabilities": map[string]float64{"approve": 0.8, "deny": 0.2},
		"choice":        "approve",
	}
	model := poo.ModelInfo{
		Name:     "gemma4",
		Endpoint: srv.URL,
		ModelID:  "gemma4",
		API:      poo.APIChatCompletions,
		Ops:      []poo.Op{poo.OpSystemOne},
	}
	dec, err := openai.NewSystemOneDeciderFor(model, openai.SystemOneConfig{})
	if err != nil {
		t.Fatalf("building the decider: %v", err)
	}
	answers, err := dec.Decide(context.Background(), "the state", "", []decide.Question{choiceQuestion("q1")})
	if err != nil {
		t.Fatalf("asking the question: %v", err)
	}
	if len(answers) != 1 {
		t.Fatalf("the call returned %d answers, want 1", len(answers))
	}
}

// TestTheSystemOneDeciderRefusesAModelWithoutTheOperation covers the
// construction: a model that does not serve the systemone operation
// cannot be the decider's model.
func TestTheSystemOneDeciderRefusesAModelWithoutTheOperation(t *testing.T) {
	model := poo.ModelInfo{
		Name:    "gemma4",
		ModelID: "gemma4",
		Ops:     []poo.Op{poo.OpChat},
	}
	_, err := openai.NewSystemOneDeciderFor(model, openai.SystemOneConfig{})
	if err == nil {
		t.Fatal("the decider was built for a model without the systemone operation")
	}
	if !strings.Contains(err.Error(), "systemone") {
		t.Errorf("the failure is %v, want it to name the operation", err)
	}
}

// TestTheSystemOneDeciderRecordsOneCall covers the recording: the
// wrapper writes one record for the call, whatever the number of wire
// requests it took.
func TestTheSystemOneDeciderRecordsOneCall(t *testing.T) {
	srv := newSystemOneServer(t)
	srv.answers["q1"] = map[string]any{
		"probabilities": map[string]float64{"approve": 0.8, "deny": 0.2},
		"choice":        "approve",
	}
	dec := srv.decider(t, 0)
	recorded := &countingSink{}
	recorder := decide.Recorded(dec, recorded, "the caller")

	if _, err := recorder.Decide(context.Background(), "the state", "", []decide.Question{choiceQuestion("q1")}); err != nil {
		t.Fatalf("asking the question: %v", err)
	}
	if recorded.count != 1 {
		t.Errorf("the sink received %d records, want 1", recorded.count)
	}
}

// countingSink counts the records it received.
type countingSink struct {
	// count is how many records the sink received.
	count int
}

// Record counts one record.
func (s *countingSink) Record(ctx context.Context, rec decide.Record) error {
	s.count++
	return nil
}

// TestTheSystemOneDeciderTalksToALiveEndpoint covers the live endpoint.
//
// The test is skipped unless the environment names one: the fixture
// tests above cover the shapes, and this one covers a real server.
func TestTheSystemOneDeciderTalksToALiveEndpoint(t *testing.T) {
	url := os.Getenv("POO_SYSTEMONE_URL")
	model := os.Getenv("POO_SYSTEMONE_MODEL")
	if url == "" || model == "" {
		t.Skip("POO_SYSTEMONE_URL and POO_SYSTEMONE_MODEL are not set")
	}
	dec := openai.NewSystemOneDecider(openai.SystemOneConfig{
		BaseURL: url,
		Model:   model,
		APIKey:  os.Getenv("POO_SYSTEMONE_API_KEY"),
		Timeout: 30 * time.Second,
	})
	answers, err := dec.Decide(context.Background(),
		decide.EvidencePayload{Instructions: "the state", Cwd: "/tmp"},
		"answer the question",
		[]decide.Question{{
			ID:           "q1",
			Instructions: "is the sky blue?",
			Kind:         decide.Bool{True: "the sky is blue", False: "the sky is not blue"},
		}})
	if err != nil {
		t.Fatalf("asking the question: %v", err)
	}
	if len(answers) != 1 {
		t.Fatalf("the call returned %d answers, want 1", len(answers))
	}
	total := 0.0
	for _, p := range answers[0].Probabilities {
		total += p
	}
	if total < 0.99 || total > 1.01 {
		t.Errorf("the probabilities sum to %v, want 1", total)
	}
}
