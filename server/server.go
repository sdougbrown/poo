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
	"sort"
	"sync"
	"time"

	"fmt"
	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/provider/catalog"
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
	// UserPromptFile is the file the binary reads the user's
	// instructions from when the settings name none.
	UserPromptFile string
	// Tools are the tool constructors the binary links, by name.
	Tools map[string]config.ToolFactory
	// MCPLauncher starts the MCP servers a session's profile selected.
	// It is the binary's link to the client, and every runtime the
	// server builds carries it.
	MCPLauncher config.MCPLauncher
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
		Overrides:      config.EnvOverrides(opts.Overrides),
		WorkingDir:     opts.WorkingDir,
		DefaultProfile: opts.DefaultProfile,
		UserPromptFile: opts.UserPromptFile,
		Tools:          opts.Tools,
		MCPLauncher:    opts.MCPLauncher,
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
	effective := config.WithOverrides(opts.Settings, build.Overrides)
	s := &Server{
		runtime:   runtime,
		build:     build,
		settings:  opts.Settings,
		effective: effective,
		workDir:   opts.WorkingDir,
		client:    client,
		runtimes:  map[runtimeKey]*config.Runtime{},
		decorate:  opts.SessionTools,
	}
	qualified, err := qualifiedModelNames(effective, runtime.Models)
	if err != nil {
		return nil, err
	}
	s.qualified = qualified
	if err := s.decorateRuntime(runtime); err != nil {
		return nil, err
	}
	return s, nil
}

// qualifiedModelNames indexes the endpoint-qualified names the settings'
// models answer to.
//
// A settings model keeps its bare key, and the models file's naming is
// `endpoint/model`: one spelling should reach a model however the
// settings declared it. The index maps each model's
// `endpoint/model_id` to its key. A form two models answer to, or one a
// catalog name already holds, is refused here — the ambiguity is the
// operator's to fix, never the router's to guess at.
//
// The index reads the effective settings — the post-override view the
// settings/get reports. The overrides never touch models today; if one
// ever renames a model, this index and the catalog must move together.
func qualifiedModelNames(settings *config.Settings, catalogModels map[string]poo.ModelInfo) (map[string]string, error) {
	qualified := map[string]string{}
	keys := make([]string, 0, len(settings.Models))
	for key := range settings.Models {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		model := settings.Models[key]
		if model.Endpoint == "" || model.ModelID == "" {
			continue
		}
		candidate := model.Endpoint + "/" + model.ModelID
		if candidate == key {
			continue
		}
		if _, held := catalogModels[candidate]; held {
			return nil, fmt.Errorf("config: the model name %q is ambiguous: the settings key %q and the catalog both answer to it", candidate, key)
		}
		if owner, seen := qualified[candidate]; seen {
			return nil, fmt.Errorf("config: the model name %q is ambiguous: %q and %q both answer to it", candidate, owner, key)
		}
		qualified[candidate] = key
	}
	return qualified, nil
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
	// overrides: the document settings/get reports.
	effective *config.Settings
	// workDir is the directory the tools work in.
	workDir string
	// client is the wire client the asker chain asks.
	client *lateClient

	// mu guards the runtimes below.
	mu sync.Mutex
	// modelsMu guards the default runtime's model map and the qualified
	// names built from it. A reprobe replaces what the catalog holds, and
	// the names a session may switch to are rebuilt from it while another
	// request may be reading them.
	modelsMu sync.RWMutex
	// runtimes are the runtimes the sessions run under, by profile and
	// model.
	runtimes map[runtimeKey]*config.Runtime
	// decorate changes a runtime the server built, before a session is
	// created from it.
	decorate func(*config.Runtime) error
	// qualified maps the endpoint-qualified names the settings'
	// models answer to — `endpoint/model_id` — to the settings keys
	// the catalog holds. The models file's spelling reaches a settings
	// model through it.
	qualified map[string]string
}

// servesModel reports whether a model name is one a session can name.
//
// The catalog answers, not the settings: a model the models file declares is a model a
// session runs, and the settings' own map does not hold it. The default runtime is where
// the server keeps the catalog, and the catalog is the same whichever profile a session
// is built from.
func (f *Server) servesModel(name string) bool {
	_, ok := f.modelFor(name)
	return ok
}

