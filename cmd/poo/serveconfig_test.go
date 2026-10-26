package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The serve command takes its sessions from the settings: the
// handshake reports the profiles the settings build, and the prompt
// overrides a client sends are applied through the config package.

// serveClient drives the serve command over its pipes.
type serveClient struct {
	// in is the client's end of the server's input.
	in *os.File
	// out is the server's end of the client's output.
	out *os.File
	// reader reads the server's answers.
	reader *bufio.Reader
	// next is the id of the next request.
	next int
}

// startServe runs the serve command and returns a client for it.
//
// The client is closed when the test ends, and the server's exit code
// is the one the command returned.
func startServe(t *testing.T, args ...string) *serveClient {
	t.Helper()
	stdin, stdout := os.Stdin, os.Stdout
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("the input pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("the output pipe: %v", err)
	}
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() {
		os.Stdin, os.Stdout = stdin, stdout
		inR.Close()
		outR.Close()
	})

	codes := make(chan int, 1)
	go func() {
		codes <- runServe(args)
	}()
	t.Cleanup(func() {
		inW.Close()
		select {
		case <-codes:
		case <-time.After(30 * time.Second):
			t.Error("the serve command did not stop")
		}
	})
	return &serveClient{in: inW, out: outR, reader: bufio.NewReader(outR)}
}

// call sends a request and returns the answer.
func (c *serveClient) call(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	c.next++
	id := c.next
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	line, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}
	if _, err := c.in.Write(append(line, '\n')); err != nil {
		t.Fatalf("writing the request: %v", err)
	}
	for {
		answer, err := c.read(t)
		if err != nil {
			t.Fatalf("reading the answer: %v", err)
		}
		if _, ok := answer["id"]; ok {
			return answer
		}
	}
}

// read reads one message from the server.
func (c *serveClient) read(t *testing.T) (map[string]any, error) {
	t.Helper()
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var msg map[string]any
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// result returns the result of an answer, and fails when the answer is
// a failure.
func result(t *testing.T, answer map[string]any) map[string]any {
	t.Helper()
	if failure, ok := answer["error"]; ok {
		t.Fatalf("the request was refused: %v", failure)
	}
	res, ok := answer["result"].(map[string]any)
	if !ok {
		t.Fatalf("the answer holds no result: %v", answer)
	}
	return res
}

// TestTheHandshakeReportsTheSettingsProfiles covers the handshake: the
// profiles the settings build are the ones the client is told about,
// with the tools they offer.
func TestTheHandshakeReportsTheSettingsProfiles(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
	settings := theSettings(t, dir, endpoint.URL+"/v1")

	client := startServe(t, "--settings", settings, "--session-dir", filepath.Join(dir, "sessions"))
	answer := client.call(t, "poo/initialize", map[string]any{
		"protocol_versions": []int{1},
		"client":            map[string]any{"name": "test", "version": "test"},
	})
	handshake := result(t, answer)

	profiles, _ := handshake["profiles"].([]any)
	byName := map[string]map[string]any{}
	for _, entry := range profiles {
		profile, _ := entry.(map[string]any)
		name, _ := profile["name"].(string)
		byName[name] = profile
	}
	code, ok := byName["code"]
	if !ok {
		t.Fatalf("the handshake holds no code profile: %v", byName)
	}
	tools, _ := code["tools"].([]any)
	names := []string{}
	for _, entry := range tools {
		tool, _ := entry.(map[string]any)
		name, _ := tool["name"].(string)
		names = append(names, name)
	}
	for _, want := range []string{"bash", "read", "grep", "edit", "jobs", "cancel_job"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the code profile offers %v, want the %q tool", names, want)
		}
	}
}

// TestTheServeOffersTheTypedResultTool covers the tool: the settings
// name a schema for the result, and the session offers the model a way
// to finish.
func TestTheServeOffersTheTypedResultTool(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(dir, "result.schema.json")
	if err := os.WriteFile(schema, []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatalf("writing the schema: %v", err)
	}
	settings := filepath.Join(dir, "settings.json")
	body := `{"version":1,
		"endpoints":{"test":{"base_url":"http://localhost:4000/v1"}},
		"models":{"test-model":{"endpoint":"test","model_id":"test-model","api":"chat_completions","tools_mode":"native","ops":["chat"],"context_limit":32768}},
		"roles":{"chat":"test-model"},
		"finish":{"schema_file":"` + schema + `"}}`
	if err := os.WriteFile(settings, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}

	client := startServe(t, "--settings", settings, "--session-dir", filepath.Join(dir, "sessions"))
	client.call(t, "poo/initialize", map[string]any{
		"protocol_versions": []int{1},
		"client":            map[string]any{"name": "test", "version": "test"},
	})
	answer := client.call(t, "session/create", map[string]any{"working_dir": dir})
	created := result(t, answer)
	tools, _ := created["tools"].([]any)
	found := false
	for _, entry := range tools {
		tool, _ := entry.(map[string]any)
		if tool["name"] == "finish" {
			found = true
		}
	}
	if !found {
		t.Errorf("the session offers %v, want the finish tool", tools)
	}
}

// TestThePromptOverridesReachTheSession covers the change a client
// asks for: the prompt overrides are applied through the config
// package, and the session's prompt shows them.
func TestThePromptOverridesReachTheSession(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
	settings := theSettings(t, dir, endpoint.URL+"/v1")

	client := startServe(t, "--settings", settings, "--session-dir", filepath.Join(dir, "sessions"))
	client.call(t, "poo/initialize", map[string]any{
		"protocol_versions": []int{1},
		"client":            map[string]any{"name": "test", "version": "test"},
	})
	answer := client.call(t, "session/create", map[string]any{"working_dir": dir})
	created := result(t, answer)
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("the session has no id: %v", created)
	}

	answer = client.call(t, "session/configure", map[string]any{
		"session_id": sessionID,
		"prompt_overrides": map[string]any{
			"sections": map[string]any{"house": "the house style"},
		},
	})
	result(t, answer)

	answer = client.call(t, "session/get", map[string]any{"session_id": sessionID})
	state := result(t, answer)
	prompt, _ := state["prompt"].(map[string]any)
	text, _ := prompt["text"].(string)
	if !strings.Contains(text, "the house style") {
		t.Errorf("the prompt is %q, want the client's section", text)
	}
}
