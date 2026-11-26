package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/callmcp"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// TestTheBinaryLinksTheMCPCallTheServersNeed covers the binary's half of the
// offering: a profile that selects a server needs the tool that calls its
// tools, and this command links it under the name the settings require.
func TestTheBinaryLinksTheMCPCallTheServersNeed(t *testing.T) {
	tools := linkedTools()
	factory, ok := tools[config.ToolCallMCP]
	if !ok {
		t.Fatalf("the command links %d tools, and not %q", len(tools), config.ToolCallMCP)
	}
	if got := factory(config.ToolOptions{}).Spec().Name; got != config.ToolCallMCP {
		t.Errorf("the tool calls itself %q, and the settings require %q", got, config.ToolCallMCP)
	}
	if _, ok := tools[cliregistry.FindToolName]; !ok {
		t.Errorf("the command links no search to narrow the MCP tools with")
	}
}

// yesDecider answers every question the same way, so a search returns every
// entry it was asked about.
type yesDecider struct{}

// Decide answers yes to every question.
func (yesDecider) Decide(_ context.Context, _ any, _ string, questions []decide.Question) ([]decide.Answer, error) {
	answers := make([]decide.Answer, 0, len(questions))
	for _, question := range questions {
		answers = append(answers, decide.Answer{ID: question.ID, Probabilities: map[string]float64{"yes": 0.9, "no": 0.1}})
	}
	return answers, nil
}

// fixtureMCPServer builds the mcp package's fixture server and returns its path.
func fixtureMCPServer(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "server")
	cmd := exec.Command("go", "build", "-o", binary, "github.com/sdougbrown/poo/mcp/testdata/server")
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the fixture server: %v\n%s", err, out)
	}
	return binary
}

// TestTheBinaryFindsAndCallsAnMCPTool covers the whole path the binary wires:
// a session starts the server its profile selected, the prompt lists the tool
// the server offers, the search returns it with the call that runs it, and the
// call reaches the process the session owns.
//
// The settings' handles are opaque to the build and read by this command's own
// tools, so this is the test that pins the two halves together.
func TestTheBinaryFindsAndCallsAnMCPTool(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(manifest, []byte(`{"mcpServers":{"fixture":{"command":"`+fixtureMCPServer(t)+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	document := "version: 1\n" +
		"endpoints:\n" +
		"  test:\n" +
		"    endpoint: http://127.0.0.1:1/v1\n" +
		"models:\n" +
		"  test-model:\n" +
		"    endpoint: test\n" +
		"    model_id: test-model\n" +
		"    api: chat_completions\n" +
		"    tools_mode: native\n" +
		"    ops: [chat, systemone, decisions]\n" +
		"    context_limit: 32768\n" +
		"roles:\n" +
		"  chat: test-model\n" +
		"  decider: test-model\n" +
		"session:\n" +
		"  dir: " + t.TempDir() + "\n" +
		"mcp:\n" +
		"  paths: [\"" + manifest + "\"]\n" +
		"profiles:\n" +
		"  picky:\n" +
		"    base: code\n" +
		"    mcp_servers: [fixture]\n" +
		"profile: picky\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     dir,
		DefaultProfile: defaultProfile,
		Tools:          linkedTools(),
		Profiles:       linkedProfiles(),
		Executors:      executorRegistry(),
		MCPLauncher:    linkedMCPLauncher(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	defer runtime.Store.Close()
	// The search answers through the session's decider, and a test names the
	// answer rather than running a decision endpoint.
	runtime.Options.Decider = yesDecider{}
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{
			Name:      cliregistry.FindToolName,
			ID:        "c1",
			Arguments: json.RawMessage(`{"intent":"echo the words I give it"}`),
		}}},
		pootest.Turn{Calls: []pootest.Call{{
			Name:      config.ToolCallMCP,
			ID:        "c2",
			Arguments: json.RawMessage(`{"name":"fixture/echo","args":{"text":"hello"}}`),
		}}},
		pootest.Turn{Text: "done"},
	)
	runtime.Options.Transport = transport

	sess, err := runtime.CreateSession(context.Background(), runtime.Store, &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-11-26T12:00:00Z",
			WorkingDir: dir,
			Profile:    "picky", Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	})
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()

	// The catalog is the session's: it is rendered when the session attaches,
	// from the server it started.
	if prompt := sess.PromptState().Text; !strings.Contains(prompt, "fixture/echo") {
		t.Errorf("the prompt does not list the tool the server offers:\n%s", prompt)
	}
	if _, err := sess.Prompt(context.Background(), "echo something"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	text := requestTextOf(t, transport)
	for _, want := range []string{
		// The search's hit named the call and the name to pass.
		"call it with " + callmcp.CallToolName + `, passing "fixture/echo" as "` + callmcp.NameArgument + `".`,
		// The call reached the server the session started.
		`"text":"hello"`,
		dir,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the conversation does not carry %q:\n%s", want, text)
		}
	}
}

// requestTextOf returns the block text of every message the last provider
// request carried, tool results included.
func requestTextOf(t *testing.T, transport *pootest.Transport) string {
	t.Helper()
	requests := transport.Requests()
	if len(requests) == 0 {
		t.Fatal("the model was never asked")
	}
	var text strings.Builder
	for _, message := range requests[len(requests)-1].Messages {
		for _, block := range message.Content {
			text.WriteString(block.Text)
			text.WriteString("\n")
		}
	}
	return text.String()
}
