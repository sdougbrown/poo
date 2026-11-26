package cliregistry

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/prompt"
)

// DefaultTopK is how many entries a search returns when nothing says
// otherwise.
const DefaultTopK = 8

// UnavailableText is what the model reads when the search cannot run and its
// view names no entry at all.
//
// A failed search does not fall back to the whole offering: a list of
// hundreds of entries is what the search was there to avoid, and a model
// told "here is everything" learns nothing about which entry fits. The
// model is told the search failed and how to go on without it.
const UnavailableText = "tool search unavailable; call a tool the prompt lists"

// FindInstructions tell the decision model what the questions are.
const FindInstructions = "Each question names one tool and asks whether that tool fits the intent. " +
	"Answer yes when the tool does the work the intent asks for, and no when it does something else."

// FindTool searches the offered entries for the tools that fit an intent.
//
// The search is a decision-model question per entry, so it needs a
// decision model: a session without one shows the whole catalog instead.
type FindTool struct {
	// offered are the entries the search narrows, in name order.
	offered []cli.Offered
	// topK is how many entries a search returns. Zero means
	// DefaultTopK.
	topK int
}

// NewFind returns the tool that searches the entries the model can reach.
//
// The entries are the session's snapshot: the catalog's registered commands,
// which call_cli runs, and the MCP tools its servers listed, which call_mcp
// runs. Each hit names the tool that calls it, so the search never points the
// model at a call the session does not offer.
func NewFind(offered []cli.Offered) *FindTool {
	return &FindTool{offered: sortOffered(offered)}
}

