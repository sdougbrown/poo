package callmcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/mcp"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/callmcp"
)

// fakeServer is one MCP server a test started: it lists the tools the test
// names and answers the way the test asks.
type fakeServer struct {
	// name is the server's declared name.
	name string
	// tools are the tools it lists.
	tools []mcp.Tool
	// result is what a call answers.
	result mcp.CallResult
	// err is the failure a call returns, when it is set.
	err error
	// calls are the tools the calls named, in order.
	calls []string
}

// Name is the server's declared name.
func (s *fakeServer) Name() string { return s.name }

// Tools returns the tools the server lists.
func (s *fakeServer) Tools() []mcp.Tool { return s.tools }

// CallTool answers for the named tool.
//
// A tool the server does not list reports the client's own unknown-tool
// failure, the way the real one does.
func (s *fakeServer) CallTool(_ context.Context, tool string, _ json.RawMessage) (mcp.CallResult, error) {
	if !s.holds(tool) {
		return mcp.CallResult{}, fmt.Errorf("%w: %s/%s", mcp.ErrUnknownTool, s.name, tool)
	}
	s.calls = append(s.calls, tool)
	if s.err != nil {
		return mcp.CallResult{}, s.err
	}
	return s.result, nil
}

// holds reports whether the server lists the tool.
func (s *fakeServer) holds(tool string) bool {
	for _, listed := range s.tools {
		if listed.Name == tool {
			return true
		}
	}
	return false
}

// tool is one MCP tool a server lists.
func tool(name, description string, readOnly bool) mcp.Tool {
	return mcp.Tool{
		Name:        name,
		Description: description,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		Annotations: mcp.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: !readOnly},
	}
}

// newTool returns the tool over the servers a session started, listing the
// entries their tools become.
//
// The session's snapshot holds the same entries, which is what the search over
// them reads.
func newTool(servers ...callmcp.Server) *callmcp.Tool {
	return callmcp.New(servers, callmcp.Offered(servers))
}

// call prepares and runs one call_mcp call.
func call(t *testing.T, tool *callmcp.Tool, args string, env poo.ToolEnv) (poo.ToolResult, error) {
	t.Helper()
	prepared, err := tool.Prepare(json.RawMessage(args), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing %s: %v", args, err)
	}
	return tool.Call(context.Background(), prepared, env)
}

