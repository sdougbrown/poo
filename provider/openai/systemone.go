package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
)

// DefaultDeciderTimeout is the latency budget of one decision call.
//
// The budget covers the whole call, the split requests included: a call
// that is split into three requests has one budget and not three.
const DefaultDeciderTimeout = 1500 * time.Millisecond

// SystemOneConfig configures the systemone decider.
type SystemOneConfig struct {
	// Endpoint is the endpoint's base, e.g. "http://localhost:8000/v1".
	Endpoint string
	// Model is the identifier the endpoint calls the model.
	Model string
	// APIKey is the bearer token, empty when the endpoint needs none.
	APIKey string
	// HTTPClient is the client to use. Nil means a default client.
	HTTPClient *http.Client
	// Timeout bounds one Decide call, the split requests included.
	// Zero means DefaultDeciderTimeout.
	Timeout time.Duration
}

// SystemOneDecider asks a decision model over a systemone endpoint.
//
// The endpoint is the one vLLM serves by default: the questions go out
// in batches of at most sixty-four, and the answers come back in the
// order the questions were asked.
//
// Every failure of a call is a deferral. A decision that could not be
// made is never an approval: the caller decides what happens next.
type SystemOneDecider struct {
	cfg  SystemOneConfig
	http *http.Client
	// mu guards requests.
	mu sync.Mutex
	// requests counts the wire requests the last call made.
	requests int
}

// NewSystemOneDecider returns a decider.
func NewSystemOneDecider(cfg SystemOneConfig) *SystemOneDecider {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		}}
	}
	return &SystemOneDecider{cfg: cfg, http: client}
}

// NewSystemOneDeciderFor returns a decider for a model.
//
// The model's endpoint and identifier are used when the configuration
// names none, and the model must serve the systemone operation: a model
// that does not cannot answer a question, and assigning it to the
// decider's role is refused.
func NewSystemOneDeciderFor(model poo.ModelInfo, cfg SystemOneConfig) (*SystemOneDecider, error) {
	if !model.HasOp(poo.OpSystemOne) {
		return nil, fmt.Errorf("openai: the model %q does not serve the %q operation",
			modelName(model), poo.OpSystemOne)
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = model.Endpoint
	}
	if cfg.Model == "" {
		cfg.Model = model.ModelID
	}
	return NewSystemOneDecider(cfg), nil
}

// Decide asks the questions about the state.
//
// The questions are checked before anything is sent: a question that is
// wrong is the caller's bug, and no request goes out for it.
//
// The call has one latency budget, the split requests included. A
// timeout, a failure, a missing answer and a distribution that does not
// add up are all deferrals.
func (d *SystemOneDecider) Decide(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
	d.setRequests(0)
	if err := decide.ValidateQuestions(qs); err != nil {
		return nil, err
	}
	budget := d.cfg.Timeout
	if budget <= 0 {
		budget = DefaultDeciderTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	answers, err := d.ask(ctx, state, instructions, qs)
	if err != nil {
		return nil, err
	}
	// The answers read the way the questions were asked.
	answers = decide.SortAnswers(qs, answers)
	if err := decide.ValidateAnswers(qs, answers); err != nil {
		return nil, err
	}
	return answers, nil
}

// Requests returns how many wire requests the last call made.
//
// A call with more than sixty-four questions is split into several
// requests, so the count is not the number of calls.
func (d *SystemOneDecider) Requests() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.requests
}

// setRequests records how many wire requests a call made.
func (d *SystemOneDecider) setRequests(n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.requests = n
}

// countRequest records one wire request.
func (d *SystemOneDecider) countRequest() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.requests++
}

