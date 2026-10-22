package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/handoff"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/provider/openai"
	"github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/edit"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// serveUsage is the usage of `poo serve`.
const serveUsage = `poo serve runs the wire protocol on stdin and stdout.

Usage:
  poo serve [flags]

Serve flags:
  --base-url <url>       The endpoint's base URL, e.g. http://localhost:4000/v1
  --model-id <id>        The model identifier
  --context-limit <n>    The model's context window in tokens (required)
  --api-key-env <var>    The environment variable holding the API key
  --tools-mode <mode>    native (default), text or auto
  --working-dir <dir>    The directory the tools work in (default: the current one)
  --session-dir <dir>    The directory the session logs live in
  --ephemeral            Keep the sessions in memory and write no files
`

// serveOptions are the flags of `poo serve`.
type serveOptions struct {
	baseURL      string
	modelID      string
	contextLimit int
	apiKeyEnv    string
	toolsMode    string
	workingDir   string
	sessionDir   string
	ephemeral    bool
}

// runServe executes `poo serve`.
//
// The server reads the client's requests from stdin and writes its
// answers to stdout. It returns the process exit code: zero after a
// clean teardown, and one after a transport failure.
func runServe(args []string) int {
	fs := flag.NewFlagSet("poo serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, serveUsage) }

	var o serveOptions
	fs.StringVar(&o.baseURL, "base-url", "", "The endpoint's base URL")
	fs.StringVar(&o.modelID, "model-id", "", "The model identifier")
	fs.IntVar(&o.contextLimit, "context-limit", 0, "The model's context window in tokens")
	fs.StringVar(&o.apiKeyEnv, "api-key-env", "", "The environment variable holding the API key")
	fs.StringVar(&o.toolsMode, "tools-mode", "native", "native, text or auto")
	fs.StringVar(&o.workingDir, "working-dir", "", "The directory the tools work in")
	fs.StringVar(&o.sessionDir, "session-dir", "", "The directory the session logs live in")
	fs.BoolVar(&o.ephemeral, "ephemeral", false, "Keep the sessions in memory and write no files")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if o.baseURL == "" || o.modelID == "" || o.contextLimit <= 0 {
		fmt.Fprint(os.Stderr, "poo serve: --base-url, --model-id and --context-limit are required\n")
		return 2
	}

	workDir := o.workingDir
	if workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
			return 1
		}
		workDir = wd
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
		return 1
	}

	sessionDir := o.sessionDir
	if sessionDir == "" && !o.ephemeral {
		sessionDir = defaultSessionDir()
	}
	if sessionDir != "" {
		sessionDir, err = filepath.Abs(sessionDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
			return 1
		}
	}

	factory, err := newServeFactory(o, absWorkDir, sessionDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
		return 2
	}

	err = wire.Serve(wire.Options{
		In:      os.Stdin,
		Out:     os.Stdout,
		Factory: factory,
		Build: protocol.Build{
			Name:       "poo",
			Version:    version,
			PooVersion: version,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
		return 1
	}
	return 0
}

// defaultSessionDir returns the directory the session logs live in.
//
// It is the state directory of the user, which is where a session that
// outlives its run belongs.
func defaultSessionDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "poo-sessions")
	}
	return filepath.Join(dir, ".local", "state", "poo", "sessions")
}

// serveFactory builds the sessions `poo serve` serves.
//
// The settings arrive with the config package; until then the factory
// takes the flags of `poo run` and builds the same session. The whole
// wiring is in this file, so the swap is one function.
type serveFactory struct {
	// opts are the flags the server was started with.
	opts serveOptions
	// workDir is the directory the tools work in.
	workDir string
	// sessionDir is where the session logs live.
	sessionDir string
	// childEnv is the scrubbed environment the tools run in.
	childEnv []string
	// tools are the tools the binary links.
	tools map[string]poo.Tool
	// toolOrder is the order the tools are offered in.
	toolOrder []string
	// model is the descriptor of the chat model.
	model poo.ModelInfo
}

