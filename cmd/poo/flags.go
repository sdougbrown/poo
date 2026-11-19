package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/provider/catalog"
)

// sharedFlags are the flags every command takes.
//
// The settings are a command's configuration, and the flags change what
// the settings say. The precedence is the profile, then the settings
// file, then the flags: the last one that says something wins.
type sharedFlags struct {
	// settingsFile is the settings file the command reads.
	settingsFile string
	// systemPrompt is the file holding the prompt's base section.
	systemPrompt string
	// appendSystemPrompt is the text appended to the prompt's base
	// section.
	appendSystemPrompt string
	// permissionUnmatched is what to do with a side-effecting call no
	// rule covers.
	permissionUnmatched string
	// sessionDir is the directory the session files live in.
	sessionDir string
	// userPromptFile is the file the user's own instructions are read
	// from.
	userPromptFile string
	// ephemeral runs the command without writing a session.
	ephemeral bool
	// profile is the profile the session is built from.
	profile string
	// feedsPaths are feed manifest paths added to the settings' paths.
	feedsPaths []string
	// cliRegistryPaths are cli registry manifest paths added to the
	// settings' paths.
	cliRegistryPaths []string
}

// sharedFlagSpecs declares the flags every command takes, in the order
// the help prints them.
func sharedFlagSpecs() []clicontract.Flag {
	return []clicontract.Flag{
		{Name: "settings", Kind: clicontract.String, Usage: "The settings file: JSON, TOML or YAML"},
		{Name: "system-prompt", Kind: clicontract.String, Usage: "The file holding the system prompt's base section"},
		{Name: "append-system-prompt", Kind: clicontract.String, Usage: "Text appended to the system prompt, or @file"},
		{Name: "permission-unmatched", Kind: clicontract.String, Usage: "What to do with a side-effecting call no rule covers: allow, ask or deny"},
		{Name: "session-dir", Kind: clicontract.String, Usage: "The directory the session files live in"},
		{Name: "user-prompt-file", Kind: clicontract.String, Usage: "The file holding the user's own instructions"},
		{Name: "ephemeral", Kind: clicontract.Bool, Usage: "Run without writing a session"},
		{Name: "profile", Kind: clicontract.String, Usage: "The profile to run under: base, code or search"},
		{Name: "feeds-path", Kind: clicontract.Repeat, Usage: "Additional feed manifest path (repeatable)"},
		{Name: "cli-registry-path", Kind: clicontract.Repeat, Usage: "Additional cli registry manifest path (repeatable)"},
	}
}

// bind reads the shared flags out of a parsed invocation.
//
// An empty repeated value adds no path, as an empty entry in the matching
// environment variable does, so it never resolves to the working
// directory.
func (f *sharedFlags) bind(inv clicontract.Invocation) {
	f.settingsFile = inv.Flags["settings"]
	f.systemPrompt = inv.Flags["system-prompt"]
	f.appendSystemPrompt = inv.Flags["append-system-prompt"]
	f.permissionUnmatched = inv.Flags["permission-unmatched"]
	f.sessionDir = inv.Flags["session-dir"]
	f.userPromptFile = inv.Flags["user-prompt-file"]
	f.ephemeral = inv.Bools["ephemeral"]
	f.profile = inv.Flags["profile"]
	f.feedsPaths = nonEmpty(inv.Repeated["feeds-path"])
	f.cliRegistryPaths = nonEmpty(inv.Repeated["cli-registry-path"])
}

// nonEmpty drops the empty values from a repeated flag.
func nonEmpty(values []string) []string {
	var kept []string
	for _, value := range values {
		if value != "" {
			kept = append(kept, value)
		}
	}
	return kept
}

// defaultUserPromptFile points to AGENTS.md in poo's platform config directory.
// A flag or setting can name a different file.
//
// A binary that keeps its instructions elsewhere sets BuildOptions'
// UserPromptFile to its own path, and one that wants no default leaves
// it empty.
func defaultUserPromptFile() string {
	dir, err := config.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "AGENTS.md")
}

