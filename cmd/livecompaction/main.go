// Command livecompaction drives a compaction strategy against a real
// poo session log.
//
// The harness restores a session from its own records, runs it under a
// context window the caller sizes, and wakes it with a prompt: the
// compaction gate fires against the real transcript, and the run
// reports what the strategy did — the cut, the tokens it recovered,
// what its side requests returned, and the replacement the model reads.
// It is the way a strategy is tested on the work a session actually
// did, rather than on scripted turns.
//
// The chat endpoint is any OpenAI-compatible one; the model's real
// window has to hold the mining request, which reads the covered
// transcript. The header record's limits are rewritten to the window
// the caller names, because a restore resurrects the limits the session
// was made with.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode/quickjs"
	codemodestarlark "github.com/sdougbrown/poo/codemode/starlark"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/profile/code"
	"github.com/sdougbrown/poo/provider/openai"
	"github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
	"github.com/sdougbrown/poo/tool/codemode"
	"github.com/sdougbrown/poo/tool/edit"
	"github.com/sdougbrown/poo/tool/fetch"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/history"
	"github.com/sdougbrown/poo/tool/read"
	"github.com/sdougbrown/poo/tool/write"
)

// strategySettings turns the flags into the settings the config's own
// dispatch reads: the strategy is named, and the cross-strategy flags
// land in that strategy's own fields. One dispatch table - config's -
// builds the strategy, so a replay's options are the ones the shipped
// binary would resolve.
func strategySettings(name string, ratio float64, maxTokens, keepRecent int) *config.Settings {
	settings := &config.Settings{Version: 1}
	named := name
	settings.Context.Strategy = &named
	r := ratio
	m := maxTokens
	k := keepRecent
	switch name {
	case "mine":
		settings.Context.Mine = config.Mine{Ratio: &r, MaxTokens: &m, KeepRecentTokens: &k}
	case "summary":
		settings.Context.Summary = config.Summary{Ratio: &r, MaxTokens: &m, KeepRecentTokens: &k}
	case "pointer":
		settings.Context.Pointer = config.Pointer{Ratio: &r, MaxTokens: &m, KeepRecentTokens: &k}
	case "native", "handoff", "none":
		// The strategies without a compose ask: the ratio flags have
		// nothing to land in, and the dispatch builds them bare.
	default:
		fmt.Println("unknown strategy:", name)
		os.Exit(2)
	}
	return settings
}

// codemodeEngine builds the codemode tool under the named engine: the
// shipped binary's selection, so a replay offers the schema the
// recorded session worked with.
func codemodeEngine(name string) poo.Tool {
	if name == "starlark" {
		return codemode.New(codemodestarlark.New())
	}
	return codemode.New(quickjs.New())
}

// toolInfos describes the linked tools the way the profile names them,
// straight from each tool's own spec. A name the profile offers that
// nothing links fails here, before restore's compatibility check names
// it readably.
func toolInfos(linked map[string]poo.Tool) ([]poo.ToolInfo, error) {
	infos := make([]poo.ToolInfo, 0, len(linked))
	for _, name := range code.Profile().Tools {
		tool, ok := linked[name]
		if !ok {
			return nil, fmt.Errorf("the %s profile offers %q and nothing links it", code.Name, name)
		}
		spec := tool.Spec()
		infos = append(infos, poo.ToolInfo{Name: name, Class: spec.Class, Background: spec.Background, Idempotent: spec.Idempotent})
	}
	return infos, nil
}

