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
	codemodestarlark "github.com/sdougbrown/poo/codemode/starlark"
	"github.com/sdougbrown/poo/compact/mine"
	"github.com/sdougbrown/poo/pootest"
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
)

func main() {
	logPath := flag.String("log", "", "the session log to restore")
	limit := flag.Int("limit", 128000, "the context limit the restored session runs under")
	endpoint := flag.String("endpoint", "http://grace:4000/v1", "the chat endpoint")
	model := flag.String("model", "deepseek-flash", "the chat model id the endpoint serves")
	prompt := flag.String("prompt", "Continue with the work.", "the prompt that wakes the restored session")
	ratio := flag.Float64("ratio", 0.85, "the fill share the mine strategy acts at")
	keepRecent := flag.Int("keeprecent", 0, "the verbatim tail the strategy keeps; 0 takes the strategy's own default")
	maxTokens := flag.Int("maxtokens", 0, "the mining request's output limit; 0 takes the strategy's own default")
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

	transport := openai.New(openai.Options{
		Endpoint: *endpoint,
		Model:    *model,
		ModelInfo: poo.ModelInfo{
			ModelID:      *model,
			Tools:        poo.ToolCalling{Mode: poo.ToolsNative},
			Reasoning:    poo.ReasoningControl{Mode: poo.ReasoningEffort, Param: "reasoning_effort"},
			MaxOutput:    16384,
			ContextLimit: *limit,
		},
	})
	sink := pootest.NewSink()
	events := &eventPrinter{}
	opts := poo.Options{
		Transport: transport,
		Model: poo.ModelInfo{
			ModelID:      *model,
			Tools:        poo.ToolCalling{Mode: poo.ToolsNative},
			MaxOutput:    16384,
			ContextLimit: *limit,
		},
		Tools: map[string]poo.Tool{
			"read":     read.New(),
			"grep":     grep.New(),
			"history":  history.New(),
			"edit":     edit.New(),
			"bash":     bash.New(),
			"fetch":    fetch.New(),
			"codemode": codemode.New(codemodestarlark.New()),
			"call_cli": cliregistry.NewCall(nil),
		},
		ToolOrder: []string{"bash", "read", "grep", "history", "edit", "fetch", "codemode", "call_cli"},
		Sink:      sink,
		Profile:   "code",
		Profiles: []poo.Profile{{
			Name: "code",
			Tools: []poo.ToolInfo{
				{Name: "bash", Class: poo.ClassSideEffecting},
				{Name: "read", Class: poo.ClassReadOnly},
				{Name: "grep", Class: poo.ClassReadOnly},
				{Name: "edit", Class: poo.ClassSideEffecting},
				{Name: "fetch", Class: poo.ClassReadOnly},
				{Name: "codemode", Class: poo.ClassReadOnly},
				{Name: "call_cli", Class: poo.ClassSideEffecting},
			},
			Unmatched: "allow",
		}},
		Compactor:    mine.New(mine.Options{Ratio: *ratio, MaxTokens: *maxTokens, KeepRecentTokens: *keepRecent}),
		Observers:    []poo.Observer{events},
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
