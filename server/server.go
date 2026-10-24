// Package server builds the factory a serve command serves the wire
// protocol with.
//
// A binary composes its tool set and its profiles, hands them to New
// with the settings, and serves the result: the factory maps the wire's
// requests onto config's build, opens the stores and turns the
// parameters into sessions. The package is the settings-to-session path
// in one place, so a custom binary does not copy it.
package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"sync"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// Options are what a server is built from.
//
// The binary says what it has: the tools it links, the executors it
// links and the profiles it offers. The settings say what the operator
// wants, and New puts the two together.
type Options struct {
	// Settings are the loaded settings.
	Settings *config.Settings
	// Overrides are the settings the command line changed.
	Overrides config.Overrides
	// WorkingDir is the directory the tools work in.
	WorkingDir string
	// DefaultProfile is the profile a session runs under when neither
	// the command line nor the settings name one.
	DefaultProfile string
	// Tools are the tool constructors the binary links, by name.
	Tools map[string]config.ToolFactory
	// Executors are the executors the binary links.
	Executors *exec.Registry
	// Profiles are the profiles the binary links.
	Profiles []profile.Profile
	// Build names the binary that serves.
	Build poo.BuildInfo
	// SessionTools changes every runtime the server builds, before a
	// session is created from it.
	//
	// A binary whose settings opt into a tool the profile does not name
	// (a typed result, say) adds it here, so the tool joins the profile
	// and the client is told about it like every other tool.
	SessionTools func(*config.Runtime) error
	// PromptFS is the prompt directory the binary embeds.
	//
	// The files replace the sections they name after the binary's
	// embedded defaults and before the settings. A nil FS keeps the
	// embedded defaults alone.
	PromptFS fs.FS
}

// New builds the server: the runtime the handshake reports, and the
// options every session's runtime is built from.
func New(opts Options) (*Server, error) {
	client := &lateClient{}
	build := config.BuildOptions{
		Settings:       opts.Settings,
		Overrides:      opts.Overrides,
		WorkingDir:     opts.WorkingDir,
		DefaultProfile: opts.DefaultProfile,
		Tools:          opts.Tools,
		Profiles:       opts.Profiles,
		Executors:      opts.Executors,
		// The server has a wire, so the chain asks the client. The
		// decide asker is there only when the settings name a decider
		// model.
		Askers:   []string{"wire"},
		Wire:     client,
		Build:    opts.Build,
		PromptFS: opts.PromptFS,
	}
	runtime, err := config.Build(build)
	if err != nil {
		return nil, err
	}
	s := &Server{
		runtime:   runtime,
		build:     build,
		settings:  opts.Settings,
		effective: config.WithOverrides(opts.Settings, opts.Overrides),
		workDir:   opts.WorkingDir,
		client:    client,
		runtimes:  map[string]*config.Runtime{},
		decorate:  opts.SessionTools,
	}
	if err := s.decorateRuntime(runtime); err != nil {
		return nil, err
	}
	return s, nil
}

// Warning is the warning the build printed. It is empty when there is
// nothing to warn about.
func (s *Server) Warning() string { return s.runtime.Warning }

// decorateRuntime applies the binary's session tools to a runtime.
func (s *Server) decorateRuntime(runtime *config.Runtime) error {
	if s.decorate == nil {
		return nil
	}
	return s.decorate(runtime)
}

// Server is the wire factory: it builds the sessions a serve command
// serves.
//
// The settings arrive with the config package, and the runtime is what
// they build: the profiles, the models, the policy and the options. The
// server turns a session's parameters into a session, and it is the only
// place the wire's parameters meet the settings.
type Server struct {
	// runtime is what the settings built. It is the runtime the
	// handshake reports and the runtime a session runs under when it
	// names no profile and no model.
	runtime *config.Runtime
	// build is what the settings are built with. A runtime for another
	// profile or another model is built from it.
	build config.BuildOptions
	// settings are the settings the build was made from.
	settings *config.Settings
	// effective are the settings a command line leaves after its
	// overrides: the document config/get reports.
	effective *config.Settings
	// workDir is the directory the tools work in.
	workDir string
	// client is the wire client the asker chain asks.
	client *lateClient

	// mu guards the runtimes below.
	mu sync.Mutex
	// runtimes are the runtimes the sessions run under, by profile and
	// model.
	runtimes map[string]*config.Runtime
	// decorate changes a runtime the server built, before a session is
	// created from it.
	decorate func(*config.Runtime) error
}