// ask sends the questions and returns the answers.
//
// The questions are split into batches, and the batches go out with at
// most four requests in flight. The answers are merged in the order the
// questions were asked.
func (d *SystemOneDecider) ask(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
	batches := decide.SplitQuestions(qs, decide.MaxQuestionsPerRequest)
	if len(batches) == 0 {
		return nil, nil
	}
	answers := make([][]decide.Answer, len(batches))
	errs := make([]error, len(batches))
	var wg sync.WaitGroup
	// The requests in flight are capped, so a call with many questions
	// does not open a socket per question.
	slots := make(chan struct{}, decide.MaxRequestsInFlight)
	for i, batch := range batches {
		wg.Add(1)
		go func(i int, batch []decide.Question) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			got, err := d.askBatch(ctx, state, instructions, batch)
			answers[i], errs[i] = got, err
		}(i, batch)
	}
	wg.Wait()
	// The first failure is the one reported, and the batches are in
	// order, so the failure names the earliest batch that failed.
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	var out []decide.Answer
	for _, batch := range answers {
		out = append(out, batch...)
	}
	return out, nil
}

// askBatch sends one batch of questions and reads the answers.
func (d *SystemOneDecider) askBatch(ctx context.Context, state any, instructions string, qs []decide.Question) ([]decide.Answer, error) {
	body, err := d.buildBody(state, instructions, qs)
	if err != nil {
		return nil, deferred(err)
	}
	raw, err := d.post(ctx, body)
	if err != nil {
		return nil, err
	}
	var resp systemOneResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, deferred(fmt.Errorf("the answer does not decode: %w", err))
	}
	return answersFrom(&resp, qs)
}

// post sends the body and returns the response body.
//
// The request is counted before it goes out, so a request that never
// reached the endpoint is still counted.
func (d *SystemOneDecider) post(ctx context.Context, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, SystemOneURL(d.cfg.Endpoint), bytes.NewReader(body))
	if err != nil {
		return nil, deferred(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if d.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.cfg.APIKey)
	}
	d.countRequest()
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, deferred(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, deferred(fmt.Errorf("the endpoint answered %s: %s",
			resp.Status, strings.TrimSpace(string(raw))))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<24))
	if err != nil {
		return nil, deferred(err)
	}
	return raw, nil
}

// buildBody turns the state and the questions into the wire payload.
func (d *SystemOneDecider) buildBody(state any, instructions string, qs []decide.Question) ([]byte, error) {
	questions, err := encodeQuestions(qs)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("the state does not encode: %w", err)
	}
	return json.Marshal(systemOneRequest{
		Model:        d.cfg.Model,
		State:        raw,
		Instructions: instructions,
		Questions:    questions,
	})
}

// SystemOneURL returns the systemone endpoint of a base URL.
func SystemOneURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/systemone") {
		return base
	}
	return base + "/systemone"
}

// systemOneRequest is the wire shape of a systemone request.
type systemOneRequest struct {
	// Model is the identifier the endpoint calls the model.
	Model string `json:"model"`
	// State is the state the questions are asked about.
	State json.RawMessage `json:"state"`
	// Instructions are the instructions the whole call carries.
	Instructions string `json:"instructions,omitempty"`
	// Questions are the questions, keyed by their id.
	Questions json.RawMessage `json:"questions"`
}

// systemOneQuestion is the wire shape of one question.
type systemOneQuestion struct {
	// Type is the question's type: "choice", "noul" or "score".
	Type string `json:"type"`
	// Instructions say what to decide.
	Instructions string `json:"instructions"`
	// Criteria are the question's answers.
	Criteria json.RawMessage `json:"criteria"`
}

// systemOneResponse is the wire shape of a systemone answer.
type systemOneResponse struct {
	// Object names the shape of the answer.
	Object string `json:"object"`
	// ID identifies the answer.
	ID string `json:"id"`
	// Created is when the answer was made.
	Created int64 `json:"created"`
	// Answers are the answers, keyed by the question's id.
	Answers map[string]systemOneAnswer `json:"answers"`
	// Diagnostics are the per-question diagnostics, keyed by the
	// question's id.
	Diagnostics map[string]systemOneDiagnostic `json:"diagnostics"`
	// Usage is the token accounting.
	Usage *systemOneUsage `json:"usage"`
}

