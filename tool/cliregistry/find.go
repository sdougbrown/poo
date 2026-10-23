package cliregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/prompt"
)

// DefaultTopK is how many entries a search returns when nothing says
// otherwise.
const DefaultTopK = 8

// UnavailableText is what the model reads when the search cannot run.
//
// A failed search does not fall back to the whole catalog: a list of
// hundreds of entries is what the search was there to avoid, and a model
// told "here is everything" learns nothing about which entry fits. The
// model is told the search failed and how to go on without it.
const UnavailableText = "tool search unavailable; call_cli with a known name"

// FindInstructions tell the decision model what the questions are.
const FindInstructions = "Each question names one registered command-line tool and asks whether that tool fits the intent. " +
	"Answer yes when the tool does the work the intent asks for, and no when it does something else."

// FindTool searches the catalog for the CLIs that fit an intent.
//
// The search is a decision-model question per entry, so it needs a
// decision model: a session without one shows the whole catalog instead.
type FindTool struct {
	// catalog is the set of CLIs the search narrows.
	catalog *Catalog
	// topK is how many entries a search returns. Zero means
	// DefaultTopK.
	topK int
}

// NewFind returns the tool that searches the catalog.
func NewFind(catalog *Catalog) *FindTool {
	return &FindTool{catalog: catalog}
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
		Description: "Search the registered command-line tools for the ones that fit what you want to do.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "intent": {"type": "string", "description": "What you want to do, in your own words."}
  },
  "required": ["intent"]
}`),
		Class: poo.ClassReadOnly,
	}
}

// The prompt section the tool brings.
//
// The catalog's own section says the catalog is behind a search. The tool
// adds how to search it and what a search costs.
const findSection = "Finding a tool:\n" +
	"- Call find_tool with what you want to do, in your own words, and it returns the registered CLIs that fit, best first, with their arguments.\n" +
	"- A search asks the decision model one question per registered tool, so call it when you need a tool you have not used before, not before every call.\n" +
	"- find_tool narrows the list; it never chooses for you. Call the tool it returns with call_cli."

// PromptSection renders the tool's guidance.
func (t *FindTool) PromptSection(prompt.Env) string { return findSection }

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
	// entry is the registered CLI.
	entry Entry
	// probability is the model's probability that the entry fits.
	probability float64
}

// Call searches the catalog.
//
// One question per entry goes to the decision model, in one call: the
// decider splits a long question set into requests of its own, so one
// search is one decision and one record. The entries the model is most
// sure about come back first, the default eight of them, with their
// descriptions and the schema of their arguments.
func (t *FindTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(*preparedFind)
	if !ok {
		return poo.ToolResult{}, fmt.Errorf("%s: prepared value is not a %s call", FindToolName, FindToolName)
	}
	if env.Decider == nil {
		return unavailable(), nil
	}
	entries := t.catalog.Entries()
	questions := make([]decide.Question, 0, len(entries))
	for _, entry := range entries {
		questions = append(questions, findQuestion(entry, p.intent))
	}
	answers, err := env.Decider.Decide(ctx, map[string]string{"intent": p.intent}, FindInstructions, questions)
	if err == nil {
		// A model that answered badly has not answered, and the search
		// fails the same way as one the decider refused.
		err = decide.ValidateAnswers(questions, answers)
	}
	if err != nil {
		return unavailable(), nil
	}

	best := make([]ranked, 0, len(entries))
	for _, entry := range entries {
		probability, ok := answerFor(answers, entry.Name)
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
		return best[i].entry.Name < best[j].entry.Name
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

// unavailable is the result of a search that cannot run.
//
// A failed search is recoverable: the model is told the search is
// unavailable and how to go on without it, and the cause stays in the
// decision record. The result is the same whatever went wrong, so a
// search never settles a run.
func unavailable() poo.ToolResult {
	return poo.ToolResult{Text: UnavailableText, IsError: true}
}

// findQuestion asks whether one entry fits the intent.
func findQuestion(entry Entry, intent string) decide.Question {
	return decide.Question{
		ID: entry.Name,
		Instructions: fmt.Sprintf("Does %q fit this intent? The intent is %q. The tool: %s",
			entry.Name, intent, entry.Description),
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
// The list carries what the model needs to make the call itself: the
// name, what the tool does, and the schema of its arguments.
func result(intent string, total int, hits []ranked) poo.ToolResult {
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d registered CLIs fit %q, best first:\n", len(hits), total, intent)
	for _, r := range hits {
		fmt.Fprintf(&b, "%s: %s\n  args: %s\n", r.entry.Name, r.entry.Description, string(marshal(r.entry.Schema)))
	}
	b.WriteString("Call one with " + CallToolName + ".")

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
		})
	}
	return out
}
