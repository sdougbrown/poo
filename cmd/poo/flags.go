package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	// ephemeral runs the command without writing a session.
	ephemeral bool
	// profile is the profile the session is built from.
	profile string
}

// register adds the shared flags to a flag set.
func (f *sharedFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.settingsFile, "settings", "", "The settings file: JSON, TOML or YAML")
	fs.StringVar(&f.systemPrompt, "system-prompt", "", "The file holding the system prompt's base section")
	fs.StringVar(&f.appendSystemPrompt, "append-system-prompt", "", "Text appended to the system prompt, or @file")
	fs.StringVar(&f.permissionUnmatched, "permission-unmatched", "", "What to do with a side-effecting call no rule covers: allow, ask or deny")
	fs.StringVar(&f.sessionDir, "session-dir", "", "The directory the session files live in")
	fs.BoolVar(&f.ephemeral, "ephemeral", false, "Run without writing a session")
	fs.StringVar(&f.profile, "profile", "", "The profile to run under: base, code or search")
}

// settings reads the settings file and returns the settings.
//
// A command that needs no settings runs without one: the settings are
// the defaults, and the flags change them.
func (f *sharedFlags) settings() (*config.Settings, error) {
	settings := &config.Settings{Version: 1}
	if f.settingsFile == "" {
		return settings, nil
	}
	loaded, err := config.Load(f.settingsFile)
	if err != nil {
		return nil, err
	}
	return loaded, nil
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
	overrides := config.Overrides{
		Profile:             f.profile,
		PermissionUnmatched: f.permissionUnmatched,
		SessionDir:          f.sessionDir,
		SystemPrompt:        f.systemPrompt,
		AppendSystemPrompt:  appended,
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

// register adds the endpoint flags to a flag set.
func (f *endpointFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.endpoint, "endpoint", "", "The model's endpoint")
	fs.StringVar(&f.modelID, "model-id", "", "The model identifier")
	fs.IntVar(&f.contextLimit, "context-limit", 0, "The model's context window in tokens")
	fs.StringVar(&f.apiKeyEnv, "api-key-env", "", "The environment variable holding the API key")
	fs.StringVar(&f.toolsMode, "tools-mode", "", "native, text or auto")
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

// validate checks the flags that describe one model.
//
// The flags that name a model describe one model, so they are all or
// none of them.
func (f *endpointFlags) validate() error {
	switch f.toolsMode {
	case "", "native", "text", "auto":
	default:
		return fmt.Errorf("--tools-mode must be native, text or auto, not %q", f.toolsMode)
	}
	named := f.endpoint != "" || f.modelID != "" || f.contextLimit > 0 || f.apiKeyEnv != "" || f.toolsMode != ""
	if !named {
		return nil
	}
	switch {
	case f.endpoint == "":
		return errors.New("--endpoint is required with --model-id")
	case f.modelID == "":
		return errors.New("--model-id is required with --endpoint")
	case f.contextLimit <= 0:
		return errors.New("--context-limit is required and must be positive")
	}
	return nil
}