// runtimeFor returns the runtime a session runs under.
//
// A session names the profile it is built from and the model it talks
// to, and the two decide the tools it offers and the policy it runs
// with. The runtimes are built once and kept: a second session with
// the same profile and model gets the same runtime.
func (f *Server) runtimeFor(profile, model string) (*config.Runtime, error) {
	if profile != "" && !hasProfile(f.build.Profiles, profile) {
		return nil, poo.ErrUnknownProfile
	}
	if model != "" {
		if _, ok := f.settings.Models[model]; !ok {
			return nil, poo.ErrUnknownModel
		}
	}
	key := profile + "\x00" + model
	f.mu.Lock()
	defer f.mu.Unlock()
	if runtime, ok := f.runtimes[key]; ok {
		return runtime, nil
	}
	if profile == "" && model == "" {
		// The default runtime is the one New built, and it carries
		// the settings' own tools.
		f.runtimes[key] = f.runtime
		return f.runtime, nil
	}
	settings := *f.settings
	settings.Roles = map[string]string{}
	for name, entry := range f.settings.Roles {
		settings.Roles[name] = entry
	}
	if model != "" {
		settings.Roles[config.RoleChat] = model
	}
	opts := f.build
	opts.Settings = &settings
	if profile != "" {
		opts.Overrides.Profile = profile
	}
	runtime, err := config.Build(opts)
	if err != nil {
		return nil, err
	}
	if err := f.decorateRuntime(runtime); err != nil {
		return nil, err
	}
	f.runtimes[key] = runtime
	return runtime, nil
}

// hasProfile reports whether the binary links the named profile.
func hasProfile(profiles []profile.Profile, name string) bool {
	for _, p := range profiles {
		if p.Name == name {
			return true
		}
	}
	return false
}

// Profiles returns the profiles the binary offers.
func (f *Server) Profiles() []protocol.ProfileInfo {
	runtime, err := f.runtimeFor("", "")
	if err != nil {
		return []protocol.ProfileInfo{}
	}
	out := make([]protocol.ProfileInfo, 0, len(runtime.Profiles))
	for _, p := range runtime.Profiles {
		info := protocol.ProfileInfo{Name: p.Name, Unmatched: p.Unmatched}
		for _, tool := range p.Tools {
			info.Tools = append(info.Tools, protocol.ToolInfo{
				Name:       tool.Name,
				Class:      string(tool.Class),
				Background: string(tool.Background),
			})
		}
		out = append(out, info)
	}
	return out
}

// Capabilities returns what this binary can do.
func (f *Server) Capabilities() protocol.Capabilities {
	runtime, err := f.runtimeFor("", "")
	if err != nil {
		return protocol.Capabilities{}
	}
	modes := []string{"ephemeral"}
	if runtime.SessionMode == string(session.ModeLogged) {
		modes = []string{"ephemeral", "logged"}
	}
	decider := false
	if _, err := runtime.DeciderModel(); err == nil {
		decider = true
	}
	return protocol.Capabilities{
		Decider:     decider,
		Compaction:  runtime.Options.Compactor != nil,
		Persistence: modes,
		Jobs:        f.jobs(),
	}
}

// jobs reports whether a tool the binary offers may run in the
// background.
//
// The capability is about the binary, not about the profile the
// settings name: a session may be created under any profile the
// handshake offers.
func (f *Server) jobs() bool {
	for _, profile := range f.Profiles() {
		for _, tool := range profile.Tools {
			if tool.Background != string(poo.BackgroundNever) {
				return true
			}
		}
	}
	return false
}

// Roles returns the models this binary talks to.
func (f *Server) Roles() protocol.ModelRoles {
	runtime, err := f.runtimeFor("", "")
	if err != nil {
		return protocol.ModelRoles{}
	}
	return rolesOf(nil, runtime)
}