// modelFor returns the model a requested name refers to: the model the
// settings name by key, or the settings model an endpoint-qualified
// name points at.
//
// The models file names its models `endpoint/model`, so that spelling
// has always reached the models a file declares. A settings model keeps
// its bare key, and the qualified form resolves to it as an alias: the
// one the endpoint and the model id name. A form two models answer to
// never reaches here — New refuses the ambiguity.
func (f *Server) modelFor(name string) (poo.ModelInfo, bool) {
	f.modelsMu.RLock()
	if model, ok := f.runtime.Models[name]; ok {
		f.modelsMu.RUnlock()
		return model, true
	}
	if key, ok := f.qualified[name]; ok {
		// The catalog holds the key only when the model is reachable:
		// a settings model whose endpoint is not configured is skipped
		// with a warning, and an alias for it is unknown the way the
		// bare key is.
		model, ok := f.runtime.Models[key]
		f.modelsMu.RUnlock()
		return model, ok
	}
	f.modelsMu.RUnlock()
	// The catalog is the wider answer: a probe reports models the settings never
	// named, and one of those is a model a session may switch to before the next
	// build. The map above is what the build put there; this is what the endpoints
	// last said.
	if model, err := f.runtime.Catalog.Resolve(name); err == nil {
		return model, true
	}
	return poo.ModelInfo{}, false
}

// adoptCatalog rebuilds the names the session may switch to from what the catalog
// holds now, after a reprobe.
//
// The model map and the qualified names are built once, from the settings and the
// layers the build read. A reprobe reports models no layer declared, and a switch
// resolves through these names, so a refreshed list that could not be switched to
// would be a list of models the operator can see and not run.
func (f *Server) adoptCatalog(runtime *config.Runtime) error {
	f.modelsMu.Lock()
	defer f.modelsMu.Unlock()
	runtime.ReloadModels()
	qualified, err := qualifiedModelNames(f.effective, runtime.Models)
	if err != nil {
		return err
	}
	f.qualified = qualified
	return nil
}

type runtimeKey struct{ profile, model string }

// runtimeFor returns the runtime a session runs under.
//
// A session names the profile it is built from and the model it talks
// to, and the two decide the tools it offers and the policy it runs
// with. The runtimes are built once and kept: a second session with
// the same profile and model gets the same runtime.
func (f *Server) runtimeFor(profileName, model string) (*config.Runtime, error) {
	if profileName != "" && !f.runtime.HasProfile(profileName) {
		return nil, poo.ErrUnknownProfile
	}
	if model != "" {
		resolved, ok := f.modelFor(model)
		if !ok {
			return nil, poo.ErrUnknownModel
		}
		model = resolved.Name
	}
	key := runtimeKey{profile: profileName, model: model}
	f.mu.Lock()
	defer f.mu.Unlock()
	if runtime, ok := f.runtimes[key]; ok {
		return runtime, nil
	}
	if profileName == "" && model == "" {
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
		settings.Roles[string(catalog.RoleChat)] = model
	}
	// The build is the one New made, carried whole: the launcher the
	// binary linked reaches this per-profile build the same way, so a
	// session under any profile starts what that profile selects.
	opts := f.build
	opts.Settings = &settings
	if profileName != "" {
		opts.Overrides.Profile = profileName
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

// Profiles returns the profiles the binary offers.
func (f *Server) Profiles() []protocol.ProfileInfo {
	runtime, err := f.runtimeFor("", "")
	if err != nil {
		return []protocol.ProfileInfo{}
	}
	out := make([]protocol.ProfileInfo, 0, len(runtime.Profiles))
	for _, p := range runtime.Profiles {
		info := protocol.ProfileInfo{Name: p.Name, Unmatched: p.Unmatched}
		// A profile with no tools writes none: the handshake's profile
		// list says null for a profile that offers nothing, and the
		// session's own tool list says [] for a session with none.
		if len(p.Tools) > 0 {
			info.Tools = toolInfos(p.Tools)
		}
		out = append(out, info)
	}
	return out
}

// DefaultProfile returns the profile a session runs under when its
// params name none.
//
// It is the profile the default runtime was resolved to, which is the
// runtime session/create falls back to.
func (f *Server) DefaultProfile() string { return f.runtime.Profile.Name }

// Capabilities returns what this binary can do.
func (f *Server) Capabilities() protocol.Capabilities {
	runtime, err := f.runtimeFor("", "")
	if err != nil {
		return protocol.Capabilities{}
	}
	modes := []string{"ephemeral"}
	switch runtime.SessionMode {
	case string(session.ModeLogged):
		modes = []string{"ephemeral", "logged"}
	case string(session.ModeDurable):
		// The durable mode is offered only when the settings' store
		// name has a registered backend: a binary that links no
		// backend for the name cannot open the sessions it would
		// announce.
		if config.StoreBackendRegistered(runtime.Settings.DurableSettings().Store) {
			modes = []string{"ephemeral", "durable"}
		}
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
		Feeds:       f.feeds(),
		// The history is this core's projection, so every item it
		// answers with carries its record's origin.
		HistoryOrigin: true,
	}
}

// feeds reports whether the binary runs the feeds its settings name.
//
// A feed is a command, so a binary that links no executor runs none.
// Nothing else about a feed is the binary's to leave out: config builds
// the feeds a settings file names, so the executor is the whole of what
// makes them reachable.
func (f *Server) feeds() bool { return f.build.Executors != nil }

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
		if model, ok := sess.CompactionModel(); ok {
			roles.Compaction = model.Name
		}
	} else if model, err := runtime.ChatModel(); err == nil {
		roles.Chat = model.Name
	}
	if roles.Decider == "" {
		if model, err := runtime.DeciderModel(); err == nil {
			roles.Decider = model.Name
		}
	}
	if roles.Compaction == "" {
		if model, err := runtime.CompactionModel(); err == nil {
			roles.Compaction = model.Name
		}
	}
	return roles
}