// newServeFactory builds the factory the server runs with.
func newServeFactory(o serveOptions, workDir, sessionDir string) (*serveFactory, error) {
	scrub := []string{}
	if o.apiKeyEnv != "" {
		scrub = append(scrub, o.apiKeyEnv)
	}
	executor, err := executorRegistry().New("local", bash.Options{})
	if err != nil {
		return nil, err
	}
	tools := map[string]poo.Tool{
		"bash": bash.New(bash.WithExecutor(executor)),
		"read": read.New(),
		"grep": grep.New(),
		"edit": edit.New(),
	}
	info := poo.ModelInfo{
		ModelID:      o.modelID,
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		ContextLimit: o.contextLimit,
		Tools:        poo.ToolCalling{Mode: poo.ToolsMode(o.toolsMode)},
	}
	return &serveFactory{
		opts:       o,
		workDir:    workDir,
		sessionDir: sessionDir,
		childEnv:   bash.ChildEnv(os.Environ(), scrub),
		tools:      tools,
		toolOrder:  []string{"bash", "read"},
		model:      info,
	}, nil
}

// Profiles returns the profiles the binary offers.
//
// The command links the tools the code profile names.
func (f *serveFactory) Profiles() []protocol.ProfileInfo {
	tools := make([]protocol.ToolInfo, 0, len(f.toolOrder))
	for _, name := range f.toolOrder {
		spec := f.tools[name].Spec()
		tools = append(tools, protocol.ToolInfo{
			Name:       spec.Name,
			Class:      string(spec.Class),
			Background: string(spec.Background),
		})
	}
	return []protocol.ProfileInfo{{
		Name:      cmdProfile,
		Tools:     tools,
		Unmatched: string(perm.ActionAllow),
	}}
}

// Capabilities returns what this binary can do.
func (f *serveFactory) Capabilities() protocol.Capabilities {
	modes := []string{"ephemeral"}
	if f.sessionDir != "" {
		modes = []string{"ephemeral", "logged"}
	}
	return protocol.Capabilities{
		Decider:     false,
		Compaction:  true,
		Persistence: modes,
		Jobs:        true,
	}
}

// Roles returns the models this binary talks to.
func (f *serveFactory) Roles() protocol.ModelRoles {
	return protocol.ModelRoles{Chat: f.opts.modelID}
}

// Create builds a session.
func (f *serveFactory) Create(ctx context.Context, params protocol.CreateParams, env wire.Env) (*poo.Session, *wire.Info, error) {
	if params.Models != nil && params.Models.Chat != "" && params.Models.Chat != f.opts.modelID {
		return nil, nil, fmt.Errorf("poo serve: the settings name no model %q", params.Models.Chat)
	}
	workDir := params.WorkingDir
	if workDir == "" {
		workDir = f.workDir
	}
	mode := params.Mode
	if mode == "" {
		if f.sessionDir == "" {
			mode = string(session.ModeEphemeral)
		} else {
			mode = string(session.ModeLogged)
		}
	}
	store, err := f.openStore(mode, params.SessionDir)
	if err != nil {
		return nil, nil, err
	}
	recorder := &session.Recording{Store: store}
	sessionID, err := f.createSession(ctx, recorder, mode, workDir, params.Profile)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	policy, err := f.policy(workDir, env)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	sess, err := poo.New(poo.Options{
		Transport:  f.transport(),
		Tools:      f.tools,
		ToolOrder:  f.toolOrder,
		Sink:       recorder,
		SessionID:  sessionID,
		Model:      f.model,
		Profile:    profileOf(params.Profile),
		Profiles:   f.pooProfiles(),
		Build:      poo.BuildInfo{Name: "poo", Version: version},
		WorkingDir: workDir,
		Env:        f.childEnv,
		Settings:   f.settings(),
		ToolsMode:  f.opts.toolsMode,
		Compactor:  handoff.New(handoff.Options{}),
	}, poo.WithObserver(env.Observer), poo.WithPolicy(policy))
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	info := &wire.Info{
		Mode:    mode,
		Tools:   f.toolInfos(),
		Models:  f.Roles(),
		Records: recorder.Records,
		Release: store.Close,
	}
	if mode == string(session.ModeLogged) {
		info.SessionDir = f.storeDir()
	}
	return sess, info, nil
}

