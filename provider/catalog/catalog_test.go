package catalog_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/catalog"
	"github.com/sdougbrown/poo/provider/catalog/internal/genmodelsdev"
)

// generate writes the generated catalog into a file.
func generate(output string) error {
	return genmodelsdev.Generate("testdata/models.dev.json", output)
}

// localSettings returns the settings of a local endpoint that serves
// gemma4.
func localSettings() catalog.Settings {
	return catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{
			"grace": {BaseURL: "http://localhost:4000/v1", APIKeyEnv: "GRACE_KEY"},
		},
		Models: map[string]catalog.ModelSettings{
			"gemma4": {Endpoint: "grace", ModelID: "gemma4"},
		},
	}
}

func TestTheCuratedLayerDescribesTheLocalModel(t *testing.T) {
	c := catalog.New(catalog.Options{Settings: localSettings()})
	info, err := c.Resolve("gemma4")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if info.ContextLimit != 262144 {
		t.Errorf("the context limit is %d, want 262144", info.ContextLimit)
	}
	if info.Tools.Mode != poo.ToolsAuto {
		t.Errorf("the tools mode is %q, want auto", info.Tools.Mode)
	}
	if info.Reasoning.Mode != poo.ReasoningToggle {
		t.Errorf("the reasoning control is %q, want toggle", info.Reasoning.Mode)
	}
	if info.Reasoning.Param != poo.ReasoningPathToggle {
		t.Errorf("the reasoning parameter is %q, want %q", info.Reasoning.Param, poo.ReasoningPathToggle)
	}
	if !info.HasOp(poo.OpChat) || !info.HasOp(poo.OpSystemOne) {
		t.Errorf("the operations are %v, want chat and systemone", info.Ops)
	}
	if info.Endpoint != "http://localhost:4000/v1" {
		t.Errorf("the endpoint is %q, want the settings' base URL", info.Endpoint)
	}
	if info.Source["context_limit"] != catalog.LayerCurated {
		t.Errorf("the context limit comes from %q, want curated", info.Source["context_limit"])
	}
	if info.Source["endpoint"] != catalog.LayerSettings {
		t.Errorf("the endpoint comes from %q, want settings", info.Source["endpoint"])
	}
}

func TestTheFamilyNotesSayWhatTheModelNeeds(t *testing.T) {
	c := catalog.New(catalog.Options{Settings: localSettings()})
	notes := c.Notes("gemma4")
	if len(notes) == 0 {
		t.Fatal("the model has no note")
	}
	if !strings.Contains(notes[0], "tool calls as text") {
		t.Errorf("the note is %q, want it to name the text tool calls", notes[0])
	}
}

func TestTheSettingsLayerWinsFieldByField(t *testing.T) {
	settings := localSettings()
	settings.Models["gemma4"] = catalog.ModelSettings{
		Endpoint:     "grace",
		ModelID:      "gemma4",
		ContextLimit: 32768,
	}
	c := catalog.New(catalog.Options{Settings: settings})
	info, err := c.Resolve("gemma4")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if info.ContextLimit != 32768 {
		t.Errorf("the context limit is %d, want the settings' 32768", info.ContextLimit)
	}
	if info.Source["context_limit"] != catalog.LayerSettings {
		t.Errorf("the context limit comes from %q, want settings", info.Source["context_limit"])
	}
	if info.Source["tools.mode"] != catalog.LayerCurated {
		t.Errorf("the tools mode comes from %q, want curated", info.Source["tools.mode"])
	}
}

func TestTheSamplingOverrideReachesTheModel(t *testing.T) {
	settings := localSettings()
	temperature := 0.2
	settings.Sampling = map[string]poo.Sampling{"gemma4": {Temperature: &temperature}}
	c := catalog.New(catalog.Options{Settings: settings})
	info, err := c.Resolve("gemma4")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if info.Sampling.Temperature == nil || *info.Sampling.Temperature != 0.2 {
		t.Errorf("the temperature is %v, want 0.2", info.Sampling.Temperature)
	}
	if info.Source["sampling.temperature"] != catalog.LayerSettings {
		t.Errorf("the temperature comes from %q, want settings", info.Source["sampling.temperature"])
	}
}

func TestTheExplainAttributesEveryField(t *testing.T) {
	settings := localSettings()
	settings.Models["gemma4"] = catalog.ModelSettings{
		Endpoint:     "grace",
		ModelID:      "gemma4",
		ContextLimit: 32768,
	}
	c := catalog.New(catalog.Options{Settings: settings})
	info, explanations, err := c.Explain("gemma4")
	if err != nil {
		t.Fatalf("the explanation: %v", err)
	}
	want := map[string]string{
		"name":           catalog.LayerSettings,
		"endpoint":       catalog.LayerSettings,
		"model_id":       catalog.LayerSettings,
		"api":            catalog.LayerDefault,
		"ops":            catalog.LayerCurated,
		"context_limit":  catalog.LayerSettings,
		"tools.mode":     catalog.LayerCurated,
		"reasoning.mode": catalog.LayerCurated,
	}
	for _, explanation := range explanations {
		layer, ok := want[explanation.Field]
		if !ok {
			continue
		}
		if explanation.Layer != layer {
			t.Errorf("the %s field comes from %q, want %q", explanation.Field, explanation.Layer, layer)
		}
		delete(want, explanation.Field)
	}
	for field := range want {
		t.Errorf("the explanation does not name the %s field", field)
	}
	if info.Name != "gemma4" {
		t.Errorf("the model is named %q, want gemma4", info.Name)
	}
}