// Create builds a session.
func (f *Server) Create(ctx context.Context, params protocol.CreateParams, env wire.FactoryEnv) (*poo.Session, *wire.Info, error) {
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
	// before the session is so a bad name leaves nothing behind. The
	// header says which model decides: the one the request names, and
	// otherwise the one the runtime resolved from the settings.
	headerDecider := runtime.Options.DeciderModel
	var deciderModel poo.ModelInfo
	var decider decide.Decider
	if deciderName != "" {
		resolved, ok := f.modelFor(deciderName)
		if !ok {
			return nil, nil, poo.ErrUnknownModel
		}
		deciderModel, decider, err = runtime.DeciderFor(resolved.Name)
		if err != nil {
			return nil, nil, err
		}
		headerDecider = deciderModel
	}
	mode := params.Mode
	if mode == "" {
		mode = runtime.SessionMode
	}
	// The modes this process can serve are the ones the handshake
	// announced. A mode outside them is refused, so a consumer that
	// names an unsupported mode cannot get a session that pretends to
	// run in it.
	if !contains(f.Capabilities().Persistence, mode) {
		return nil, nil, fmt.Errorf("server: this process does not serve the %q mode", mode)
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
	created, err := f.created(runtime, mode, workDir, headerDecider)
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
	// The client reads the warning with the session it built: a client
	// that cannot answer is told why every ask falls back.
	if warning := perm.WireClientWarning(runtime.Policy, runtime.Askers, env.Client); warning != "" {
		info.Warnings = append(info.Warnings, warning)
	}
	// The session's own start may carry warnings of its own: the family
	// guidance a profile opted into rides the variant the session
	// renders in, and the session knows whether it does.
	info.Warnings = append(info.Warnings, sess.StartWarnings()...)
	if mode == string(session.ModeLogged) || mode == string(session.ModeDurable) {
		info.SessionDir = session.StoreDir(store)
	}
	return sess, info, nil
}

// Load opens a recorded session.
func (f *Server) Load(ctx context.Context, params protocol.LoadParams, env wire.FactoryEnv) (*poo.Session, *wire.Info, error) {
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
	// The store is the session's own mode: a durable session's records
	// go to the store the settings open, and its unsettled run is
	// planned for a continuation by the store's recovery.
	store, err := runtime.OpenStore(string(meta.Mode), dir)
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
	// The header records the decider the session ran with, which may
	// override the settings' default. The resumed session takes the
	// recorded one, so it does not contradict the header it came from.
	if role := loaded.Metadata.Roles.Decider; role != nil && role.Name != "" {
		if used, ok := resumed.Session.DeciderModel(); !ok || used.Name != role.Name {
			deciderModel, decider, err := runtime.DeciderFor(role.Name)
			if err != nil {
				_ = resumed.Session.Close()
				_ = store.Close()
				// The settings name the recorded model, and this build
				// cannot build a decider from it, so the session no longer
				// fits the build.
				return nil, nil, &poo.IncompatibleError{
					Code: poo.CodeSessionIncompatible,
					Message: fmt.Sprintf("poo: the session ran with the %q decider, and this build cannot build one from it",
						role.Name),
					Differences: []string{err.Error()},
				}
			}
			if err := resumed.Session.SetDecider(deciderModel, decider); err != nil {
				_ = resumed.Session.Close()
				_ = store.Close()
				return nil, nil, err
			}
		}
	}
	info := &wire.Info{
		Mode:            string(loaded.Metadata.Mode),
		SessionDir:      session.StoreDir(store),
		Tools:           toolInfos(runtime.Profile.Tools),
		Models:          rolesOf(resumed.Session, runtime),
		RecoveredTail:   loaded.RecoveredTail,
		InterruptedRuns: resumed.Restored.Interrupted,
		Warnings:        resumed.Restored.Warnings,
		Records:         func() []poo.Record { return recorder.RecordsWith(loaded.Records) },
		Release:         store.Close,
	}
	// The client reads the warning with the session it opened: a client
	// that cannot answer is told why every ask falls back.
	if warning := perm.WireClientWarning(runtime.Policy, runtime.Askers, env.Client); warning != "" {
		info.Warnings = append(info.Warnings, warning)
	}
	// A session that has run renders its recorded prompt and carries no
	// start warnings: nothing re-renders it, so there is nothing to
	// warn about again. A session that never ran is still on the
	// build's prompt, and opening it says what creating it did.
	info.Warnings = append(info.Warnings, resumed.Session.StartWarnings()...)
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
	if model != "" && !f.servesModel(model) {
		model = ""
	}
	return f.runtimeFor(meta.Profile, model)
}

// Configure applies a configuration change to a session.
//
// The prompt overrides are the settings' own prompt changes, the models
// are the ones the settings offer, and the reasoning level is one the
// session's chat model takes. A model the settings do not name is refused,
// and so is a level the model does not take.
//
// The models come first: a switch may move the session to a model whose
// levels differ, and the level is checked against the model the session
// talks to once the switch is done. A level the new model does not take is
// reset by the switch itself, so the result says what the session runs
// at.
func (f *Server) Configure(ctx context.Context, sess *poo.Session, params protocol.ConfigureParams) (protocol.ConfigureResult, error) {
	runtime, err := f.runtimeFor("", "")
	if err != nil {
		return protocol.ConfigureResult{}, err
	}
	roles := rolesOf(sess, runtime)
	if params.PromptOverrides != nil {
		edits, err := runtime.ApplyPromptOverrides(promptOverrides(params.PromptOverrides))
		if err != nil {
			return protocol.ConfigureResult{}, err
		}
		if err := sess.SetPrompt(edits...); err != nil {
			return protocol.ConfigureResult{}, err
		}
	}
	if params.Models != nil && params.Models.Chat != "" {
		model, ok := f.modelFor(params.Models.Chat)
		if !ok {
			return protocol.ConfigureResult{}, poo.ErrUnknownModel
		}
		transport, err := runtime.Transport(model)
		if err != nil {
			return protocol.ConfigureResult{}, err
		}
		changed, err := sess.SetModel(model, transport)
		if err != nil {
			return protocol.ConfigureResult{}, err
		}
		roles.Chat = changed.To
	}
	if params.Models != nil && params.Models.Decider != "" {
		resolved, ok := f.modelFor(params.Models.Decider)
		if !ok {
			return protocol.ConfigureResult{}, poo.ErrUnknownModel
		}
		model, decider, err := runtime.DeciderFor(resolved.Name)
		if err != nil {
			return protocol.ConfigureResult{}, err
		}
		if err := sess.SetDecider(model, decider); err != nil {
			return protocol.ConfigureResult{}, err
		}
		roles.Decider = model.Name
	}
	// An empty level is "leave it alone" on the wire, so a session that
	// has taken a level cannot be returned to its never-set state; off
	// is the level that behaves the same at the transport.
	if params.Reasoning != "" {
		if err := sess.SetReasoning(params.Reasoning); err != nil {
			return protocol.ConfigureResult{}, err
		}
	}
	return protocol.ConfigureResult{Models: roles, Reasoning: sess.ReasoningLevel()}, nil
}

// reasoningMode maps a model's reasoning control onto the wire's name
// for it. The core's zero mode is the one that cannot be moved.
func reasoningMode(mode poo.ReasoningMode) string {
	switch mode {
	case poo.ReasoningEffort:
		return protocol.ReasoningModeEffort
	case poo.ReasoningBudget:
		return protocol.ReasoningModeBudget
	case poo.ReasoningToggle:
		return protocol.ReasoningModeToggle
	default:
		return protocol.ReasoningModeNone
	}
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
		if err := f.adoptCatalog(runtime); err != nil {
			return nil, err
		}
	}
	models := runtime.Catalog.List()
	out := make([]protocol.ModelInfo, 0, len(models))
	for _, model := range models {
		// A model that says nothing about its reasoning control is one
		// that cannot move: the catalog says so rather than leaving a
		// client to read the empty string either way.
		mode := reasoningMode(model.Reasoning.Mode)
		out = append(out, protocol.ModelInfo{
			Name:            model.Name,
			ModelID:         model.ModelID,
			API:             string(model.API),
			Ops:             opNames(model.Ops),
			Inputs:          modalityNames(model.Inputs),
			ContextLimit:    model.ContextLimit,
			InputLimit:      model.InputLimit,
			MaxOutput:       model.MaxOutput,
			ToolsMode:       string(model.Tools.Mode),
			ReasoningLevels: model.ReasoningLevels,
			ReasoningMode:   mode,
		})
	}
	return out, nil
}