// TestTheCallIsSideEffectingAndNamed covers the spec: a call runs a tool that
// may change anything, so it is side-effecting with the qualified name as its
// primary argument, and it may leave the turn as a background job.
func TestTheCallIsSideEffectingAndNamed(t *testing.T) {
	spec := callmcp.New(nil, nil).Spec()
	if spec.Name != callmcp.CallToolName {
		t.Errorf("the tool calls itself %q, want %q", spec.Name, callmcp.CallToolName)
	}
	if spec.Class != poo.ClassSideEffecting {
		t.Errorf("the call is %q, want side-effecting", spec.Class)
	}
	if spec.PrimaryArg != callmcp.NameArgument {
		t.Errorf("the primary argument is %q, want %q", spec.PrimaryArg, callmcp.NameArgument)
	}
	if spec.Background != poo.BackgroundAllowed {
		t.Errorf("the call is %q, want it allowed in the background", spec.Background)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(spec.Schema, &schema); err != nil {
		t.Fatalf("the schema is not JSON: %v", err)
	}
	if len(schema.Required) != 1 || schema.Required[0] != callmcp.NameArgument {
		t.Errorf("the schema requires %q, want the tool's name", schema.Required)
	}
}

// TestPrepareValidatesTheQualifiedName covers the argument check: the name is
// a tool's qualified name, and nothing else is accepted in its place.
func TestPrepareValidatesTheQualifiedName(t *testing.T) {
	server := &fakeServer{name: "fixture", tools: []mcp.Tool{tool("echo", "answers", true)}}
	toolCalled := newTool(server)
	for _, args := range []string{
		`{}`,
		`{"name":""}`,
		`{"name":"fixture"}`,
		`{"name":"/echo"}`,
		`{"name":"fixture/"}`,
		`{"name":"  /  "}`,
		`{"name":"fixture/echo","args":null}`,
		`{"name":"fixture/echo","args":[]}`,
		`{"name":"fixture/echo","args":"text"}`,
		`{"name":"fixture/echo","extra":true}`,
		`{"name":"fixture/echo"} {"name":"fixture/echo"}`,
	} {
		if _, err := toolCalled.Prepare(json.RawMessage(args), poo.PrepareEnv{}); err == nil {
			t.Errorf("%s prepared a call", args)
		}
	}
	// An omitted args is an empty object: a tool that takes nothing is called
	// with its name alone.
	if _, err := toolCalled.Prepare(json.RawMessage(`{"name":" fixture/echo "}`), poo.PrepareEnv{}); err != nil {
		t.Errorf("a qualified name with padding was refused: %v", err)
	}
	if len(server.calls) != 0 {
		t.Errorf("preparing called the server %q", server.calls)
	}
}

// TestAnUnknownToolPointsAtWhereTheNamesAre covers the refusal: a name no
// server lists is the model's to correct, and the correction is the search when
// the session offers one and the prompt's catalog when it does not.
func TestAnUnknownToolPointsAtWhereTheNamesAre(t *testing.T) {
	server := &fakeServer{name: "fixture", tools: []mcp.Tool{tool("echo", "answers", true)}}
	for _, tc := range []struct {
		name string
		tool *callmcp.Tool
		want string
	}{
		{name: "searched", tool: newTool(server).WithSearch(true), want: "find_tool"},
		{name: "listed", tool: newTool(server), want: "MCP catalog in the prompt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := call(t, tc.tool, `{"name":"fixture/nope"}`, poo.ToolEnv{})
			var invalid *poo.ErrInvalidArgs
			if !errors.As(err, &invalid) {
				t.Fatalf("the refusal is %T (%v), want an invalid call", err, err)
			}
			for _, want := range []string{"fixture/nope", tc.want} {
				if !strings.Contains(invalid.Message, want) {
					t.Errorf("the refusal %q does not name %q", invalid.Message, want)
				}
			}
			if strings.Contains(invalid.Message, "is down") {
				t.Errorf("an unknown tool reads as a stopped server: %q", invalid.Message)
			}
		})
	}
}

// TestAnUnknownServerNamesTheServer covers the other half of an unknown name:
// the session started no server by that name, and the call says so.
func TestAnUnknownServerNamesTheServer(t *testing.T) {
	server := &fakeServer{name: "fixture", tools: []mcp.Tool{tool("echo", "answers", true)}}
	_, err := call(t, newTool(server), `{"name":"elsewhere/echo"}`, poo.ToolEnv{})
	if err == nil || !strings.Contains(err.Error(), `"elsewhere"`) {
		t.Fatalf("the refusal is %v, want it to name the server", err)
	}
}

// TestAStoppedServerIsAResult covers the server that ended: the session keeps
// running, so the model reads that the server is down instead of a failed
// call, and a later call may succeed.
func TestAStoppedServerIsAResult(t *testing.T) {
	server := &fakeServer{name: "fixture", tools: []mcp.Tool{tool("echo", "answers", true)}, err: fmt.Errorf("%w: fixture/echo", mcp.ErrServerDown)}
	res, err := call(t, newTool(server), `{"name":"fixture/echo"}`, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("the stopped server came back as an error: %v", err)
	}
	if !res.IsError || res.Text != "server fixture is down" {
		t.Errorf("the model reads %q (an error: %v), want the server named as down", res.Text, res.IsError)
	}
}

// TestATransportFailureIsTheCallsOwnError covers the failure that is neither
// the tool's nor the server's: a transport that broke mid-call reached the
// server, so the call may have had effects, and it is reported as the call's
// own error rather than as a result the model can retry. The call is made
// once: a repeat would be a second effect the model never asked for.
func TestATransportFailureIsTheCallsOwnError(t *testing.T) {
	broken := errors.New("the transport broke")
	server := &fakeServer{name: "fixture", tools: []mcp.Tool{tool("echo", "answers", true)}, err: broken}
	res, err := call(t, newTool(server), `{"name":"fixture/echo"}`, poo.ToolEnv{})
	if err == nil {
		t.Fatalf("a transport failure came back as the result %+v", res)
	}
	if !errors.Is(err, broken) {
		t.Errorf("the failure %v does not carry the cause", err)
	}
	if !strings.Contains(err.Error(), callmcp.CallToolName) {
		t.Errorf("the failure %q does not name the call", err)
	}
	if len(server.calls) != 1 {
		t.Errorf("the server was called %d times, want one delivery for one call", len(server.calls))
	}
}