func TestTheHostedModelsAreListed(t *testing.T) {
	c := catalog.New(catalog.Options{Settings: localSettings()})
	var hosted *poo.ModelInfo
	for _, info := range c.List() {
		if info.Name == "gpt-5" {
			model := info
			hosted = &model
		}
	}
	if hosted == nil {
		t.Fatal("the hosted models are not listed")
	}
	if hosted.ContextLimit != 400000 {
		t.Errorf("the context limit is %d, want the generated 400000", hosted.ContextLimit)
	}
	if hosted.Temperature != poo.TempFixed {
		t.Errorf("the temperature control is %q, want fixed", hosted.Temperature)
	}
	if hosted.Reasoning.Mode != poo.ReasoningEffort {
		t.Errorf("the reasoning control is %q, want effort", hosted.Reasoning.Mode)
	}
}

func TestAHostedModelTheSettingsServeIsNotListedTwice(t *testing.T) {
	settings := localSettings()
	settings.Models["fast"] = catalog.ModelSettings{Endpoint: "grace", ModelID: "gpt-5"}
	c := catalog.New(catalog.Options{Settings: settings})
	count := 0
	for _, info := range c.List() {
		if info.ModelID == "gpt-5" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("the model is listed %d times, want once", count)
	}
}

func TestTheCatalogListsTheModelsByName(t *testing.T) {
	settings := localSettings()
	settings.Models["alpha"] = catalog.ModelSettings{Endpoint: "grace", ModelID: "gemma4"}
	c := catalog.New(catalog.Options{Settings: settings})
	var names []string
	for _, info := range c.List() {
		names = append(names, info.Name)
	}
	if len(names) < 3 {
		t.Fatalf("the catalog holds %v, want the settings' models and the hosted ones", names)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("the catalog is not sorted: %v", names)
		}
	}
}

func TestTheChatModelMustResolve(t *testing.T) {
	settings := localSettings()
	settings.Roles = map[string]string{"chat": "missing"}
	c := catalog.New(catalog.Options{Settings: settings})
	err := c.CheckRoles()
	if err == nil {
		t.Fatal("the missing chat model was accepted")
	}
	var unresolved *catalog.UnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("the error is %v, want an unresolved model", err)
	}
	if unresolved.Role != catalog.RoleChat {
		t.Errorf("the error names the %q role, want chat", unresolved.Role)
	}
}

func TestAnUnresolvableDeciderIsOnlyAWarning(t *testing.T) {
	var log bytes.Buffer
	settings := localSettings()
	settings.Roles = map[string]string{"chat": "gemma4", "decider": "missing"}
	c := catalog.New(catalog.Options{
		Settings: settings,
		Logger:   slog.New(slog.NewTextHandler(&log, nil)),
	})
	if err := c.CheckRoles(); err != nil {
		t.Fatalf("the roles: %v", err)
	}
	if !strings.Contains(log.String(), "missing") {
		t.Errorf("the log is %q, want it to name the missing model", log.String())
	}
}

func TestARoleNeedsTheOperation(t *testing.T) {
	settings := localSettings()
	settings.Models["gemma4"] = catalog.ModelSettings{
		Endpoint: "grace",
		ModelID:  "gemma4",
		Ops:      []poo.Op{poo.OpChat},
	}
	settings.Roles = map[string]string{"chat": "gemma4", "decider": "gemma4"}
	c := catalog.New(catalog.Options{Settings: settings})
	err := c.CheckRoles()
	if err == nil {
		t.Fatal("the model without the operation was accepted")
	}
	var opErr *catalog.OpError
	if !errors.As(err, &opErr) {
		t.Fatalf("the error is %v, want an operation error", err)
	}
	if opErr.Role != catalog.RoleDecider {
		t.Errorf("the error names the %q role, want decider", opErr.Role)
	}
	if opErr.Op != poo.OpSystemOne {
		t.Errorf("the error names the %q operation, want systemone", opErr.Op)
	}
}

func TestTheChatRoleNeedsTheChatOperation(t *testing.T) {
	settings := localSettings()
	settings.Models["gemma4"] = catalog.ModelSettings{
		Endpoint: "grace",
		ModelID:  "gemma4",
		Ops:      []poo.Op{poo.OpSystemOne},
	}
	settings.Roles = map[string]string{"chat": "gemma4"}
	c := catalog.New(catalog.Options{Settings: settings})
	err := c.CheckRoles()
	var opErr *catalog.OpError
	if !errors.As(err, &opErr) {
		t.Fatalf("the error is %v, want an operation error", err)
	}
	if opErr.Op != poo.OpChat {
		t.Errorf("the error names the %q operation, want chat", opErr.Op)
	}
}