// systemOneAnswer is the wire shape of one answer.
type systemOneAnswer struct {
	// Probabilities is the distribution over the question's answers.
	Probabilities map[string]float64 `json:"probabilities"`
	// Choice is the option the model chose, for a choice question.
	Choice string `json:"choice"`
	// Confidence is how sure the model is.
	Confidence *float64 `json:"confidence"`
	// Noul is the probability of "yes", for a noul question.
	Noul *float64 `json:"noul"`
	// Score is the weighted mean of the levels, for a score question.
	Score *float64 `json:"score"`
	// Legend names the levels, for a score question.
	Legend map[string]string `json:"legend"`
	// LabelMass is how much of the distribution sat on the labels.
	//
	// The endpoint reports it in the diagnostics, and an endpoint that
	// reports it in the answer is read too.
	LabelMass *float64 `json:"label_mass"`
}

// systemOneDiagnostic is the wire shape of one question's diagnostics.
type systemOneDiagnostic struct {
	// LabelMass is how much of the distribution sat on the labels.
	LabelMass *float64 `json:"label_mass"`
	// ArgmaxIsLabel reports whether the model's most likely token was
	// one of the labels.
	ArgmaxIsLabel *bool `json:"argmax_is_label"`
}

// systemOneUsage is the wire shape of the token accounting.
type systemOneUsage struct {
	// InputTokens is the number of prompt tokens.
	InputTokens int `json:"input_tokens"`
	// OutputTokens is the number of completion tokens.
	OutputTokens int `json:"output_tokens"`
}