// settings reads the settings file and returns the settings.
//
// The file is found the way every command finds it: an explicit
// --settings file, the environment's file, the project's, the user's;
// a command that finds none runs on the defaults, and the flags
// change them.
func (f *sharedFlags) settings(workingDir string) (*config.Settings, error) {
	_, settings, err := resolveSettings(f.settingsFile, workingDir)
	return settings, err
}

// resolveSettings finds and loads the settings a command runs under:
// the explicit file a flag or the environment names, then discovery
// walking up from the working directory, then the defaults. The path
// comes back with the settings, for the commands that hand it to a
// child.
func resolveSettings(explicit, workingDir string) (string, *config.Settings, error) {
	path, err := config.FindSettings(explicit, workingDir)
	if err != nil {
		return "", nil, err
	}
	if path == "" {
		return "", &config.Settings{Version: 1}, nil
	}
	settings, err := config.Load(path)
	return path, settings, err
}

// overrides returns the overrides the flags make.
//
// The appended prompt may name a file, so the overrides are read and
// the failure is the caller's.
func (f *sharedFlags) overrides() (config.Overrides, error) {
	appended, err := f.appendedPrompt()
	if err != nil {
		return config.Overrides{}, err
	}
	systemPrompt := ""
	if f.systemPrompt != "" {
		systemPrompt, err = config.ResolveSettingPath("", f.systemPrompt)
		if err != nil {
			return config.Overrides{}, fmt.Errorf("resolving --system-prompt %s: %w", f.systemPrompt, err)
		}
	}
	overrides := config.Overrides{
		Profile:             f.profile,
		PermissionUnmatched: f.permissionUnmatched,
		SessionDir:          f.sessionDir,
		UserPromptFile:      f.userPromptFile,
		SystemPrompt:        systemPrompt,
		AppendSystemPrompt:  appended,
		FeedsPaths:          f.feedsPaths,
		CLIRegistryPaths:    f.cliRegistryPaths,
	}
	if f.ephemeral {
		overrides.SessionMode = "ephemeral"
	}
	return overrides, nil
}

// appendedPrompt returns the text the --append-system-prompt flag
// carries.
//
// The flag takes text, or @file for the text in a file.
func (f *sharedFlags) appendedPrompt() (string, error) {
	if f.appendSystemPrompt == "" {
		return "", nil
	}
	if !strings.HasPrefix(f.appendSystemPrompt, "@") {
		return f.appendSystemPrompt, nil
	}
	path := strings.TrimPrefix(f.appendSystemPrompt, "@")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), nil
}

// checkUnmatched checks the value of the --permission-unmatched flag.
func (f *sharedFlags) checkUnmatched() error {
	switch f.permissionUnmatched {
	case "", "allow", "ask", "deny":
		return nil
	default:
		return errors.New("--permission-unmatched must be allow, ask or deny")
	}
}

// workingDirectory returns the directory the tools work in.
//
// The directory has to exist: a typo would otherwise create an empty
// directory and run the task in it.
func workingDirectory(dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("--working-dir %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--working-dir %s is not a directory", abs)
	}
	return abs, nil
}

// endpointFlags are the flags that describe the endpoint and the model
// a command talks to.
//
// The flags build a settings document, and the flags come last: they
// change what the settings file said.
type endpointFlags struct {
	// endpoint is the model's endpoint.
	endpoint string
	// modelID is the model identifier.
	modelID string
	// contextLimit is the model's context window in tokens.
	contextLimit int
	// apiKeyEnv is the environment variable holding the API key.
	apiKeyEnv string
	// toolsMode is how the run asks for tool calls.
	toolsMode string
}

