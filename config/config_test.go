package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// theSettings is a settings document that holds one of everything the
// formats have to agree on.
const theSettings = `
version: 1
profile: code
endpoints:
  grace:
    endpoint: http://grace:4000/v1
    api_key_env: GRACE_KEY
models:
  gemma4:
    endpoint: grace
    model_id: gemma4
    api: chat_completions
    tools_mode: native
    ops: [chat, systemone]
    inputs: [text]
    context_limit: 131072
roles:
  chat: gemma4
  decider: gemma4
decider:
  timeout_ms: 1500
permission:
  unmatched: ask
  fallback: deny
  doom_loop:
    threshold: 3
    near_threshold: 5
    action: ask
  rules:
    - tool: bash
      command_prefix: git status
      action: allow
  askers: [decide]
  chain_timeout_s: 120
  decide:
    allow: 0.85
    deny: 0.99
    timeout_ms: 1500
system_prompt:
  append: more
  sections:
    house: style
  remove: [noise]
context:
  notice_ratio: 0.8
  hard_ratio: 0.97
  strategy: handoff
  handoff:
    ratio: 0.9
    max_tokens: 2048
tools:
  bash:
    timeout_s: 120
    executor: local
  bwrap:
    network: true
    ro_binds: [/usr, /bin]
    rw_binds: []
  env:
    scrub: [AWS_*]
    passthrough: [PATH]
  protected_paths: [/etc/shadow]
limits:
  max_tokens_per_run: 0
  max_duration_s: 0
  max_turns: 12
  max_continuations: 3
jobs:
  max_running: 4
  timeout_s: 1800
  on_idle: wake
  max_unattended_wakes: 0
reasoning:
  chat: off
sampling:
  gemma4:
    temperature: 1.0
    top_p: 0.95
cli_registry:
  paths: [./registry]
session:
  mode: logged
  dir: ~/.local/state/poo/sessions
  project_prompt: true
  user_prompt: true
  retention:
    max_age_days: 30
    max_sessions: 500
`

// theTOML is the same settings in TOML.
const theTOML = `
version = 1
profile = "code"

[endpoints.grace]
endpoint = "http://grace:4000/v1"
api_key_env = "GRACE_KEY"

[models.gemma4]
endpoint = "grace"
model_id = "gemma4"
api = "chat_completions"
tools_mode = "native"
ops = ["chat", "systemone"]
inputs = ["text"]
context_limit = 131072

[roles]
chat = "gemma4"
decider = "gemma4"

[decider]
timeout_ms = 1500

[permission]
unmatched = "ask"
fallback = "deny"
askers = ["decide"]
chain_timeout_s = 120

[permission.doom_loop]
threshold = 3
near_threshold = 5
action = "ask"

[permission.decide]
allow = 0.85
deny = 0.99
timeout_ms = 1500

[[permission.rules]]
tool = "bash"
command_prefix = "git status"
action = "allow"

[system_prompt]
append = "more"
remove = ["noise"]

[system_prompt.sections]
house = "style"

[context]
notice_ratio = 0.8
hard_ratio = 0.97
strategy = "handoff"

[context.handoff]
ratio = 0.9
max_tokens = 2048

[tools]
protected_paths = ["/etc/shadow"]

[tools.bash]
timeout_s = 120
executor = "local"

[tools.bwrap]
network = true
ro_binds = ["/usr", "/bin"]
rw_binds = []

[tools.env]
scrub = ["AWS_*"]
passthrough = ["PATH"]

[limits]
max_tokens_per_run = 0
max_duration_s = 0
max_turns = 12
max_continuations = 3

[jobs]
max_running = 4
timeout_s = 1800
on_idle = "wake"
max_unattended_wakes = 0

[reasoning]
chat = "off"

[sampling.gemma4]
temperature = 1.0
top_p = 0.95

[cli_registry]
paths = ["./registry"]

[session]
mode = "logged"
dir = "~/.local/state/poo/sessions"
project_prompt = true
user_prompt = true

[session.retention]
max_age_days = 30
max_sessions = 500
`

