// Package codemode implements the codemode tool: the model writes one
// script, the script makes the tool calls, and only the script's output
// and return value reach the model.
//
// The engine is the sandbox; the tool is the seam between the session
// and the engine. The script's calls go through the nested-call seam,
// so the permission engine judges every one of them.
package codemode

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode"
	"github.com/sdougbrown/poo/internal/caps"
	"github.com/sdougbrown/poo/prompt"
)

//go:embed prompt.md
var sectionText string

// section is the guidance the tool brings to the system prompt.
var section = prompt.MustCompile("codemode", sectionText, nil)

// Tool is the codemode tool.
type Tool struct {
	// Engine runs the scripts.
	Engine codemode.Engine
	// Limits override the session's, for a binary that pins them.
	// Zero fields take the session's, or the engine's defaults.
	Limits codemode.Limits
}

// New returns a codemode tool that runs its scripts on the engine.
func New(engine codemode.Engine) *Tool {
	return &Tool{Engine: engine}
}

// Spec describes the tool.
func (t *Tool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name: "codemode",
		Description: "Run a short script whose only capability is calling your other tools. " +
			"Use it when one call's result decides the next call, or when many calls would flood the conversation: the script's intermediate results stay out of the context, and only what it prints and returns comes back.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "script": {"type": "string", "description": "The script. It can call(name, args), gather([(name, args), ...]), print(x) and exit(value)."}
  },
  "required": ["script"]
,"additionalProperties":false}`),
		Class:      poo.ClassSideEffecting,
		PrimaryArg: "script",
	}
}

// args are the tool's arguments.
type args struct {
	Script string `json:"script"`
}

// prepared is a validated script run.
type prepared struct {
	script string
}

// Summary describes the call for a human reader.
func (p *prepared) Summary() string {
	return fmt.Sprintf("%d bytes of script", len(p.script))
}

// Prepare decodes and validates the arguments. It has no side effects.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a args
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, poo.InvalidArgsf("the arguments: %v", err)
	}
	if strings.TrimSpace(a.Script) == "" {
		return nil, poo.InvalidArgsf("the script is empty")
	}
	return &prepared{script: a.Script}, nil
}

// Call runs the script on the engine.
//
// The script's tool calls go through the nested seam, so a refusal or a
// failure is data the script sees, and every call is recorded with the
// mode "nested" and this call's handle as its parent.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	if env.Nested == nil {
		return poo.ToolResult{Text: noSeamText, IsError: true}, nil
	}
	p := in.(*prepared)
	limits := t.Limits
	if limits.TimeoutS == 0 {
		limits.TimeoutS = env.Settings.CodemodeTimeoutS
	}
	if limits.ScriptMaxBytes == 0 {
		limits.ScriptMaxBytes = env.Settings.CodemodeScriptMaxBytes
	}
	if limits.OutputCapChars == 0 {
		limits.OutputCapChars = env.Settings.CodemodeOutputCapChars
	}
	if limits.ExecSteps == 0 {
		limits.ExecSteps = env.Settings.CodemodeExecSteps
	}

	result, err := t.Engine.Run(ctx, codemode.RunOptions{
		Script: p.script,
		Nested: env.Nested,
		Limits: limits,
	})
	text := resultText(result, err)
	cap := env.Settings.ObservationCapChars
	if cap <= 0 {
		cap = caps.ObservationMax
	}
	if utf8.RuneCountInString(text) > cap {
		text = caps.RunePrefix(text, cap) + "\n… the script's output was cut at the context budget"
	}
	return poo.ToolResult{Text: text, IsError: err != nil}, nil
}

// noSeamText is what the model is told when the tool runs without a
// nested caller.
//
// A call the model makes always has one. The seam is missing for a
// script that calls codemode itself, and for a background job: the
// nesting is one call deep, and a job runs outside the turn's tools.
const noSeamText = "codemode runs only as a call the model makes: a nested call or a background job has no tools to call"

// resultText renders what the model sees of a script run: what the
// script did, then why it stopped, if it failed.
//
// A failed run keeps what it did. A script that timed out after an edit
// tells the model the edit happened, so the model does not issue it
// again.
func resultText(result codemode.RunResult, err error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "the script made %d call(s)\n", result.Calls)
	for _, line := range result.Output {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if result.Truncated {
		b.WriteString("the printed output was cut at the output cap\n")
	}
	if result.Value != nil {
		b.WriteString("returned: ")
		b.WriteString(string(result.Value))
		b.WriteByte('\n')
	}
	if err != nil {
		b.WriteString(err.Error())
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// PromptSection renders the tool's own section of the system prompt.
func (t *Tool) PromptSection(env prompt.Env) string {
	return section.Render(env)
}