// Load opens a recorded session.
func (f *serveFactory) Load(ctx context.Context, params protocol.LoadParams, env wire.Env) (*poo.Session, *wire.Info, error) {
	if f.sessionDir == "" {
		return nil, nil, session.ErrSessionNotFound
	}
	dir := params.SessionDir
	if dir == "" {
		dir = f.storeDir()
	}
	store, err := session.NewStore(dir, session.WithBuild(poo.BuildInfo{Name: "poo", Version: version}))
	if err != nil {
		return nil, nil, err
	}
	recorder := &session.Recording{Store: store}
	policy, err := f.policy(f.workDir, env)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	resumed, err := session.Resume(ctx, recorder, params.SessionID, poo.Options{
		Transport:  f.transport(),
		Tools:      f.tools,
		ToolOrder:  f.toolOrder,
		Model:      f.model,
		Profiles:   f.pooProfiles(),
		Build:      poo.BuildInfo{Name: "poo", Version: version},
		WorkingDir: f.workDir,
		Env:        f.childEnv,
		Settings:   f.settings(),
		ToolsMode:  f.opts.toolsMode,
		Compactor:  handoff.New(handoff.Options{}),
	}, poo.WithObserver(env.Observer), poo.WithPolicy(policy))
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	loaded := resumed.Loaded
	records := recorder.RecordsWith(loaded.Records)
	info := &wire.Info{
		Mode:            string(loaded.Metadata.Mode),
		SessionDir:      dir,
		Tools:           f.toolInfos(),
		Models:          f.Roles(),
		RecoveredTail:   loaded.RecoveredTail,
		InterruptedRuns: resumed.Restored.Interrupted,
		Warnings:        resumed.Restored.Warnings,
		Records:         func() []poo.Record { return records },
		Release:         store.Close,
	}
	return resumed.Session, info, nil
}

// Configure applies a configuration change to a session.
//
// The command's settings own the prompt and the models, so a change
// that names another model is refused and a change that names none is
// answered with the session's own roles.
func (f *serveFactory) Configure(ctx context.Context, sess *poo.Session, params protocol.ConfigureParams) (protocol.ModelRoles, error) {
	if params.Models != nil && params.Models.Chat != "" && params.Models.Chat != f.opts.modelID {
		return protocol.ModelRoles{}, poo.ErrUnknownModel
	}
	if params.PromptOverrides != nil {
		return protocol.ModelRoles{}, errors.New("poo serve: the settings own the prompt in this build")
	}
	return f.Roles(), nil
}

// Models returns the effective model catalog.
func (f *serveFactory) Models(ctx context.Context, refresh bool) ([]protocol.ModelInfo, error) {
	return []protocol.ModelInfo{{
		Name:         f.opts.modelID,
		ModelID:      f.opts.modelID,
		API:          string(poo.APIChatCompletions),
		Ops:          []string{string(poo.OpChat)},
		ContextLimit: f.opts.contextLimit,
		ToolsMode:    f.opts.toolsMode,
	}}, nil
}

// Config returns the effective settings, with the credentials
// redacted.
func (f *serveFactory) Config(ctx context.Context) (map[string]any, error) {
	return map[string]any{
		"version": 1,
		"endpoints": map[string]any{
			"default": map[string]any{"base_url": f.opts.baseURL},
		},
		"models": map[string]any{
			f.opts.modelID: map[string]any{
				"model_id":      f.opts.modelID,
				"context_limit": f.opts.contextLimit,
				"api":           string(poo.APIChatCompletions),
				"ops":           []string{string(poo.OpChat)},
				"tools_mode":    f.opts.toolsMode,
			},
		},
		"roles":   map[string]any{"chat": f.opts.modelID},
		"profile": cmdProfile,
		"session": map[string]any{
			"mode": f.sessionMode(),
			"dir":  f.storeDir(),
		},
	}, nil
}