// theJSON is the same settings in JSON.
const theJSON = `{
  "version": 1,
  "profile": "code",
  "endpoints": {
    "grace": {"endpoint": "http://grace:4000/v1", "api_key_env": "GRACE_KEY"}
  },
  "models": {
    "gemma4": {
      "endpoint": "grace",
      "model_id": "gemma4",
      "api": "chat_completions",
      "tools_mode": "native",
      "ops": ["chat", "systemone"],
      "inputs": ["text"],
      "context_limit": 131072
    }
  },
  "roles": {"chat": "gemma4", "decider": "gemma4"},
  "decider": {"timeout_ms": 1500},
  "permission": {
    "unmatched": "ask",
    "fallback": "deny",
    "doom_loop": {"threshold": 3, "near_threshold": 5, "action": "ask"},
    "rules": [{"tool": "bash", "command_prefix": "git status", "action": "allow"}],
    "askers": ["decide"],
    "chain_timeout_s": 120,
    "decide": {"allow": 0.85, "deny": 0.99, "timeout_ms": 1500}
  },
  "system_prompt": {"append": "more", "sections": {"house": "style"}, "remove": ["noise"]},
  "context": {
    "notice_ratio": 0.8,
    "hard_ratio": 0.97,
    "strategy": "handoff",
    "handoff": {"ratio": 0.9, "max_tokens": 2048}
  },
  "tools": {
    "bash": {"timeout_s": 120, "executor": "local"},
    "bwrap": {"network": true, "ro_binds": ["/usr", "/bin"], "rw_binds": []},
    "env": {"scrub": ["AWS_*"], "passthrough": ["PATH"]},
    "protected_paths": ["/etc/shadow"]
  },
  "limits": {"max_tokens_per_run": 0, "max_duration_s": 0, "max_turns": 12, "max_continuations": 3},
  "jobs": {"max_running": 4, "timeout_s": 1800, "on_idle": "wake", "max_unattended_wakes": 0},
  "reasoning": {"chat": "off"},
  "sampling": {"gemma4": {"temperature": 1, "top_p": 0.95}},
  "cli_registry": {"paths": ["./registry"]},
  "session": {
    "mode": "logged",
    "dir": "~/.local/state/poo/sessions",
    "project_prompt": true,
    "user_prompt": true,
    "retention": {"max_age_days": 30, "max_sessions": 500}
  }
}
`