// rolesOf returns the models a session talks to.
//
// The session's own chat and decision models come first. A session that
// has no decision model reports the runtime's, which is the role the
// settings name.
func rolesOf(sess *poo.Session, runtime *config.Runtime) protocol.ModelRoles {
	roles := protocol.ModelRoles{}
	if sess != nil {
		if model := sess.ChatModel(); model.Name != "" {
			roles.Chat = model.Name
		}
		if model, ok := sess.DeciderModel(); ok {
			roles.Decider = model.Name
		}
	} else if model, err := runtime.ChatModel(); err == nil {
		roles.Chat = model.Name
	}
	if roles.Decider == "" {
		if model, err := runtime.DeciderModel(); err == nil {
			roles.Decider = model.Name
		}
	}
	return roles
}

// Create builds a session.
func (f *Server) Create(ctx context.Context, params protocol.CreateParams, env wire.Env) (*poo.Session, *wire.Info, error) {
	f.client.set(env.Client)
	model := ""
	deciderName := ""
	if params.Models != nil {
		model = params.Models.Chat
		deciderName = params.Models.Decider
	}
	runtime, err := f.runtimeFor(params.Profile, model)
	if err != nil {
		return nil, nil, err
	}
	// The decision model must be one the settings name, and it is built
	// before the session is so a bad name leaves nothing behind.
	var deciderModel poo.ModelInfo
	var decider decide.Decider
	if deciderName != "" {
		if _, ok := f.settings.Models[deciderName]; !ok {
			return nil, nil, poo.ErrUnknownModel
		}
		deciderModel, decider, err = runtime.DeciderFor(deciderName)
		if err != nil {
			return nil, nil, err
		}
	}
	mode := params.Mode
	if mode == "" {
		mode = runtime.SessionMode
	}
	store, err := runtime.OpenStore(mode, params.SessionDir)
	if err != nil {
		return nil, nil, err
	}
	// The recorder keeps the records the session writes, so the server
	// can project them into the session's history.
	recorder := &session.Recording{Store: store}
	workDir := params.WorkingDir
	if workDir == "" {
		workDir = f.workDir
	}
	created, err := f.created(runtime, mode, workDir)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	sess, err := runtime.CreateSession(ctx, recorder, created, poo.WithObserver(env.Observer))
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	if params.PromptOverrides != nil {
		edits, err := runtime.ApplyPromptOverrides(promptOverrides(params.PromptOverrides))
		if err != nil {
			_ = sess.Close()
			_ = store.Close()
			return nil, nil, err
		}
		if err := sess.SetPrompt(edits...); err != nil {
			_ = sess.Close()
			_ = store.Close()
			return nil, nil, err
		}
	}
	if deciderName != "" {
		if err := sess.SetDecider(deciderModel, decider); err != nil {
			_ = sess.Close()
			_ = store.Close()
			return nil, nil, err
		}
	}
	info := &wire.Info{
		Mode:   mode,
		Tools:  toolInfos(runtime.Profile.Tools),
		Models: rolesOf(sess, runtime),
		// The records are the session's, and the store is what holds
		// the session's lock.
		Records: recorder.Records,
		Release: store.Close,
	}
	if mode == string(session.ModeLogged) {
		info.SessionDir = storeDir(store)
	}
	return sess, info, nil
}