// Settings returns the effective settings, with the credentials
// redacted.
//
// The keys never leave the process: an endpoint's key comes from the
// environment it names or from the settings themselves, and the
// document carries neither — a base URL is redacted too, because one
// may carry credentials of its own.
func (f *Server) Settings(ctx context.Context) (map[string]any, error) {
	settings := f.effective
	redacted := *settings
	// The endpoints the catalog serves are the settings' own and the ones the models file
	// declares. A name both declare is the settings', which is the declaration that wins.
	layer := settings.CatalogOptions().File
	redacted.Endpoints = make(map[string]config.Endpoint, len(settings.Endpoints)+len(layer.Endpoints))
	for name, endpoint := range settings.Endpoints {
		redacted.Endpoints[name] = endpoint
	}
	for name, endpoint := range layer.Endpoints {
		if _, ok := redacted.Endpoints[name]; ok {
			continue
		}
		redacted.Endpoints[name] = config.Endpoint{
			Endpoint:  endpoint.Endpoint,
			APIKeyEnv: endpoint.APIKeyEnv,
			APIKey:    endpoint.APIKey,
			SkipProbe: endpoint.SkipProbe,
		}
	}
	// One redaction, over everything the document names: two loops that each redact a
	// field would drift the day one of them learns about a new one.
	for name, endpoint := range redacted.Endpoints {
		endpoint.Endpoint = session.LoggedEndpoint(endpoint.Endpoint)
		endpoint.APIKey = ""
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
//
// The roles are the models the session runs with: the chat model the
// runtime was built for, the decision model the caller resolved, and
// the compaction model the strategies reconstruct with. A model with no
// name is no model, so the header records no such role.
func (f *Server) created(runtime *config.Runtime, mode, workDir string, decider poo.ModelInfo) (*poo.SessionCreatedRecord, error) {
	model := runtime.Options.Model
	chat := session.LoggedModel(model)
	roles := poo.ModelRoles{Chat: &chat}
	if decider.Name != "" {
		logged := session.LoggedModel(decider)
		roles.Decider = &logged
	}
	if runtime.Options.SideModel.ModelID != "" {
		side := session.LoggedModel(runtime.Options.SideModel)
		roles.Compaction = &side
	}
	return &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"),
		SessionMetadata: poo.SessionMetadata{
			Format:       poo.FormatVersion,
			SessionID:    session.NewID(),
			Mode:         mode,
			CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
			WorkingDir:   workDir,
			Tools:        runtime.SessionTools(),
			Profile:      runtime.Profile.Name,
			Roles:        roles,
			Build:        f.build.Build,
			Model:        poo.SessionModel{ModelID: model.ModelID, Endpoint: session.LoggedEndpoint(model.Endpoint), MaxOutput: model.MaxOutput},
			ContextLimit: model.ContextLimit,
			ToolsMode:    poo.ToolsMode(model.Tools.Mode),
		},
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
	answer, err := c.AskWithMessage(ctx, ask)
	return answer.Decision, err
}

// AskWithMessage sends the ask to the connected client and waits for
// its answer, with the message the answer carries. A client without
// the message seam is asked through its Ask.
func (c *lateClient) AskWithMessage(ctx context.Context, ask perm.Ask) (perm.Answer, error) {
	client := c.current()
	if client == nil {
		return perm.Answer{Decision: perm.DecisionDefer}, nil
	}
	if messenger, ok := client.(perm.MessageClient); ok {
		return messenger.AskWithMessage(ctx, ask)
	}
	decision, err := client.Ask(ctx, ask)
	return perm.Answer{Decision: decision}, err
}

// contains says whether the list holds the value.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