// TestTheToolsOwnFailureIsTheResult covers a tool that ran and answered that
// it failed: the call reached the server, so the answer is the result.
func TestTheToolsOwnFailureIsTheResult(t *testing.T) {
	server := &fakeServer{
		name:   "fixture",
		tools:  []mcp.Tool{tool("fail", "answers with the protocol's error flag", true)},
		result: mcp.CallResult{IsError: true, Text: []string{"the tool refused"}},
	}
	res, err := call(t, newTool(server), `{"name":"fixture/fail"}`, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("the tool's own failure came back as an error: %v", err)
	}
	if !res.IsError || res.Text != "the tool refused" {
		t.Errorf("the model reads %q (an error: %v), want the tool's own answer", res.Text, res.IsError)
	}
}

// TestTheTextContentsJoin covers the answer: the text contents are the answer,
// in the order the server returned them.
func TestTheTextContentsJoin(t *testing.T) {
	server := &fakeServer{
		name:   "fixture",
		tools:  []mcp.Tool{tool("echo", "answers", true)},
		result: mcp.CallResult{Text: []string{"first", "second"}},
	}
	res, err := call(t, newTool(server), `{"name":"fixture/echo"}`, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if res.Text != "first\nsecond" {
		t.Errorf("the model reads %q, want the two contents joined", res.Text)
	}
	if len(server.calls) != 1 || server.calls[0] != "echo" {
		t.Errorf("the server was called with %q, want the tool's own name", server.calls)
	}
}

// TestTheStructuredContentFitsOrIsLeftOut covers the side channel: it rides
// beside the text when it fits under the observation cap, and a larger one is
// left out whole with a line saying so, because a structured value cut in half
// is not the value the server returned.
func TestTheStructuredContentFitsOrIsLeftOut(t *testing.T) {
	structured := json.RawMessage(`{"count":1}`)
	server := &fakeServer{
		name:   "fixture",
		tools:  []mcp.Tool{tool("echo", "answers", true)},
		result: mcp.CallResult{Text: []string{"one"}, Structured: structured},
	}
	res, err := call(t, newTool(server), `{"name":"fixture/echo"}`, poo.ToolEnv{
		Settings: poo.ToolSettings{ObservationCapChars: 100},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if string(res.Structured) != string(structured) {
		t.Errorf("the structured content is %q, want %q", res.Structured, structured)
	}

	big := json.RawMessage(`{"text":"` + strings.Repeat("x", 200) + `"}`)
	server.result.Structured = big
	res, err = call(t, newTool(server), `{"name":"fixture/echo"}`, poo.ToolEnv{
		Settings: poo.ToolSettings{ObservationCapChars: 100},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if res.Structured != nil {
		t.Errorf("the structured content %q was kept past the cap", res.Structured)
	}
	if !strings.Contains(res.Text, "structured content") || !strings.Contains(res.Text, "left out") {
		t.Errorf("the model is not told the structured content was dropped: %q", res.Text)
	}
}

// TestInvalidStructuredContentIsLeftOut covers the other reason a structured
// value is dropped: it is not JSON, so it is not the value the server said it
// returned.
func TestInvalidStructuredContentIsLeftOut(t *testing.T) {
	server := &fakeServer{
		name:   "fixture",
		tools:  []mcp.Tool{tool("echo", "answers", true)},
		result: mcp.CallResult{Text: []string{"one"}, Structured: json.RawMessage(`{"count":`)},
	}
	res, err := call(t, newTool(server), `{"name":"fixture/echo"}`, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if res.Structured != nil {
		t.Errorf("malformed structured content was kept: %q", res.Structured)
	}
	if !strings.Contains(res.Text, "not JSON") {
		t.Errorf("the model is not told why the structured content is missing: %q", res.Text)
	}
}

// TestTheTextIsCapped covers the cap: an answer longer than the model may read
// keeps its head and its tail, and the middle is elided with the count.
func TestTheTextIsCapped(t *testing.T) {
	answer := strings.Repeat("a", 400) + "MIDDLE" + strings.Repeat("b", 400)
	server := &fakeServer{
		name:   "fixture",
		tools:  []mcp.Tool{tool("echo", "answers", true)},
		result: mcp.CallResult{Text: []string{answer}},
	}
	res, err := call(t, newTool(server), `{"name":"fixture/echo"}`, poo.ToolEnv{
		Settings: poo.ToolSettings{ObservationCapChars: 200},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if strings.Contains(res.Text, "MIDDLE") {
		t.Errorf("the middle of the answer was kept: %q", res.Text)
	}
	if !strings.HasPrefix(res.Text, "aaa") || !strings.HasSuffix(res.Text, "bbb") {
		t.Errorf("the answer's head or tail was cut: %q", res.Text)
	}
	if !strings.Contains(res.Text, "elided") {
		t.Errorf("the answer does not say it was elided: %q", res.Text)
	}
	if len([]rune(res.Text)) > 200+120 {
		t.Errorf("the elision kept %d characters, far past the cap", len([]rune(res.Text)))
	}
}

// TestAnImageReachesAModelThatReadsOne covers the attachment: an image is an
// attachment for a model that reads images, and a line of text for one that
// does not.
func TestAnImageReachesAModelThatReadsOne(t *testing.T) {
	image := mcp.CallImage{MediaType: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}}
	server := &fakeServer{
		name:   "fixture",
		tools:  []mcp.Tool{tool("shot", "takes a screenshot", true)},
		result: mcp.CallResult{Images: []mcp.CallImage{image}},
	}
	toolCalled := newTool(server)

	res, err := call(t, toolCalled, `{"name":"fixture/shot"}`, poo.ToolEnv{
		Model: poo.ModelInfo{Inputs: []poo.Modality{poo.ModalityText, poo.ModalityImage}},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if len(res.Attachments) != 1 {
		t.Fatalf("a model that reads images was given %d attachments", len(res.Attachments))
	}
	if res.Attachments[0].Modality != poo.ModalityImage || res.Attachments[0].MediaType != "image/png" {
		t.Errorf("the attachment is %+v, want the image the server returned", res.Attachments[0])
	}

	// The tool's own answer is the fallback for a caller that names no model.
	built := newTool(server).WithInputs(poo.ModalityImage)
	if res, err := call(t, built, `{"name":"fixture/shot"}`, poo.ToolEnv{}); err != nil || len(res.Attachments) != 1 {
		t.Errorf("a caller with no model lost the tool's own answer: %+v (%v)", res, err)
	}

	// A model that reads text alone is told what the tool returned instead.
	res, err = call(t, toolCalled, `{"name":"fixture/shot"}`, poo.ToolEnv{
		Model: poo.ModelInfo{Inputs: []poo.Modality{poo.ModalityText}},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if len(res.Attachments) != 0 {
		t.Errorf("a text-only model was given %d images", len(res.Attachments))
	}
	for _, want := range []string{"image/png", "does not accept image input"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("the note %q does not name %q", res.Text, want)
		}
	}
}

// TestAnImageTypeProvidersRefuseIsLeftOut covers the type: an image whose media
// type no provider carries is left out with a line, rather than handed to a
// block layer that would drop it silently.
func TestAnImageTypeProvidersRefuseIsLeftOut(t *testing.T) {
	server := &fakeServer{
		name:   "fixture",
		tools:  []mcp.Tool{tool("shot", "takes a screenshot", true)},
		result: mcp.CallResult{Images: []mcp.CallImage{{MediaType: "image/tiff", Data: []byte{1, 2}}}},
	}
	res, err := call(t, newTool(server), `{"name":"fixture/shot"}`, poo.ToolEnv{
		Model: poo.ModelInfo{Inputs: []poo.Modality{poo.ModalityText, poo.ModalityImage}},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if len(res.Attachments) != 0 {
		t.Errorf("an image no provider carries was attached")
	}
	if !strings.Contains(res.Text, `"image/tiff"`) || !strings.Contains(res.Text, "left out") {
		t.Errorf("the note %q does not name the type that was left out", res.Text)
	}
}

// TestAnImageTooLargeForAProviderIsLeftOut covers the size: an image larger
// than the bytes a provider takes is left out with a line, the way a feed
// image over the same cap is refused.
func TestAnImageTooLargeForAProviderIsLeftOut(t *testing.T) {
	server := &fakeServer{
		name:   "fixture",
		tools:  []mcp.Tool{tool("shot", "takes a screenshot", true)},
		result: mcp.CallResult{Images: []mcp.CallImage{{MediaType: "image/png", Data: make([]byte, poo.MaxImageBytes+1)}}},
	}
	res, err := call(t, newTool(server), `{"name":"fixture/shot"}`, poo.ToolEnv{
		Model: poo.ModelInfo{Inputs: []poo.Modality{poo.ModalityText, poo.ModalityImage}},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if len(res.Attachments) != 0 {
		t.Errorf("an image larger than a provider takes was attached")
	}
	for _, want := range []string{"image/png", "larger than", "left out"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("the note %q does not name %q", res.Text, want)
		}
	}
}

// TestTheCatalogListsWhatTheSessionCanCall covers the prompt: the section
// names each tool by its qualified name with its arguments and the server's own
// hint, and a session with no server carries no catalog.
func TestTheCatalogListsWhatTheSessionCanCall(t *testing.T) {
	listed := newTool(
		&fakeServer{name: "fixture", tools: []mcp.Tool{
			tool("echo", "answers with what it is given", false),
			tool("count", "reads the counter", true),
		}},
		&fakeServer{name: "notes", tools: []mcp.Tool{tool("add", "adds a note", false)}},
	)
	section := listed.PromptSection(prompt.Env{})
	for _, want := range []string{
		"fixture/count (args: text*) (read-only): reads the counter",
		"fixture/echo (args: text*) (destructive): answers with what it is given",
		"notes/add (args: text*) (destructive): adds a note",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the section does not carry %q:\n%s", want, section)
		}
	}
	// The qualified names come in one order whatever the servers listed.
	if strings.Index(section, "fixture/count") > strings.Index(section, "notes/add") {
		t.Errorf("the listing is not in name order:\n%s", section)
	}
	if !strings.Contains(section, callmcp.CallToolName) {
		t.Errorf("the section does not say what calls a tool:\n%s", section)
	}

	empty := callmcp.New(nil, nil)
	if text := empty.PromptSection(prompt.Env{}); strings.Contains(text, "MCP tools:") {
		t.Errorf("a session with no server carries a catalog: %q", text)
	}
	if text := empty.PromptSection(prompt.Env{}); text == "" {
		t.Error("a session with no server carries no guidance")
	}
}

// TestALongOfferingIsLeftToTheSearch covers the prompt's other shape: past the
// threshold a session with a search says so and lists nothing, and a session
// without one lists the tools whatever their number.
func TestALongOfferingIsLeftToTheSearch(t *testing.T) {
	tools := make([]mcp.Tool, 0, 40)
	for i := 0; i < 40; i++ {
		tools = append(tools, tool(fmt.Sprintf("tool-%03d", i), "does the job", false))
	}
	server := &fakeServer{name: "fixture", tools: tools}

	searched := newTool(server).WithSearch(true).PromptSection(prompt.Env{})
	if !strings.Contains(searched, "too many to list") || !strings.Contains(searched, "find_tool") {
		t.Errorf("the section does not leave the offering to the search:\n%s", searched)
	}
	if strings.Contains(searched, "fixture/tool-000") {
		t.Errorf("the section lists an offering it should search:\n%s", searched)
	}

	listed := newTool(server).PromptSection(prompt.Env{})
	if strings.Contains(listed, "too many to list") {
		t.Errorf("the section names a search the session does not offer:\n%s", listed)
	}
	if !strings.Contains(listed, "fixture/tool-000") {
		t.Errorf("the section does not list the offering it cannot search:\n%s", listed)
	}
}

// TestTheToolDeclaresTheMoveCapability covers the rule: call_mcp is a
// tool whose running call may move to a session job, so the repl's /bg
// keys on the core's backgroundable rule for it.
func TestTheToolDeclaresTheMoveCapability(t *testing.T) {
	mover, ok := any(newTool(&fakeServer{name: "files", tools: []mcp.Tool{tool("grep", "a find", true)}})).(poo.BackgroundableCaller)
	if !ok {
		t.Fatal("call_mcp does not implement BackgroundableCaller")
	}
	if !mover.CanBackgroundRunningCall() {
		t.Error("CanBackgroundRunningCall is false, want true")
	}
}