// Load opens a recorded session.
func (f *Server) Load(ctx context.Context, params protocol.LoadParams, env wire.Env) (*poo.Session, *wire.Info, error) {
	f.client.set(env.Client)
	dir := params.SessionDir
	if dir == "" {
		dir = f.runtime.SessionDir
	}
	// The session's own profile decides the tools it runs with, and its
	// own model decides the endpoint it talks to. The header says both,
	// and it is read before the session is loaded.
	meta, err := session.MetadataOf(dir, params.SessionID)
	if err != nil {
		return nil, nil, err
	}
	runtime, err := f.runtimeForLoad(meta)
	if err != nil {
		return nil, nil, err
	}
	store, err := runtime.OpenStore(string(session.ModeLogged), dir)
	if err != nil {
		return nil, nil, err
	}
	recorder := &session.Recording{Store: store}
	resumed, err := runtime.ResumeSession(ctx, recorder, params.SessionID, poo.WithObserver(env.Observer))
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	loaded := resumed.Loaded
	info := &wire.Info{
		Mode:            string(loaded.Metadata.Mode),
		SessionDir:      storeDir(store),
		Tools:           toolInfos(runtime.Profile.Tools),
		Models:          rolesOf(resumed.Session, runtime),
		RecoveredTail:   loaded.RecoveredTail,
		InterruptedRuns: resumed.Restored.Interrupted,
		Warnings:        resumed.Restored.Warnings,
		Records:         func() []poo.Record { return recorder.RecordsWith(loaded.Records) },
		Release:         store.Close,
	}
	return resumed.Session, info, nil
}

// runtimeForLoad returns the runtime a recorded session is restored
// with.
//
// The session's own profile decides the tools it runs with, and its own
// model decides the endpoint it talks to. A model the settings do not
// name is left to the restore's own check: the session does not fit
// this build.
func (f *Server) runtimeForLoad(meta session.Metadata) (*config.Runtime, error) {
	model := ""
	if meta.Roles.Chat != nil {
		model = meta.Roles.Chat.Name
	}
	if model != "" {
		if _, ok := f.settings.Models[model]; !ok {
			model = ""
		}
	}
	return f.runtimeFor(meta.Profile, model)
}

// Configure applies a configuration change to a session.
//
// The prompt overrides are the settings' own prompt changes, and the
// models are the ones the settings offer. A model the settings do not
// name is refused.
func (f *Server) Configure(ctx context.Context, sess *poo.Session, params protocol.ConfigureParams) (protocol.ModelRoles, error) {
	runtime, err := f.runtimeFor("", "")
	if err != nil {
		return protocol.ModelRoles{}, err
	}
	roles := rolesOf(sess, runtime)
	if params.PromptOverrides != nil {
		edits, err := runtime.ApplyPromptOverrides(promptOverrides(params.PromptOverrides))
		if err != nil {
			return protocol.ModelRoles{}, err
		}
		if err := sess.SetPrompt(edits...); err != nil {
			return protocol.ModelRoles{}, err
		}
	}
	if params.Models != nil && params.Models.Chat != "" {
		model, ok := runtime.Models[params.Models.Chat]
		if !ok {
			return protocol.ModelRoles{}, poo.ErrUnknownModel
		}
		transport, err := runtime.Transport(model)
		if err != nil {
			return protocol.ModelRoles{}, err
		}
		changed, err := sess.SetModel(model, transport)
		if err != nil {
			return protocol.ModelRoles{}, err
		}
		roles.Chat = changed.To
	}
	if params.Models != nil && params.Models.Decider != "" {
		if _, ok := f.settings.Models[params.Models.Decider]; !ok {
			return protocol.ModelRoles{}, poo.ErrUnknownModel
		}
		model, decider, err := runtime.DeciderFor(params.Models.Decider)
		if err != nil {
			return protocol.ModelRoles{}, err
		}
		if err := sess.SetDecider(model, decider); err != nil {
			return protocol.ModelRoles{}, err
		}
		roles.Decider = model.Name
	}
	return roles, nil
}

// Models returns the effective model catalog.
func (f *Server) Models(ctx context.Context, refresh bool) ([]protocol.ModelInfo, error) {
	runtime, err := f.runtimeFor("", "")
	if err != nil {
		return nil, err
	}
	if refresh {
		if err := runtime.Catalog.Reprobe(ctx); err != nil {
			return nil, err
		}
	}
	models := runtime.Catalog.List()
	out := make([]protocol.ModelInfo, 0, len(models))
	for _, model := range models {
		out = append(out, protocol.ModelInfo{
			Name:         model.Name,
			ModelID:      model.ModelID,
			API:          string(model.API),
			Ops:          opNames(model.Ops),
			Inputs:       modalityNames(model.Inputs),
			ContextLimit: model.ContextLimit,
			MaxOutput:    model.MaxOutput,
			ToolsMode:    string(model.Tools.Mode),
		})
	}
	return out, nil
}