func TestTheCredentialsAreEnvOrStatic(t *testing.T) {
	settings := localSettings()
	settings.Endpoints["static"] = catalog.EndpointSettings{
		BaseURL: "http://localhost:5000/v1",
		APIKey:  "not-a-real-key",
	}
	settings.Endpoints["open"] = catalog.EndpointSettings{BaseURL: "http://localhost:6000/v1"}
	c := catalog.New(catalog.Options{Settings: settings})
	endpoints := c.Endpoints()
	if got := endpoints["grace"].Credential.Source; got != catalog.CredentialEnv {
		t.Errorf("the credential source is %q, want env", got)
	}
	if got := endpoints["grace"].Credential.Env; got != "GRACE_KEY" {
		t.Errorf("the credential reads %q, want GRACE_KEY", got)
	}
	if got := endpoints["static"].Credential.Source; got != catalog.CredentialStatic {
		t.Errorf("the credential source is %q, want static", got)
	}
	if got := endpoints["open"].Credential.Source; got != catalog.CredentialNone {
		t.Errorf("the credential source is %q, want none", got)
	}
	names := c.CredentialEnvNames()
	if !slices.Contains(names, "GRACE_KEY") {
		t.Errorf("the credential names are %v, want them to hold GRACE_KEY", names)
	}
	if slices.Contains(names, "not-a-real-key") {
		t.Errorf("the credential names are %v, want no static value among them", names)
	}
	if !slices.IsSorted(names) {
		t.Errorf("the credential names are %v, want them sorted", names)
	}
}

func TestTheModelOfAnUnknownEndpointIsDropped(t *testing.T) {
	var log bytes.Buffer
	settings := localSettings()
	settings.Models["orphan"] = catalog.ModelSettings{Endpoint: "nowhere", ModelID: "gemma4"}
	c := catalog.New(catalog.Options{
		Settings: settings,
		Logger:   slog.New(slog.NewTextHandler(&log, nil)),
	})
	if _, err := c.Resolve("orphan"); err == nil {
		t.Fatal("the model of an unknown endpoint was kept")
	}
	if !strings.Contains(log.String(), "orphan") {
		t.Errorf("the log is %q, want it to name the model", log.String())
	}
}

func TestTheCatalogReportsAModelItDoesNotHold(t *testing.T) {
	c := catalog.New(catalog.Options{Settings: localSettings()})
	_, err := c.Resolve("nowhere")
	var unresolved *catalog.UnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("the error is %v, want an unresolved model", err)
	}
}

func TestTheReasoningSettingReachesTheRequest(t *testing.T) {
	settings := localSettings()
	settings.Reasoning = "low"
	c := catalog.New(catalog.Options{Settings: settings})
	fields, err := c.ReasoningFields("gemma4")
	if err != nil {
		t.Fatalf("the fields: %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("the fields are %v, want one", fields)
	}
	if fields[0].Path != poo.ReasoningPathToggle {
		t.Errorf("the field is %q, want %q", fields[0].Path, poo.ReasoningPathToggle)
	}
	if fields[0].Value != true {
		t.Errorf("the value is %v, want true", fields[0].Value)
	}
}

func TestTheReasoningSettingOfAModelThatCannotDoIt(t *testing.T) {
	settings := localSettings()
	settings.Reasoning = "high"
	settings.Models["plain"] = catalog.ModelSettings{
		Endpoint: "grace",
		ModelID:  "plain",
		Reasoning: &poo.ReasoningControl{
			Mode: poo.ReasoningNone,
		},
	}
	c := catalog.New(catalog.Options{Settings: settings})
	_, err := c.ReasoningFields("plain")
	var cfg *poo.ConfigError
	if !errors.As(err, &cfg) {
		t.Fatalf("the error is %v, want a config error", err)
	}
}

func TestTheGeneratedCatalogIsFresh(t *testing.T) {
	dir := t.TempDir()
	generated := filepath.Join(dir, "modelsdev_gen.go")
	if err := generate(generated); err != nil {
		t.Fatalf("the generator: %v", err)
	}
	want, err := os.ReadFile("modelsdev_gen.go")
	if err != nil {
		t.Fatalf("the generated file: %v", err)
	}
	got, err := os.ReadFile(generated)
	if err != nil {
		t.Fatalf("the generated file: %v", err)
	}
	if string(got) != string(want) {
		t.Error("the generated catalog is out of date: run go generate ./provider/catalog")
	}
}

func TestTheBuildChecksTheRoles(t *testing.T) {
	settings := localSettings()
	settings.Roles = map[string]string{"chat": "gemma4"}
	c, err := catalog.Build(context.Background(), catalog.Options{
		Settings: settings,
		StateDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("the catalog: %v", err)
	}
	info, err := c.Role(catalog.RoleChat)
	if err != nil {
		t.Fatalf("the chat model: %v", err)
	}
	if info.Name != "gemma4" {
		t.Errorf("the chat model is %q, want gemma4", info.Name)
	}
}