// TestTheFormatsNormalizeToTheSameJSON covers the formats: the same
// settings in JSON, TOML and YAML are the same settings, and they
// normalize to the same JSON.
func TestTheFormatsNormalizeToTheSameJSON(t *testing.T) {
	yamlJSON, err := config.Normalize([]byte(theSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("normalizing the YAML: %v", err)
	}
	tomlJSON, err := config.Normalize([]byte(theTOML), config.FormatTOML)
	if err != nil {
		t.Fatalf("normalizing the TOML: %v", err)
	}
	jsonJSON, err := config.Normalize([]byte(theJSON), config.FormatJSON)
	if err != nil {
		t.Fatalf("normalizing the JSON: %v", err)
	}
	if string(yamlJSON) != string(tomlJSON) {
		t.Errorf("the TOML normalizes to\n%s\nand the YAML to\n%s", tomlJSON, yamlJSON)
	}
	if string(yamlJSON) != string(jsonJSON) {
		t.Errorf("the JSON normalizes to\n%s\nand the YAML to\n%s", jsonJSON, yamlJSON)
	}
}

// TestTheFormatsDecodeToTheSameSettings covers the formats again: the
// decoded settings are the same, whatever the format was.
func TestTheFormatsDecodeToTheSameSettings(t *testing.T) {
	fromYAML, err := config.Parse([]byte(theSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the YAML: %v", err)
	}
	fromTOML, err := config.Parse([]byte(theTOML), config.FormatTOML)
	if err != nil {
		t.Fatalf("decoding the TOML: %v", err)
	}
	fromJSON, err := config.Parse([]byte(theJSON), config.FormatJSON)
	if err != nil {
		t.Fatalf("decoding the JSON: %v", err)
	}
	if fromYAML.Roles["chat"] != fromTOML.Roles["chat"] || fromYAML.Roles["chat"] != fromJSON.Roles["chat"] {
		t.Errorf("the formats name different chat models: %v, %v, %v",
			fromYAML.Roles, fromTOML.Roles, fromJSON.Roles)
	}
	if fromYAML.Models["gemma4"].ContextLimit != fromTOML.Models["gemma4"].ContextLimit {
		t.Errorf("the formats name different windows: %d and %d",
			fromYAML.Models["gemma4"].ContextLimit, fromTOML.Models["gemma4"].ContextLimit)
	}
}

// TestAnUnknownKeyIsRejected covers the strict decode: a key the
// settings do not have is a failure, and the failure names the key.
func TestAnUnknownKeyIsRejected(t *testing.T) {
	_, err := config.Parse([]byte("version: 1\ncolour: blue\n"), config.FormatYAML)
	if err == nil {
		t.Fatal("the settings decoded, want a failure")
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeUnknownField {
		t.Fatalf("the failure is %v, want an unknown field", err)
	}
	field, ok := config.FieldOf(err)
	if !ok || field != "colour" {
		t.Errorf("the failure names %q, want colour", field)
	}
}

// TestAnUnavailableFieldIsNamedWithItsChallenge covers the generated
// check: a field the settings set but the other settings do not allow
// is a failure that names the field and explains itself.
func TestAnUnavailableFieldIsNamedWithItsChallenge(t *testing.T) {
	_, err := config.Parse([]byte("version: 1\npermission:\n  decide:\n    allow: 0.9\n"), config.FormatYAML)
	if err == nil {
		t.Fatal("the settings decoded, want a failure")
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeUnavailable {
		t.Fatalf("the failure is %v, want an unavailable field", err)
	}
	field, ok := config.FieldOf(err)
	if !ok || field != "permission.decide.allow" {
		t.Errorf("the failure names %q, want permission.decide.allow", field)
	}
	var settingsErr *config.Error
	if !asError(err, &settingsErr) {
		t.Fatalf("the failure is %T, want a settings failure", err)
	}
	if len(settingsErr.Challenge) == 0 {
		t.Error("the failure carries no challenge")
	}
	joined := strings.Join(settingsErr.Challenge, "\n")
	if !strings.Contains(joined, "decider") {
		t.Errorf("the challenge does not mention the decider: %q", joined)
	}
}

// TestThePromptFieldsAreMutuallyExclusive covers the second rule the
// generated check carries: the base file and the replacement are
// exclusive, and the settings that hold both are refused.
func TestThePromptFieldsAreMutuallyExclusive(t *testing.T) {
	_, err := config.Parse([]byte("version: 1\nsystem_prompt:\n  base_file: a.md\n  replace: text\n"), config.FormatYAML)
	if err == nil {
		t.Fatal("the settings decoded, want a failure")
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeUnavailable {
		t.Fatalf("the failure is %v, want an unavailable field", err)
	}
}

// TestTheDecideAskerNeedsADecider covers the rule the schema carries:
// the asker chain names the decide asker, and a decider model has to be
// there to answer it. It is the field set but unavailable, the way the
// decide thresholds without a decider are.
func TestTheDecideAskerNeedsADecider(t *testing.T) {
	_, err := config.Parse([]byte("version: 1\npermission:\n  askers: [decide]\n"), config.FormatYAML)
	if err == nil {
		t.Fatal("the settings decoded, want a failure")
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeUnavailable {
		t.Fatalf("the failure is %v, want an unavailable field", err)
	}
	field, ok := config.FieldOf(err)
	if !ok || field != "permission.askers" {
		t.Errorf("the failure names %q, want permission.askers", field)
	}
	var settingsErr *config.Error
	if !asError(err, &settingsErr) {
		t.Fatalf("the failure is %T, want a settings failure", err)
	}
	if joined := strings.Join(settingsErr.Challenge, "\n"); !strings.Contains(joined, "decider") {
		t.Errorf("the challenge does not mention the decider: %q", joined)
	}
}

// TestTheDefaultCodemodeEngineRefusesExecSteps covers the limit the
// default engine cannot honor: a settings file that names no engine gets
// QuickJS, so exec_steps is refused there too, and starlark is the engine
// that takes it.
func TestTheDefaultCodemodeEngineRefusesExecSteps(t *testing.T) {
	_, err := config.Parse([]byte("version: 1\ntools:\n  codemode:\n    exec_steps: 100\n"), config.FormatYAML)
	if err == nil {
		t.Fatal("the settings decoded, want the default engine's refusal")
	}
	if field, ok := config.FieldOf(err); !ok || field != "tools.codemode.exec_steps" {
		t.Fatalf("the failure names %q (%v), want tools.codemode.exec_steps", field, err)
	}
	// The named engine reaches the same check, and the arm that refuses
	// must still refuse it.
	if _, err := config.Parse([]byte("version: 1\ntools:\n  codemode:\n    engine: quickjs\n    exec_steps: 100\n"), config.FormatYAML); err == nil {
		t.Fatal("a settings file naming quickjs decoded with an exec_steps limit")
	} else if field, ok := config.FieldOf(err); !ok || field != "tools.codemode.exec_steps" {
		t.Fatalf("the failure names %q (%v), want tools.codemode.exec_steps", field, err)
	}
	if _, err := config.Parse([]byte("version: 1\ntools:\n  codemode:\n    engine: starlark\n    exec_steps: 100\n"), config.FormatYAML); err != nil {
		t.Fatalf("starlark refused exec_steps: %v", err)
	}
	// Zero is how a settings file says the limit is off, so the default
	// engine takes it.
	if _, err := config.Parse([]byte("version: 1\ntools:\n  codemode:\n    exec_steps: 0\n"), config.FormatYAML); err != nil {
		t.Fatalf("the default engine refused a zero exec_steps: %v", err)
	}
}

// TestARuleNeedsOneMatcher covers the rules the settings check by
// hand: a rule matches on a tool or on a class, never on both.
func TestARuleNeedsOneMatcher(t *testing.T) {
	_, err := config.Parse([]byte("version: 1\npermission:\n  rules:\n    - tool: bash\n      class: side_effecting\n      action: allow\n"), config.FormatYAML)
	if err == nil {
		t.Fatal("the settings decoded, want a failure")
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeInvalid {
		t.Fatalf("the failure is %v, want an invalid value", err)
	}
}

// TestAContainsRuleNeedsOneMatcherAndASubstring covers the rules the
// settings check by hand for a contains rule: it names a tool, it
// carries no prefix beside it, and its substrings are not empty.
func TestAContainsRuleNeedsOneMatcherAndASubstring(t *testing.T) {
	tests := []struct {
		name    string
		setting string
	}{
		{
			name:    "a contains substring on a class rule",
			setting: "version: 1\npermission:\n  rules:\n    - class: side_effecting\n      command_contains: [\"rm -rf\"]\n      action: ask\n",
		},
		{
			name:    "a contains substring beside a prefix",
			setting: "version: 1\npermission:\n  rules:\n    - tool: bash\n      command_prefix: git\n      command_contains: [\"rm -rf\"]\n      action: ask\n",
		},
		{
			name:    "an empty substring",
			setting: "version: 1\npermission:\n  rules:\n    - tool: bash\n      command_contains: [\"\"]\n      action: ask\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Parse([]byte(tt.setting), config.FormatYAML)
			if err == nil {
				t.Fatal("the settings decoded, want a failure")
			}
			code, ok := config.CodeOf(err)
			if !ok || code != config.CodeInvalid {
				t.Fatalf("the failure is %v, want an invalid value", err)
			}
		})
	}
}

// TestTheGeneratedFileIsFresh covers the generated file: the generator
// and the committed file are the same.
func TestTheGeneratedFileIsFresh(t *testing.T) {
	if testing.Short() {
		t.Skip("the generator is not run in the short mode")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "settings_gen.go")
	cmd := exec.Command("go", "run", "github.com/umpire-tools/umpire-go-gen@v0.3.1",
		"-i", "schema/settings.umpire.json", "-output-file", out, "-pkg", "config")
	cmd.Dir = "."
	if output, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(output), "dial tcp") || strings.Contains(string(output), "module lookup disabled") {
			t.Skipf("the generator is not available: %s", output)
		}
		t.Fatalf("running the generator: %v\n%s", err, output)
	}
	generated, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading the generated file: %v", err)
	}
	committed, err := os.ReadFile("settings_gen.go")
	if err != nil {
		t.Fatalf("reading the committed file: %v", err)
	}
	if string(generated) != string(committed) {
		t.Error("the generated file is not fresh: run go generate ./config")
	}
}

// asError is errors.As, without the import dance in every test.
func asError(err error, target **config.Error) bool {
	for err != nil {
		if e, ok := err.(*config.Error); ok {
			*target = e
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// TestTheExampleSettingsDecode covers the example: the settings file
// the repository ships decodes, so it stays in step with the settings.
func TestTheExampleSettingsDecode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "examples", "code.settings.yaml"))
	if err != nil {
		t.Fatalf("reading the example: %v", err)
	}
	settings, err := config.Parse(data, config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the example: %v", err)
	}
	if settings.Profile != "code" {
		t.Errorf("the profile is %q, want code", settings.Profile)
	}
	if settings.Roles["chat"] == "" {
		t.Error("the example names no chat model")
	}
}

// TestEveryExampleSettingsDecode covers the examples: every settings
// file the repository ships decodes, so they stay in step with the
// settings as the schema moves.
func TestEveryExampleSettingsDecode(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "examples", "*.settings.yaml"))
	if err != nil {
		t.Fatalf("listing the examples: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("the examples hold no settings files")
	}
	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("reading the example: %v", err)
			}
			if _, err := config.Parse(data, config.FormatYAML); err != nil {
				t.Fatalf("decoding the example: %v", err)
			}
		})
	}
}