func main() {
	logPath := flag.String("log", "", "the session log to restore")
	limit := flag.Int("limit", 128000, "the context limit the restored session runs under")
	endpoint := flag.String("endpoint", "http://grace:4000/v1", "the chat endpoint")
	model := flag.String("model", "deepseek-flash", "the chat model id the endpoint serves")
	prompt := flag.String("prompt", "Continue with the work.", "the prompt that wakes the restored session")
	ratio := flag.Float64("ratio", 0, "the optional fill share the strategy acts at; 0 takes the fit rule")
	keepRecent := flag.Int("keeprecent", 0, "the verbatim tail the strategy keeps; 0 takes the strategy's own default")
	maxTokens := flag.Int("maxtokens", 0, "the strategy's compose output limit; 0 takes the strategy's own default")
	strategy := flag.String("strategy", "pointer", "the compaction strategy: pointer, mine, or summary")
	engine := flag.String("engine", config.DefaultCodemodeEngine, "the codemode engine the replay offers; match the recorded session")
	flag.Parse()
	if *logPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	dir := filepath.Dir(*logPath)
	id := strings.TrimSuffix(filepath.Base(*logPath), ".jsonl")
	store, err := session.NewStore(dir)
	if err != nil {
		fmt.Println("opening the store:", err)
		os.Exit(1)
	}
	loaded, err := store.Load(context.Background(), id)
	if err != nil {
		fmt.Println("loading the session:", err)
		os.Exit(1)
	}
	records := loaded.Records
	// Run the session under the window the caller asked for: the header
	// record's limits are the ones a restore resurrects.
	for _, rec := range records {
		if created, ok := rec.(*poo.SessionCreatedRecord); ok {
			created.ContextLimit = *limit
			if created.Roles.Chat != nil {
				created.Roles.Chat.ContextLimit = *limit
				// The session's model identity is the recorded one:
				// restore sends the header's model in every request,
				// so the flag rewrites it here.
				if *model != "" {
					created.Roles.Chat.ModelID = *model
				}
			}
		}
	}
	fmt.Printf("records: %d\n", len(records))
	var view []poo.Message
	for _, rec := range records {
		if m, ok := rec.(*poo.MessageRecord); ok {
			view = append(view, poo.Message{Role: m.Role, Content: m.Content})
		}
	}
	fmt.Printf("view chars: %d, estimate at ratio 4: %d\n", poo.EstimateHistoryChars(view), poo.EstimateHistoryChars(view)/4)

	sink := pootest.NewSink()
	events := &eventPrinter{}
	// One ModelInfo for both surfaces: the transport stamps its requests
	// with it, and the session sends it in every request. A model whose
	// header records a reasoning effort needs the control on both, or
	// the restored session's first request refuses to send.
	modelInfo := poo.ModelInfo{
		ModelID:      *model,
		Tools:        poo.ToolCalling{Mode: poo.ToolsNative},
		Reasoning:    poo.ReasoningControl{Mode: poo.ReasoningEffort, Param: "reasoning_effort"},
		MaxOutput:    16384,
		ContextLimit: *limit,
	}
	transport := openai.New(openai.Options{
		Endpoint:  *endpoint,
		Model:     *model,
		ModelInfo: modelInfo,
	})
	// The tools the shipped code profile offers, and the profile's own
	// declaration of them: the classes come from the tools' specs, and
	// a name the profile offers that nothing links fails the build with
	// the missing tool named.
	linked := map[string]poo.Tool{
		"read":     read.New(),
		"grep":     grep.New(),
		"history":  history.New(),
		"edit":     edit.New(),
		"write":    write.New(),
		"bash":     bash.New(),
		"fetch":    fetch.New(),
		"codemode": codemodeEngine(*engine),
		"call_cli": cliregistry.NewCall(nil),
	}
	infos, err := toolInfos(linked)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	opts := poo.Options{
		Transport: transport,
		Model:     modelInfo,
		Tools:     linked,
		ToolOrder: code.Profile().Tools,
		Sink:      sink,
		Profile:   "code",
		Profiles: []poo.Profile{{
			Name:      code.Name,
			Tools:     infos,
			Unmatched: code.Profile().Unmatched,
		}},
		Compactor: config.CompactorOf(strategySettings(*strategy, *ratio, *maxTokens, *keepRecent)),
		Observers: []poo.Observer{events},
		// Restore replaces this with the recorded prompt whenever the
		// log holds one, so it applies only to a log without it.
		SystemPrompt: "You are continuing an earlier session. Work from the context you are given.",
	}
	restored, meta, err := poo.Restore(opts, records)
	if err != nil {
		fmt.Println("restoring:", err)
		os.Exit(1)
	}
	defer restored.Close()
	fmt.Printf("restored: session %s, %d messages, %d turns, %d warnings\n", restored.ID(), meta.Messages, meta.Turns, len(meta.Warnings))
	for _, w := range meta.Warnings {
		fmt.Println("  warning:", w)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	start := time.Now()
	if _, err := restored.Prompt(ctx, *prompt); err != nil {
		fmt.Println("prompting:", err)
		os.Exit(1)
	}
	res, err := restored.Wait(ctx)
	if err != nil {
		fmt.Println("waiting:", err)
		os.Exit(1)
	}
	fmt.Printf("settled %s in %s: source %q, text %q\n", res.Reason, time.Since(start).Round(time.Second), res.Detail.Source, res.Detail.Text)

	newRecords := sink.Records()
	kinds := map[string]int{}
	for _, rec := range newRecords {
		kinds[rec.Meta().Record]++
		if u, ok := rec.(*poo.UsageRecord); ok {
			fmt.Printf("usage: input %d output %d turn %d\n", u.InputTokens, u.OutputTokens, u.Turn)
		}
	}
	fmt.Println("run records:", kinds)
	for _, rec := range newRecords {
		cr, ok := rec.(*poo.CompactionRecord)
		if !ok {
			continue
		}
		before, _ := json.Marshal(cr.Compaction.Side)
		fmt.Printf("compaction: strategy %s, form %s, cut %v, tokens %d -> %d\n",
			cr.Strategy, cr.Form, cr.Cut, cr.TokensBefore, cr.TokensAfter)
		fmt.Printf("side: %s\n", string(before))
		for _, m := range cr.Replacement {
			text := m.Text()
			if len(text) > 2400 {
				text = text[:2400] + "…"
			}
			fmt.Printf("--- replacement ---\n%s\n", text)
		}
	}
}

// eventPrinter prints the compaction events a run emits.
type eventPrinter struct{}

// Observe prints the event.
func (p *eventPrinter) Observe(ev poo.Event) error {
	if ev.Name == "compaction.started" || ev.Name == "compaction.ended" {
		payload, _ := json.Marshal(ev.Payload)
		fmt.Printf("event %s: %s\n", ev.Name, payload)
	}
	return nil
}

// HookName names the observer.
func (p *eventPrinter) HookName() string { return "livecompaction" }