// endpointFlagSpecs declares the flags that describe the endpoint and the
// model a command talks to, in the order the help prints them.
func endpointFlagSpecs() []clicontract.Flag {
	return []clicontract.Flag{
		{Name: "endpoint", Kind: clicontract.String, Usage: "The model's endpoint, e.g. http://localhost:4000/v1"},
		{Name: "model-id", Kind: clicontract.String, Usage: "The model identifier"},
		{Name: "context-limit", Kind: clicontract.Int, Usage: "The model's context window in tokens"},
		{Name: "api-key-env", Kind: clicontract.String, Usage: "The environment variable holding the API key"},
		{Name: "tools-mode", Kind: clicontract.String, Usage: "How the run asks for tool calls: native, text or auto"},
	}
}

// bind reads the endpoint flags out of a parsed invocation.
func (f *endpointFlags) bind(inv clicontract.Invocation) {
	f.endpoint = inv.Flags["endpoint"]
	f.modelID = inv.Flags["model-id"]
	f.contextLimit = inv.Ints["context-limit"]
	f.apiKeyEnv = inv.Flags["api-key-env"]
	f.toolsMode = inv.Flags["tools-mode"]
}

// apply turns the flags into settings.
//
// The flags come last: they change what the settings file said. A flag
// that was not given changes nothing.
func (f *endpointFlags) apply(settings *config.Settings) {
	if f.endpoint == "" {
		return
	}
	name := f.modelID
	if settings.Endpoints == nil {
		settings.Endpoints = map[string]config.Endpoint{}
	}
	if settings.Models == nil {
		settings.Models = map[string]config.Model{}
	}
	if settings.Roles == nil {
		settings.Roles = map[string]string{}
	}
	settings.Endpoints[name] = config.Endpoint{
		Endpoint:  f.endpoint,
		APIKeyEnv: f.apiKeyEnv,
	}
	toolsMode := f.toolsMode
	if toolsMode == "" {
		toolsMode = string(poo.ToolsNative)
	}
	settings.Models[name] = config.Model{
		Endpoint:     name,
		ModelID:      f.modelID,
		API:          string(poo.APIChatCompletions),
		ToolsMode:    poo.ToolsMode(toolsMode),
		Ops:          []string{string(poo.OpChat)},
		ContextLimit: f.contextLimit,
	}
	settings.Roles[string(catalog.RoleChat)] = name
}

// given returns the flags that describe a model and were given.
//
// The context limit counts when it is positive: a zero is the flag's
// default, and the check reports a zero as a value, not as a flag that
// was not given.
func (f *endpointFlags) given() []string {
	flags := make([]string, 0, 5)
	if f.endpoint != "" {
		flags = append(flags, "--endpoint")
	}
	if f.modelID != "" {
		flags = append(flags, "--model-id")
	}
	if f.contextLimit > 0 {
		flags = append(flags, "--context-limit")
	}
	if f.apiKeyEnv != "" {
		flags = append(flags, "--api-key-env")
	}
	if f.toolsMode != "" {
		flags = append(flags, "--tools-mode")
	}
	return flags
}

// validate checks the flags that describe one model.
//
// The flags that name a model describe one model, so they are all or
// none of them, and the message names the flags that were given.
func (f *endpointFlags) validate() error {
	switch f.toolsMode {
	case "", "native", "text", "auto":
	default:
		return fmt.Errorf("--tools-mode must be native, text or auto, not %q", f.toolsMode)
	}
	given := f.given()
	if len(given) == 0 {
		return nil
	}
	var missing []string
	if f.endpoint == "" {
		missing = append(missing, "--endpoint")
	}
	if f.modelID == "" {
		missing = append(missing, "--model-id")
	}
	if len(missing) > 0 {
		needs := "needs"
		if len(given) > 1 {
			needs = "need"
		}
		return fmt.Errorf("%s %s %s", strings.Join(given, " and "), needs, strings.Join(missing, " and "))
	}
	if f.contextLimit <= 0 {
		return errors.New("--context-limit is required and must be positive")
	}
	return nil
}