// transport returns the transport the sessions talk to the endpoint
// with.
func (f *serveFactory) transport() *openai.Transport {
	apiKey := ""
	if f.opts.apiKeyEnv != "" {
		apiKey = os.Getenv(f.opts.apiKeyEnv)
	}
	return openai.New(openai.Config{
		BaseURL:   f.opts.baseURL,
		Model:     f.opts.modelID,
		APIKey:    apiKey,
		ToolsMode: f.opts.toolsMode,
		Info:      f.model,
	})
}

// policy builds the permission policy of a session.
//
// The wire asker is in the chain: a client that declared the
// permission_answers capability is asked about the calls the policy
// wants to ask about.
func (f *serveFactory) policy(workDir string, env wire.Env) (*perm.Policy, error) {
	opts := perm.Options{Unmatched: perm.ActionAllow, WorkingDir: workDir}
	if env.Client != nil {
		opts.Askers = []perm.Asker{perm.NewWireAsker(env.Client, 0)}
	}
	return perm.New(opts)
}

// settings returns the tools' tunable knobs.
func (f *serveFactory) settings() poo.ToolSettings {
	return poo.ToolSettings{
		BashTimeoutSeconds:  int(bash.DefaultTimeout / time.Second),
		ObservationCapChars: bash.ObservationCap(f.opts.contextLimit),
		ReadOutputCapChars:  read.OutputCap(f.opts.contextLimit),
	}
}

// toolInfos returns the tools the sessions offer.
func (f *serveFactory) toolInfos() []protocol.ToolInfo {
	tools := make([]protocol.ToolInfo, 0, len(f.toolOrder))
	for _, name := range f.toolOrder {
		spec := f.tools[name].Spec()
		tools = append(tools, protocol.ToolInfo{
			Name:       spec.Name,
			Class:      string(spec.Class),
			Background: string(spec.Background),
		})
	}
	return tools
}

// pooProfiles returns the profiles the sessions may name.
func (f *serveFactory) pooProfiles() []poo.Profile {
	return []poo.Profile{{
		Name:      cmdProfile,
		Tools:     f.pooTools(),
		Unmatched: string(perm.ActionAllow),
	}}
}

// pooTools returns the tools the profiles link.
func (f *serveFactory) pooTools() []poo.ToolInfo {
	tools := make([]poo.ToolInfo, 0, len(f.toolOrder))
	for _, name := range f.toolOrder {
		spec := f.tools[name].Spec()
		tools = append(tools, poo.ToolInfo{
			Name:       spec.Name,
			Class:      spec.Class,
			Background: spec.Background,
		})
	}
	return tools
}

// openStore opens the store a session's records live in.
func (f *serveFactory) openStore(mode, dir string) (session.Store, error) {
	if mode == string(session.ModeEphemeral) {
		return session.NewMemory(), nil
	}
	if dir == "" {
		dir = f.storeDir()
	}
	return session.NewStore(dir, session.WithBuild(poo.BuildInfo{Name: "poo", Version: version}))
}

// storeDir returns the directory the session logs live in.
//
// It is empty when the command keeps its sessions in memory, and the
// handshake then announces the ephemeral mode alone.
func (f *serveFactory) storeDir() string {
	return f.sessionDir
}

// sessionMode returns the mode a session gets when the client names
// none.
func (f *serveFactory) sessionMode() string {
	if f.sessionDir == "" {
		return string(session.ModeEphemeral)
	}
	return string(session.ModeLogged)
}

// createSession writes the session's header and returns its id.
func (f *serveFactory) createSession(ctx context.Context, store session.Store, mode, workDir, profile string) (string, error) {
	tools := f.pooTools()
	return store.Create(ctx, &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    newSessionID(),
		Mode:         mode,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		WorkingDir:   workDir,
		Model:        poo.SessionModel{ID: f.opts.modelID, MaxOutput: f.model.MaxOutput},
		Tools:        tools,
		ContextLimit: f.opts.contextLimit,
		ToolsMode:    f.opts.toolsMode,
		Profile:      profileOf(profile),
		Roles:        poo.ModelRoles{Chat: &f.model},
		Build:        poo.BuildInfo{Name: "poo", Version: version},
	})
}

// profileOf returns the profile a session is built from.
func profileOf(name string) string {
	if name == "" {
		return cmdProfile
	}
	return name
}
