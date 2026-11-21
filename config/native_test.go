package config_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/native"
	"github.com/sdougbrown/poo/config"
)

// buildFailure builds a runtime and returns the failure the settings cause.
func buildFailure(t *testing.T, settings *config.Settings) error {
	t.Helper()
	_, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	return err
}

// TestTheSettingsChooseTheNativeCompactionStrategy covers the explicit
// strategy on a model and transport that can serve it: the session gets the
// native compactor, not the summary default.
func TestTheSettingsChooseTheNativeCompactionStrategy(t *testing.T) {
	dir := t.TempDir()
	token := fakeCodexToken(t, "acct_live")
	writeCodexAuth(t, dir, token, "rt_live", 4102444800000)
	path := writeCodexSettings(t, dir, `version: 1
pi_auth_file: auth.json
session:
  mode: ephemeral
models:
  openai-codex/gpt-6.1-sol:
    endpoint: openai-codex
    model_id: gpt-6.1-sol
    api: responses
    ops: [chat, compact]
roles:
  chat: openai-codex/gpt-6.1-sol
context:
  strategy: native
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	endpoint := &codexEndpoint{accessToken: token}
	runtime, err := buildCodex(t, settings, &http.Client{Transport: endpoint})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	strategy, ok := runtime.Options.Compactor.(*native.Compactor)
	if !ok {
		t.Fatalf("the session's compactor is %T, want the native strategy", runtime.Options.Compactor)
	}
	if got := strategy.Ratio(); got != 0 {
		t.Errorf("the native strategy's ratio is %v, want zero for no ratio trigger", got)
	}
	if _, ok := runtime.Options.Transport.(poo.NativeCompactor); !ok {
		t.Errorf("the runtime's transport is %T, want one that can compact natively", runtime.Options.Transport)
	}
	// The settings entry adds the operation and keeps the built-in model's
	// limits: declaring compaction does not require restating the model.
	if runtime.Options.Model.ContextLimit <= 0 {
		t.Errorf("the chat model's window is %d, want the built-in model's limit", runtime.Options.Model.ContextLimit)
	}
}

// TestTheNativeStrategyRefusesAModelWithoutTheCompactOperation covers the
// settings check: the resolved model must declare the operation before a
// session is built, and the failure names the setting to change.
func TestTheNativeStrategyRefusesAModelWithoutTheCompactOperation(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`
context:
  strategy: native
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	err = buildFailure(t, settings)
	if err == nil {
		t.Fatal("the build accepted the native strategy for a model that does not declare compaction")
	}
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the failure is %T, want a settings failure", err)
	}
	if failure.Field != "context.strategy" {
		t.Errorf("the failure names %q, want context.strategy", failure.Field)
	}
	if !strings.Contains(failure.Message, "compact") || !strings.Contains(failure.Message, "test-model") {
		t.Errorf("the failure is %q, want it to name the model and the compact operation", failure.Message)
	}
}

// TestTheNativeStrategyRefusesACodexModelWithoutTheCompactOperation covers the
// model check on its own. The Codex transport can compact the provider's own
// state, so only the missing model operation can refuse this build. The failure
// names the setting to change.
func TestTheNativeStrategyRefusesACodexModelWithoutTheCompactOperation(t *testing.T) {
	dir := t.TempDir()
	token := fakeCodexToken(t, "acct_live")
	writeCodexAuth(t, dir, token, "rt_live", 4102444800000)
	path := writeCodexSettings(t, dir, `version: 1
pi_auth_file: auth.json
session:
  mode: ephemeral
models:
  openai-codex/gpt-6.1-sol:
    endpoint: openai-codex
    model_id: gpt-6.1-sol
    api: responses
    ops: [chat]
roles:
  chat: openai-codex/gpt-6.1-sol
context:
  strategy: native
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	runtime, err := buildCodex(t, settings, &http.Client{Transport: &codexEndpoint{accessToken: token}})
	if err == nil {
		t.Fatalf("the build accepted the native strategy on %T, a transport that can compact natively, "+
			"for a model that does not declare compaction", runtime.Options.Transport)
	}
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the failure is %T, want a settings failure", err)
	}
	if failure.Field != "context.strategy" {
		t.Errorf("the failure names %q, want context.strategy", failure.Field)
	}
	// The transport's capability check has its own failure; this build has to
	// fail on the model's operation, naming the field that would add it.
	if !strings.Contains(failure.Message, "does not declare") ||
		!strings.Contains(failure.Message, "models.openai-codex/gpt-6.1-sol.ops") {
		t.Errorf("the failure is %q, want it to name the model's missing compact operation", failure.Message)
	}
}

// TestTheNativeStrategyRefusesATransportWithoutTheCapability covers the other
// half: a model that declares compaction is still refused when its transport
// cannot produce a provider window.
func TestTheNativeStrategyRefusesATransportWithoutTheCapability(t *testing.T) {
	body := strings.Replace(theModelSettings, "ops: [chat]", "ops: [chat, compact]", 1) + `
context:
  strategy: native
`
	settings, err := config.Parse([]byte(body), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	err = buildFailure(t, settings)
	if err == nil {
		t.Fatal("the build accepted the native strategy on a transport that cannot compact")
	}
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the failure is %T, want a settings failure", err)
	}
	if failure.Field != "context.strategy" {
		t.Errorf("the failure names %q, want context.strategy", failure.Field)
	}
	if !strings.Contains(failure.Message, "native") || !strings.Contains(failure.Message, "openai-codex") {
		t.Errorf("the failure is %q, want it to name native compaction and the provider that supports it", failure.Message)
	}
}

// TestTheNativeStrategyBuildsAPromptWithoutAChatModel covers the prompt-only
// build: a command that renders the prompt resolves no chat model, so it
// resolves no transport either, and the native strategy is the session's
// business. The build keeps the strategy the settings asked for instead of
// refusing a capability check it has no model to answer.
func TestTheNativeStrategyBuildsAPromptWithoutAChatModel(t *testing.T) {
	settings, err := config.Parse([]byte(`
version: 1
roles:
  chat: absent-model
context:
  strategy: native
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildWith(t, config.BuildOptions{
		Settings:    settings,
		WorkingDir:  t.TempDir(),
		NoChatModel: true,
	})
	if runtime.Options.Transport != nil {
		t.Errorf("the runtime's transport is %T, want none for a build that resolved no chat model",
			runtime.Options.Transport)
	}
	if _, ok := runtime.Options.Compactor.(*native.Compactor); !ok {
		t.Errorf("the runtime's compactor is %T, want the native strategy", runtime.Options.Compactor)
	}
	// The prompt is what the build exists for here, and it renders with no
	// model and no transport.
	if text := poo.PromptBuilder(runtime.Options).Render(); text == "" {
		t.Error("the prompt is empty, want the rendered prompt a prompt-only build produces")
	}
}