// Config returns the effective settings, with the credentials
// redacted.
//
// The settings name the environment variables the keys come from and
// never the keys themselves, so the document is the settings as they
// are.
func (f *Server) Config(ctx context.Context) (map[string]any, error) {
	settings := f.effective
	if settings == nil {
		settings = f.settings
	}
	// The settings name environment variables, never keys. A base URL
	// may carry credentials all the same, and the document is what the
	// client reads.
	redacted := *settings
	redacted.Endpoints = make(map[string]config.Endpoint, len(settings.Endpoints))
	for name, endpoint := range settings.Endpoints {
		endpoint.BaseURL = session.LoggedBaseURL(endpoint.BaseURL)
		redacted.Endpoints[name] = endpoint
	}
	encoded, err := json.Marshal(&redacted)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// created returns the header record a session starts with.
func (f *Server) created(runtime *config.Runtime, mode, workDir string) (*poo.SessionCreated, error) {
	model := runtime.Options.Model
	chat := session.LoggedModel(model)
	return &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    session.NewID(),
		Mode:         mode,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		WorkingDir:   workDir,
		Model:        poo.SessionModel{ID: model.ModelID, BaseURL: session.LoggedBaseURL(model.Endpoint), MaxOutput: model.MaxOutput},
		Tools:        runtime.SessionTools(),
		ContextLimit: model.ContextLimit,
		ToolsMode:    string(model.Tools.Mode),
		Profile:      runtime.Profile.Name,
		Roles:        poo.ModelRoles{Chat: &chat},
		Build:        f.build.Build,
	}, nil
}

// promptOverrides converts the wire's prompt overrides into the
// settings' own.
func promptOverrides(overrides *protocol.PromptOverrides) config.PromptOverrides {
	return config.PromptOverrides{
		BaseFile: overrides.BaseFile,
		Replace:  overrides.Replace,
		Append:   overrides.Append,
		Sections: overrides.Sections,
		Remove:   overrides.Remove,
	}
}

// toolInfos converts the core's tool descriptions into the wire's.
func toolInfos(tools []poo.ToolInfo) []protocol.ToolInfo {
	out := make([]protocol.ToolInfo, 0, len(tools))
	for _, tool := range tools {
		out = append(out, protocol.ToolInfo{
			Name:       tool.Name,
			Class:      string(tool.Class),
			Background: string(tool.Background),
		})
	}
	return out
}

// storeDir returns the directory a store keeps its sessions in.
func storeDir(store session.Store) string {
	if file, ok := store.(*session.FileStore); ok {
		return file.Dir()
	}
	return ""
}

// opNames returns the names of a model's operations.
func opNames(ops []poo.Op) []string {
	if len(ops) == 0 {
		return nil
	}
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		out = append(out, string(op))
	}
	return out
}

// modalityNames returns the names of a model's modalities.
func modalityNames(inputs []poo.Modality) []string {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]string, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, string(input))
	}
	return out
}

// lateClient is the wire client the asker chain asks.
//
// The server creates its client when it starts, and the factory learns
// it when the first session is built. The runtime is built before
// either, so the client is filled in later and an ask made before that
// is deferred.
type lateClient struct {
	// mu guards the client below.
	mu sync.Mutex
	// client is the connected client, once the factory has seen it.
	client perm.Client
}

// set points the client at the connected one.
func (c *lateClient) set(client perm.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client = client
}

// current returns the connected client.
func (c *lateClient) current() perm.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

// Connected reports that a client is connected.
func (c *lateClient) Connected() bool { return c.current() != nil }

// PermissionAnswers reports that the connected client declared the
// permission_answers capability.
func (c *lateClient) PermissionAnswers() bool {
	client := c.current()
	return client != nil && client.PermissionAnswers()
}

// Ask sends the ask to the client and waits for its answer.
func (c *lateClient) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	client := c.current()
	if client == nil {
		return perm.DecisionDefer, nil
	}
	return client.Ask(ctx, ask)
}