// encodeQuestions encodes the questions as the wire's question object.
//
// The wire shape is an object, and an object's keys have no order of
// their own: the body is written by hand, so the questions read the way
// they were asked.
func encodeQuestions(qs []decide.Question) (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, q := range qs {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(q.ID)
		if err != nil {
			return nil, fmt.Errorf("the question id %q does not encode: %w", q.ID, err)
		}
		b.Write(key)
		b.WriteByte(':')
		body, err := encodeQuestion(q)
		if err != nil {
			return nil, err
		}
		b.Write(body)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// encodeQuestion encodes one question.
//
// The three kinds map to the three question types the endpoint knows: a
// bool question is a noul, a choice question is a choice and a score
// question is a score.
func encodeQuestion(q decide.Question) (json.RawMessage, error) {
	kind, criteria, err := questionCriteria(q)
	if err != nil {
		return nil, err
	}
	return json.Marshal(systemOneQuestion{
		Type:         kind,
		Instructions: q.Instructions,
		Criteria:     criteria,
	})
}

// questionCriteria returns the question's type and its criteria.
func questionCriteria(q decide.Question) (string, json.RawMessage, error) {
	switch k := q.Kind.(type) {
	case decide.Bool:
		criteria, err := noulCriteria(k)
		return "noul", criteria, err
	case *decide.Bool:
		if k == nil {
			return "", nil, fmt.Errorf("the question %q has a nil pointer kind", q.ID)
		}
		criteria, err := noulCriteria(*k)
		return "noul", criteria, err
	case decide.Choice:
		criteria, err := choiceCriteria(k.Options)
		return "choice", criteria, err
	case *decide.Choice:
		if k == nil {
			return "", nil, fmt.Errorf("the question %q has a nil pointer kind", q.ID)
		}
		criteria, err := choiceCriteria(k.Options)
		return "choice", criteria, err
	case decide.Score:
		criteria, err := scoreCriteria(k.Levels)
		return "score", criteria, err
	case *decide.Score:
		if k == nil {
			return "", nil, fmt.Errorf("the question %q has a nil pointer kind", q.ID)
		}
		criteria, err := scoreCriteria(k.Levels)
		return "score", criteria, err
	default:
		return "", nil, fmt.Errorf("the question %q has an unknown kind %T", q.ID, q.Kind)
	}
}

// noulCriteria encodes a bool question's two descriptions.
//
// The wire shape is a map with a "true" and a "false" member, and a
// description that was not given is left out.
func noulCriteria(b decide.Bool) (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	for _, member := range []struct{ key, value string }{{"true", b.True}, {"false", b.False}} {
		if member.value == "" {
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		key, err := json.Marshal(member.key)
		if err != nil {
			return nil, fmt.Errorf("the %q member does not encode: %w", member.key, err)
		}
		value, err := json.Marshal(member.value)
		if err != nil {
			return nil, fmt.Errorf("the %q description does not encode: %w", member.key, err)
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// choiceCriteria encodes a choice question's options.
//
// The wire shape is a map from the option's name to its description, and
// an option without a description has a null one. The options are
// written in the order they were asked, so the endpoint renders them the
// way the caller meant them.
func choiceCriteria(options []decide.Option) (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, o := range options {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(o.Name)
		if err != nil {
			return nil, fmt.Errorf("the option name %q does not encode: %w", o.Name, err)
		}
		buf.Write(key)
		buf.WriteByte(':')
		if o.Desc == "" {
			buf.WriteString("null")
			continue
		}
		desc, err := json.Marshal(o.Desc)
		if err != nil {
			return nil, fmt.Errorf("the description of %q does not encode: %w", o.Name, err)
		}
		buf.Write(desc)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// scoreCriteria encodes a score question's levels.
func scoreCriteria(levels []string) (json.RawMessage, error) {
	raw, err := json.Marshal(levels)
	if err != nil {
		return nil, fmt.Errorf("the levels do not encode: %w", err)
	}
	return raw, nil
}

// answersFrom turns the endpoint's answers into the package's answers.
//
// The answers are keyed by the question's id, and the diagnostics carry
// the label mass. A question the endpoint did not answer has no answer,
// and the call is a deferral.
func answersFrom(resp *systemOneResponse, qs []decide.Question) ([]decide.Answer, error) {
	out := make([]decide.Answer, 0, len(qs))
	for _, q := range qs {
		wire, ok := resp.Answers[q.ID]
		if !ok {
			return nil, fmt.Errorf("%w: the endpoint did not answer the question %q",
				decide.ErrDeferred, q.ID)
		}
		answer := decide.Answer{
			ID:            q.ID,
			Probabilities: wire.Probabilities,
			Confidence:    wire.Confidence,
			Score:         wire.Score,
			LabelMass:     labelMass(resp, q.ID, wire),
		}
		if answer.Probabilities == nil && wire.Noul != nil {
			// A noul answer carries the probability of "yes", and the
			// distribution is built from it.
			answer.Probabilities = map[string]float64{
				"yes": *wire.Noul,
				"no":  1 - *wire.Noul,
			}
		}
		if isChoiceKind(q.Kind) {
			answer.Choice = wire.Choice
		}
		out = append(out, answer)
	}
	return out, nil
}

// labelMass returns how much of the distribution sat on the labels.
//
// The endpoint reports the mass in the diagnostics, and an endpoint that
// reports it in the answer is read too.
func labelMass(resp *systemOneResponse, id string, wire systemOneAnswer) *float64 {
	if diag, ok := resp.Diagnostics[id]; ok && diag.LabelMass != nil {
		return diag.LabelMass
	}
	return wire.LabelMass
}

// isChoiceKind reports whether the kind is a choice question.
func isChoiceKind(kind decide.Kind) bool {
	switch kind.(type) {
	case decide.Choice, *decide.Choice:
		return true
	default:
		return false
	}
}

// deferred wraps a failure as a deferral.
//
// Every failure of a decision call is a deferral: the caller's question
// could not be answered, and the caller decides what happens next.
func deferred(err error) error {
	return fmt.Errorf("%w: %w", decide.ErrDeferred, err)
}

// modelName names a model.
func modelName(m poo.ModelInfo) string {
	if m.Name != "" {
		return m.Name
	}
	return m.ModelID
}