// sortOffered orders the entries by name, so two searches over the same
// offering return the same list. The list is a copy: a search never writes
// into the offering it was given.
func sortOffered(entries []cli.Offered) []cli.Offered {
	out := make([]cli.Offered, len(entries))
	copy(out, entries)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// WithTopK sets how many entries a search returns.
func (t *FindTool) WithTopK(k int) *FindTool {
	t.topK = k
	return t
}

// Spec describes the tool.
func (t *FindTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        FindToolName,
		Description: "Search the tools this session offers — registered commands and MCP tools — for the ones that fit what you want to do.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "intent": {"type": "string", "description": "What you want to do, in your own words."}
  },
  "required": ["intent"]
,"additionalProperties":false}`),
		Class: poo.ClassReadOnly,
	}
}

// The prompt section the tool brings.
//
// The catalog's own section says the catalog is behind a search. The tool
// adds how to search it and what a search costs.
//
//go:embed find.md
var findSectionText string

var findSection = prompt.MustCompile(FindToolName, findSectionText, nil)

// PromptSection renders the tool's guidance.
func (t *FindTool) PromptSection(env prompt.Env) string { return findSection.Render(env) }

// findArgs are the tool's arguments.
type findArgs struct {
	// Intent is what the model wants to do.
	Intent string `json:"intent"`
}

// preparedFind is a validated find_tool call.
type preparedFind struct {
	// intent is what the model wants to do.
	intent string
}

// Summary describes the call for a human reader.
func (p *preparedFind) Summary() string { return p.intent }

// CanonicalKey returns the call's prepared input.
func (p *preparedFind) CanonicalKey() json.RawMessage {
	return marshal(struct {
		Intent string `json:"intent"`
	}{p.intent})
}

// EvidenceArguments returns the arguments a decision model may see.
func (p *preparedFind) EvidenceArguments() json.RawMessage { return p.CanonicalKey() }

// Prepare validates the arguments.
func (t *FindTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a findArgs
	if err := decodeStrict(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("%s: %v", FindToolName, err)
	}
	a.Intent = strings.TrimSpace(a.Intent)
	if a.Intent == "" {
		return nil, poo.InvalidArgsf("%s: intent must not be empty", FindToolName)
	}
	return &preparedFind{intent: a.Intent}, nil
}

// ranked is one entry with the model's answer about it.
type ranked struct {
	// entry is the offered entry.
	entry cli.Offered
	// probability is the model's probability that the entry fits.
	probability float64
}

// Call searches the offering.
//
// One question per entry goes to the decision model, in one call: the
// decider splits a long question set into requests of its own, so one
// search is one decision and one record. The entries the model is most
// sure about come back first, the default eight of them, with their
// descriptions and the schema of their arguments, each beside the tool that
// calls it.
func (t *FindTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(*preparedFind)
	if !ok {
		return poo.ToolResult{}, fmt.Errorf("%s: prepared value is not a %s call", FindToolName, FindToolName)
	}
	if env.Decider == nil {
		return t.unavailable(), nil
	}
	entries := t.offered
	// Two entries of different dispatchers may share a name, and a question
	// is identified by its entry: the identifier is the name unless another
	// entry already holds it.
	ids := questionIDs(entries)
	questions := make([]decide.Question, 0, len(entries))
	for i, entry := range entries {
		questions = append(questions, findQuestion(entry, ids[i], p.intent))
	}
	answers, err := env.Decider.Decide(ctx, map[string]string{"intent": p.intent}, FindInstructions, questions)
	if err == nil {
		// A model that answered badly has not answered, and the search
		// fails the same way as one the decider refused.
		err = decide.ValidateAnswers(questions, answers)
	}
	if err != nil {
		return t.unavailable(), nil
	}

	best := make([]ranked, 0, len(entries))
	for i, entry := range entries {
		probability, ok := answerFor(answers, ids[i])
		if !ok {
			// The answers were checked against the questions, so an
			// entry without an answer cannot happen; leaving it out
			// keeps a missing entry out of the list rather than
			// giving it a made-up rank.
			continue
		}
		best = append(best, ranked{entry: entry, probability: probability})
	}
	// The best first, and the name breaks a tie, so two searches with
	// the same answers return the same list.
	sort.SliceStable(best, func(i, j int) bool {
		if best[i].probability != best[j].probability {
			return best[i].probability > best[j].probability
		}
		if best[i].entry.Name != best[j].entry.Name {
			return best[i].entry.Name < best[j].entry.Name
		}
		return best[i].entry.Dispatcher < best[j].entry.Dispatcher
	})
	k := t.topK
	if k <= 0 {
		k = DefaultTopK
	}
	if k > len(best) {
		k = len(best)
	}
	return result(p.intent, len(entries), best[:k]), nil
}

// questionIDs returns the identifier each entry's question is asked under.
//
// The name identifies the entry, and two entries of different dispatchers may
// share one: the second is asked under the dispatcher's own name, so the
// answers tell the two apart and each hit still reads by its own name.
func questionIDs(entries []cli.Offered) []string {
	ids := make([]string, len(entries))
	seen := make(map[string]bool, len(entries))
	for i, entry := range entries {
		id := entry.Name
		if seen[id] {
			id = entry.Dispatcher + ":" + entry.Name
		}
		seen[id] = true
		ids[i] = id
	}
	return ids
}

// unavailable is the result of a search that cannot run.
//
// A failed search is recoverable: the model is told the search is
// unavailable and how to go on without it, and the cause stays in the
// decision record. The result is the same whatever went wrong, so a
// search never settles a run.
func (t *FindTool) unavailable() poo.ToolResult {
	return poo.ToolResult{Text: t.unavailableText(), IsError: true}
}

// unavailableText says where the names are when the search cannot answer.
//
// The view is what the session can reach, so each dispatcher it holds is
// named with the argument that carries an entry's name: a model sent to a
// tool the session does not offer, or to a search that just failed, has
// nowhere to go. A view that holds no entry names no tool, and the prompt's
// catalog is all that is left.
func (t *FindTool) unavailableText() string {
	var parts []string
	for _, entry := range t.offered {
		part := entry.Dispatcher + " takes " + strconv.Quote(entry.Argument)
		if slices.Contains(parts, part) {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return UnavailableText
	}
	// The offering's order is the entries' own, so the words are ordered by the
	// call: the same dispatchers read the same way whatever the entries are
	// called.
	sort.Strings(parts)
	return "tool search unavailable; " + strings.Join(parts, ", or ")
}

// findQuestion asks whether one entry fits the intent.
//
// The question names the entry the way the model writes it, and says what
// calls it: a model that answered for the wrong dispatcher would still have
// chosen the same tool.
func findQuestion(entry cli.Offered, id, intent string) decide.Question {
	return decide.Question{
		ID: id,
		Instructions: fmt.Sprintf("Does %q fit this intent? The intent is %q. The tool: %s It is called with %s.",
			entry.Name, intent, entry.Description, entry.Dispatcher),
		Kind: decide.Bool{
			True:  "the tool does what the intent asks for",
			False: "the tool does something else",
		},
	}
}

// answerFor returns the probability that an entry fits.
func answerFor(answers []decide.Answer, id string) (float64, bool) {
	for _, answer := range answers {
		if answer.ID == id {
			return answer.Probabilities["yes"], true
		}
	}
	return 0, false
}

// result renders the entries a search found.
//
// The list carries what the model needs to make the call itself: the name,
// what the tool does, the schema of its arguments, and the tool that calls
// it. A hit that named only the entry would leave the model to guess between
// call_cli and call_mcp.
func result(intent string, total int, hits []ranked) poo.ToolResult {
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d tools fit %q, best first:\n", len(hits), total, intent)
	for _, r := range hits {
		fmt.Fprintf(&b, "%s%s: %s\n  args: %s\n  call it with %s, passing %q as %q.\n",
			r.entry.Name, r.entry.Hint(), r.entry.Description, string(marshal(r.entry.Schema)),
			r.entry.Dispatcher, r.entry.Name, r.entry.Argument)
	}

	structured, err := json.Marshal(struct {
		Intent  string  `json:"intent"`
		Total   int     `json:"total"`
		Results []found `json:"results"`
	}{intent, total, foundEntries(hits)})
	if err != nil {
		structured = []byte("{}")
	}
	return poo.ToolResult{Text: strings.TrimRight(b.String(), "\n"), Structured: structured}
}

// found is one entry of a search's structured result.
type found struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Probability float64        `json:"probability"`
	Schema      map[string]any `json:"schema"`
	Dispatcher  string         `json:"dispatcher"`
	Argument    string         `json:"argument"`
	ReadOnly    bool           `json:"read_only,omitempty"`
	Destructive bool           `json:"destructive,omitempty"`
}

// foundEntries renders the search's entries for the structured result.
func foundEntries(hits []ranked) []found {
	out := make([]found, 0, len(hits))
	for _, r := range hits {
		out = append(out, found{
			Name:        r.entry.Name,
			Description: r.entry.Description,
			Probability: r.probability,
			Schema:      r.entry.Schema,
			Dispatcher:  r.entry.Dispatcher,
			Argument:    r.entry.Argument,
			ReadOnly:    r.entry.ReadOnly,
			Destructive: r.entry.Destructive,
		})
	}
	return out
}