// TestAnOldRegistryKeyStillLoads covers the cliregistry → cli_registry
// rename: a settings file that predates the rename loads to the same
// settings the new name gives, a value that merely names the tool
// survives untouched, and a file that names both fails loudly.
func TestAnOldRegistryKeyStillLoads(t *testing.T) {
	fromOld, err := config.Parse([]byte("version: 1\ncliregistry:\n  paths: [\"./registry\"]\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("the old key did not load: %v", err)
	}
	fromNew, err := config.Parse([]byte("version: 1\ncli_registry:\n  paths: [\"./registry\"]\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("the new key did not load: %v", err)
	}
	if strings.Join(fromOld.CLIRegistry.Paths, ",") != strings.Join(fromNew.CLIRegistry.Paths, ",") {
		t.Errorf("the old key loaded %v, want %v", fromOld.CLIRegistry.Paths, fromNew.CLIRegistry.Paths)
	}
	valued, err := config.Parse([]byte("version: 1\ncliregistry:\n  paths: [\"cliregistry\", \"tool/cliregistry\"]\nsystem_prompt:\n  replace: \"mentions cliregistry in prose\"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("a value naming the tool did not load: %v", err)
	}
	if got := strings.Join(valued.CLIRegistry.Paths, ","); got != "cliregistry,tool/cliregistry" {
		t.Errorf("the paths came out %q, want the word intact", got)
	}
	if valued.SystemPrompt.Replace != "mentions cliregistry in prose" {
		t.Errorf("the value came out %q, want the word intact", valued.SystemPrompt.Replace)
	}
	both, err := config.Parse([]byte("version: 1\ncliregistry:\n  paths: [\"./a\"]\ncli_registry:\n  paths: [\"./b\"]\n"), config.FormatYAML)
	if err == nil {
		t.Fatalf("the settings named both keys and loaded: %+v", both)
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeUnknownField {
		t.Errorf("naming both keys failed with %v, want an unknown field", err)
	}
}

// TestAnOldBaseURLKeyStillLoads covers the base_url → endpoint rename on
// an endpoint: a settings file that predates the rename loads to the
// settings the new name gives, and a file that names both fails loudly.
func TestAnOldBaseURLKeyStillLoads(t *testing.T) {
	// The entry's other keys survive the rename, and a value that holds
	// the old name is a value, not a key.
	fromOld, err := config.Parse([]byte("version: 1\nendpoints:\n  grace:\n    base_url: \"http://grace:4000/v1\"\n    api_key_env: \"base_url\"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("the old key did not load: %v", err)
	}
	fromNew, err := config.Parse([]byte("version: 1\nendpoints:\n  grace:\n    endpoint: \"http://grace:4000/v1\"\n    api_key_env: \"base_url\"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("the new key did not load: %v", err)
	}
	if !reflect.DeepEqual(fromOld.Endpoints["grace"], fromNew.Endpoints["grace"]) {
		t.Errorf("the old key loaded %+v, want %+v", fromOld.Endpoints["grace"], fromNew.Endpoints["grace"])
	}
	both, err := config.Parse([]byte("version: 1\nendpoints:\n  grace:\n    base_url: \"http://a:4000/v1\"\n    endpoint: \"http://b:4000/v1\"\n"), config.FormatYAML)
	if err == nil {
		t.Fatalf("the settings named both keys and loaded: %+v", both)
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeUnknownField {
		t.Errorf("naming both keys failed with %v, want an unknown field", err)
	}
	// Outside an endpoint the old name is not renamed: a base_url key
	// anywhere else stays unknown.
	if _, err := config.Parse([]byte("version: 1\nbase_url: \"http://a:4000/v1\"\n"), config.FormatYAML); err == nil {
		t.Error("a base_url key outside the endpoints loaded")
	}
}

// TestAnUnknownTOMLKeyIsRejected covers the strict decode in the TOML
// path: the formats normalize to the same JSON, and the same unknown
// key is refused.
func TestAnUnknownTOMLKeyIsRejected(t *testing.T) {
	_, err := config.Parse([]byte("version = 1\ncolour = \"blue\"\n"), config.FormatTOML)
	if err == nil {
		t.Fatal("the TOML settings decoded, want a failure")
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeUnknownField {
		t.Fatalf("the failure is %v, want an unknown field", err)
	}
	field, ok := config.FieldOf(err)
	if !ok || field != "colour" {
		t.Errorf("the failure names %q, want colour", field)
	}
}

// TestASetZeroIsChecked covers the generated minimum rules: a numeric
// field the settings set to zero is a value, and the rule refuses it.
func TestASetZeroIsChecked(t *testing.T) {
	for _, body := range []string{
		"version: 1\npermission:\n  chain_timeout_s: 0\n",
		"version: 1\ndecider:\n  timeout_ms: 0\n",
		"version: 1\npermission:\n  doom_loop:\n    threshold: 0\n",
		"version: 1\npermission:\n  doom_loop:\n    near_threshold: 0\n",
		"version: 1\npermission:\n  decide:\n    timeout_ms: 0\n",
	} {
		if _, err := config.Parse([]byte(body), config.FormatYAML); err == nil {
			t.Errorf("the settings %q decoded, want the minimum rule to refuse it", body)
		}
	}
}

// TestAnUnknownCompactionStrategyIsRefused covers the generated check on
// context.strategy: a name the build does not implement is refused, so
// a session never runs with no compactor because of a typo.
func TestAnUnknownCompactionStrategyIsRefused(t *testing.T) {
	_, err := config.Parse([]byte("version: 1\ncontext:\n  strategy: summarize\n"), config.FormatYAML)
	if err == nil {
		t.Fatal("the settings decoded, want a failure")
	}
	field, ok := config.FieldOf(err)
	if !ok || field != "context.strategy" {
		t.Fatalf("the failure names %q, want context.strategy", field)
	}
	var settingsErr *config.Error
	if !asError(err, &settingsErr) {
		t.Fatalf("the failure is %T, want a settings failure", err)
	}
	if joined := strings.Join(settingsErr.Challenge, "\n"); !strings.Contains(joined, "summary") {
		t.Errorf("the challenge does not name the strategies: %q", joined)
	}
}

// TestASummaryRatioOutOfRangeIsRefused covers the rule the generated
// check carries for the summary strategy: a ratio above one would never
// reach the trigger, so the strategy would be silently disabled.
func TestASummaryRatioOutOfRangeIsRefused(t *testing.T) {
	_, err := config.Parse([]byte("version: 1\ncontext:\n  strategy: summary\n  summary:\n    ratio: 5\n"), config.FormatYAML)
	if err == nil {
		t.Fatal("the settings decoded, want a failure")
	}
	field, ok := config.FieldOf(err)
	if !ok || field != "context.summary.ratio" {
		t.Fatalf("the failure names %q, want context.summary.ratio", field)
	}
}
