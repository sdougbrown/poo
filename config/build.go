package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/handoff"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/perm"
	permissive "github.com/sdougbrown/poo/perm/decide"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/provider/catalog"
	"github.com/sdougbrown/poo/provider/openai"
	"github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/protect"
	"github.com/sdougbrown/poo/tool/read"
)

// The names the settings and the profiles use.
const (
	// RoleChat is the name of the chat role.
	RoleChat = "chat"
	// RoleDecider is the name of the decider role.
	RoleDecider = "decider"
	// BaseSection is the name of the prompt's base section.
	BaseSection = "base"
)

// The defaults the settings take when they do not say otherwise.
const (
	// DefaultSessionDir is where the session files live when the
	// settings do not say.
	DefaultSessionDir = "~/.local/state/poo/sessions"
	// DefaultSessionMode is the persistence mode a session runs in
	// when the settings do not say.
	DefaultSessionMode = "logged"
	// DefaultContextStrategy is how the view is compacted when the
	// settings do not say.
	DefaultContextStrategy = "handoff"
	// DefaultExecutor is the executor the commands run through when
	// the settings do not say.
	DefaultExecutor = "local"
)

// The overrides a command line makes.
//
// The precedence is the profile, then the settings file, then these:
// the last one that says something wins.
type Overrides struct {
	// Profile is the name of the profile the session is built from.
	Profile string
	// PermissionUnmatched is the action for a side-effecting call no
	// rule covers.
	PermissionUnmatched string
	// PermissionFallback is the decision when the asker chain is
	// exhausted.
	PermissionFallback string
	// SessionDir is the directory the session files live in.
	SessionDir string
	// SessionMode is the persistence mode: logged or ephemeral.
	SessionMode string
	// SystemPrompt is the file holding the prompt's base section.
	SystemPrompt string
	// AppendSystemPrompt is the text appended to the prompt's base
	// section.
	AppendSystemPrompt string
}

// BuildOptions are what a build needs.
//
// The binary says what it has: the tools it links, the executors it
// links, the profiles it offers and the askers it can ask. The
// settings say what the operator wants. The build puts the two
// together.
type BuildOptions struct {
	// Settings are the loaded settings.
	Settings *Settings
	// Overrides are the settings a command line changed.
	Overrides Overrides
	// WorkingDir is the directory the session works in.
	WorkingDir string
	// DefaultProfile is the profile a session runs under when neither
	// the command line nor the settings name one.
	DefaultProfile string
	// Tools are the tools the binary links, by name.
	Tools map[string]ToolFactory
	// Profiles are the profiles the binary links.
	Profiles []profile.Profile
	// Executors are the executors the binary links.
	Executors *bash.Registry
	// Askers are the names of the askers the binary can ask, in the
	// order the chain asks them when the settings do not say.
	Askers []string
	// Wire is the client the wire asker asks. Nil when the binary has
	// no wire.
	Wire perm.Client
	// Logger receives the catalog's warnings.
	Logger *slog.Logger
	// HTTPClient is the client the transports use.
	HTTPClient *http.Client
	// Build names the binary that owns the sessions.
	Build poo.BuildInfo
	// NoChatModel lets the build succeed without a chat model.
	//
	// A command that only shows the prompt does not need a model: the
	// prompt names the model, and a name may be empty.
	NoChatModel bool
}

// Runtime is what a build produced.
//
// It holds everything a session needs: the options, the store, the
// catalog, the policy and the askers, and the profiles the binary
// offers.
type Runtime struct {
	// Options are the session's options.
	Options poo.Options
	// Store is the store the session's records go to. It is nil in
	// the ephemeral mode, where nothing is written.
	Store *session.FileStore
	// Catalog is the model catalog the roles are resolved in.
	Catalog *catalog.Catalog
	// Policy is the permission policy.
	Policy *perm.Policy
	// Askers are the asker chain, in order.
	Askers []perm.Asker
	// Profiles are the profiles the binary offers.
	Profiles []poo.Profile
	// Profile is the profile the session is built from.
	Profile poo.Profile
	// Models are the models the settings offer, by name.
	Models map[string]poo.ModelInfo
	// Settings are the settings the runtime was built from.
	Settings *Settings
	// Warning is the warning a load prints. It is empty when there is
	// nothing to warn about.
	Warning string
	// Prompt is the prompt the settings ask for.
	Prompt []func(*prompt.Builder)
	// FinishSchema is the JSON schema a finish result is validated
	// against. It is nil when the settings name no schema.
	FinishSchema json.RawMessage
	// SessionDir is the directory the session files live in.
	SessionDir string
	// SessionMode is the persistence mode the session runs in.
	SessionMode string
	// Env is the environment the tools' commands get.
	Env []string
	// ProtectedPaths are the paths the tools must not touch.
	ProtectedPaths []string
	// toolNames are the names of the tools the session offers, in
	// offer order.
	toolNames []string
	// httpClient is the client the transports and the probes use.
	httpClient *http.Client
}

// ToolFactory builds a tool from the settings.
//
// The factory is the binary's: the binary knows what a tool is, and
// the settings know how it is set up. A tool that needs nothing from
// the settings ignores the options.
type ToolFactory func(ToolOptions) poo.Tool

// ToolOptions are the settings' options for the tools.
type ToolOptions struct {
	// Executor is the executor the bash tool runs its commands
	// through.
	Executor bash.Executor
	// Scrub is the rules for the child environment.
	Scrub bash.Scrub
	// ProtectedPaths are the paths the tools must not touch.
	ProtectedPaths *protect.Set
}

// Build turns the settings into the runtime a session is built from.
//
// The build is the whole mapping: the profile's tools, the catalog,
// the policy and its askers, the executor, the prompt and the store.
// It builds options and nothing else: every piece it returns is a
// thing a Go consumer could build by hand.
func Build(opts BuildOptions) (*Runtime, error) {
	settings := opts.Settings
	if settings == nil {
		return nil, &Error{Code: CodeInvalid, Message: "the build has no settings"}
	}
	workingDir := opts.WorkingDir
	if workingDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("config: reading the working directory: %w", err)
		}
		workingDir = wd
	}

	runtime := &Runtime{Settings: settings, httpClient: opts.HTTPClient}

	if err := runtime.resolveProfile(opts); err != nil {
		return nil, err
	}
	if err := runtime.resolveModels(opts); err != nil {
		return nil, err
	}
	if err := runtime.resolveTools(opts, workingDir); err != nil {
		return nil, err
	}
	if err := runtime.resolvePolicy(opts); err != nil {
		return nil, err
	}
	if err := runtime.resolveSession(opts, workingDir); err != nil {
		return nil, err
	}
	if err := runtime.resolvePrompt(opts, workingDir); err != nil {
		return nil, err
	}
	return runtime, nil
}

// resolveProfile picks the profile the session is built from.
//
// The command line names the profile, and the settings do, and the
// binary has a default. The profile names the tools the session
// offers, and every tool it names must be one the binary links.
func (r *Runtime) resolveProfile(opts BuildOptions) error {
	name := opts.Overrides.Profile
	if name == "" {
		name = r.Settings.Profile
	}
	if name == "" {
		name = opts.DefaultProfile
	}
	if name == "" && len(opts.Profiles) > 0 {
		name = opts.Profiles[0].Name
	}
	if name == "" {
		return &Error{Code: CodeUnsupported, Message: "the build has no profile to run under"}
	}
	chosen, err := profile.Find(opts.Profiles, name)
	if err != nil {
		return &Error{
			Code:    CodeUnsupported,
			Field:   "profile",
			Message: err.Error(),
			Err:     err,
		}
	}
	r.Profile = poo.Profile{Name: chosen.Name, Unmatched: chosen.Unmatched}
	for _, tool := range chosen.Tools {
		factory, ok := opts.Tools[tool]
		if !ok || factory == nil {
			return &Error{
				Code:    CodeUnsupported,
				Field:   "profile",
				Message: fmt.Sprintf("the profile %q offers the tool %q, and this binary does not link it", chosen.Name, tool),
			}
		}
		r.Profile.Tools = append(r.Profile.Tools, toolInfo(factory))
		r.toolNames = append(r.toolNames, tool)
	}
	// The core adds the job tools on its own whenever a linked tool may
	// run in the background, and the profile offers them with it.
	r.Profile.Tools = withJobTools(r.Profile.Tools)
	r.Profiles = resolveProfiles(opts)
	return nil
}

// toolInfo describes a tool the way the profile and the wire name it.
func toolInfo(factory ToolFactory) poo.ToolInfo {
	spec := factory(ToolOptions{}).Spec()
	return poo.ToolInfo{
		Name:       spec.Name,
		Class:      spec.Class,
		Background: spec.Background,
	}
}

// withJobTools adds the core's job tools to a profile's tools.
//
// The core adds the job tools whenever a linked tool may run in the
// background, so the profile offers them with it.
func withJobTools(tools []poo.ToolInfo) []poo.ToolInfo {
	for _, tool := range tools {
		if tool.Background != poo.BackgroundNever {
			return append(tools,
				poo.ToolInfo{Name: profile.JobsToolName, Class: poo.ClassReadOnly, Background: poo.BackgroundNever},
				poo.ToolInfo{Name: profile.CancelJobToolName, Class: poo.ClassReadOnly, Background: poo.BackgroundNever})
		}
	}
	return tools
}

// resolveProfiles resolves every profile the binary links.
//
// A profile that names a tool the binary does not link offers the
// tools it has: the profile cannot be run with the others, and a
// profile that cannot be run is not one the binary offers.
func resolveProfiles(opts BuildOptions) []poo.Profile {
	var out []poo.Profile
	for _, definition := range opts.Profiles {
		resolved := poo.Profile{Name: definition.Name, Unmatched: definition.Unmatched}
		for _, tool := range definition.Tools {
			factory, ok := opts.Tools[tool]
			if !ok || factory == nil {
				continue
			}
			resolved.Tools = append(resolved.Tools, toolInfo(factory))
		}
		if len(definition.Tools) > 0 && len(resolved.Tools) == 0 {
			continue
		}
		resolved.Tools = withJobTools(resolved.Tools)
		out = append(out, resolved)
	}
	return out
}

// CatalogSettings returns the catalog's view of the settings.
//
// The catalog reads the endpoints, the models, the roles and the
// sampling overrides from the settings, and the settings say nothing
// else about the models.
func (s *Settings) CatalogSettings() catalog.Settings {
	settings := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{},
		Models:    map[string]catalog.ModelSettings{},
		Roles:     map[string]string{},
		Sampling:  map[string]poo.Sampling{},
	}
	for name, endpoint := range s.Endpoints {
		settings.Endpoints[name] = catalog.EndpointSettings{
			BaseURL:   endpoint.BaseURL,
			APIKeyEnv: endpoint.APIKeyEnv,
		}
	}
	for name, model := range s.Models {
		settings.Models[name] = catalog.ModelSettings{
			Endpoint:     model.Endpoint,
			ModelID:      model.ModelID,
			API:          poo.API(model.API),
			ToolsMode:    poo.ToolsMode(model.ToolsMode),
			Ops:          ops(model.Ops),
			Inputs:       modalities(model.Inputs),
			ContextLimit: model.ContextLimit,
		}
	}
	for role, model := range s.Roles {
		settings.Roles[role] = model
	}
	if s.Reasoning.Chat != nil {
		settings.Reasoning = *s.Reasoning.Chat
	}
	for name, sampling := range s.Sampling {
		settings.Sampling[name] = poo.Sampling{
			Temperature: sampling.Temperature,
			TopP:        sampling.TopP,
			TopK:        sampling.TopK,
			MinP:        sampling.MinP,
		}
	}
	return settings
}

// resolveModels builds the catalog and resolves the roles.
//
// The catalog is built from the settings' endpoints, models, roles and
// sampling overrides. The chat model is the one a session cannot run
// without, so a chat role the catalog cannot resolve is a failure.
func (r *Runtime) resolveModels(opts BuildOptions) error {
	cat := catalog.New(catalog.Options{
		Settings:   r.Settings.CatalogSettings(),
		Logger:     opts.Logger,
		HTTPClient: opts.HTTPClient,
	})
	if err := cat.CheckRoles(); err != nil {
		if !opts.NoChatModel || !isChatRole(err) {
			return fmt.Errorf("config: %w", err)
		}
	}
	r.Catalog = cat
	r.Models = map[string]poo.ModelInfo{}
	for _, model := range cat.List() {
		r.Models[model.Name] = model
	}
	return nil
}

// linkedTool reports whether the binary links a tool.
//
// The core's own job tools are linked by the core, so a rule may name
// them.
func linkedTool(opts BuildOptions, name string) bool {
	if name == profile.JobsToolName || name == profile.CancelJobToolName {
		return true
	}
	_, ok := opts.Tools[name]
	return ok
}

// isChatRole reports whether a failure is about the chat role.
//
// The chat model is the one a session cannot run without, so its
// failure stops a build. A build that only shows a prompt goes on
// without one.
func isChatRole(err error) bool {
	var unresolved *catalog.UnresolvedError
	if !errors.As(err, &unresolved) {
		return false
	}
	return unresolved.Role == catalog.RoleChat
}

// ops returns the operations the settings name.
func ops(names []string) []poo.Op {
	if len(names) == 0 {
		return nil
	}
	out := make([]poo.Op, 0, len(names))
	for _, name := range names {
		out = append(out, poo.Op(name))
	}
	return out
}

// modalities returns the modalities the settings name.
func modalities(names []string) []poo.Modality {
	if len(names) == 0 {
		return nil
	}
	out := make([]poo.Modality, 0, len(names))
	for _, name := range names {
		out = append(out, poo.Modality(name))
	}
	return out
}

// resolveTools builds the tools the session offers.
//
// The tools are the ones the profile names. The bash tool runs its
// commands through the executor the settings name, with the sandbox's
// options and the protected paths the settings name. The child
// environment is the session's own, less the names the settings scrub
// and the names the endpoints' credentials are read from.
func (r *Runtime) resolveTools(opts BuildOptions, workingDir string) error {
	settings := r.Settings
	protected, err := protect.Compile(settings.Tools.ProtectedPaths)
	if err != nil {
		return &Error{
			Code:    CodeInvalid,
			Field:   "tools.protected_paths",
			Message: "the protected paths do not compile",
			Err:     err,
		}
	}
	r.ProtectedPaths = append([]string(nil), settings.Tools.ProtectedPaths...)

	scrub := bash.Scrub{
		Names:       append([]string(nil), settings.Tools.Env.Scrub...),
		Passthrough: append([]string(nil), settings.Tools.Env.Passthrough...),
	}
	if r.Catalog != nil {
		scrub.Names = append(scrub.Names, r.Catalog.CredentialEnvNames()...)
	}
	r.Env = bash.ScrubEnv(os.Environ(), scrub)

	executorName := DefaultExecutor
	if settings.Tools.Bash.Executor != nil {
		executorName = *settings.Tools.Bash.Executor
	}
	if opts.Executors == nil {
		return &Error{
			Code:    CodeUnsupported,
			Field:   "tools.bash.executor",
			Message: "this binary links no executor",
		}
	}
	if !opts.Executors.Has(executorName) {
		return &Error{
			Code:    CodeUnsupported,
			Field:   "tools.bash.executor",
			Message: fmt.Sprintf("this binary links no executor named %q", executorName),
		}
	}

	toolOptions := ToolOptions{Scrub: scrub, ProtectedPaths: protected}
	tools := map[string]poo.Tool{}
	var order []string
	for _, tool := range r.toolNames {
		factory, ok := opts.Tools[tool]
		if !ok || factory == nil {
			continue
		}
		if tool == "bash" {
			executor, err := opts.Executors.New(executorName, bash.Options{
				Masker: protected,
				Sandbox: bash.Sandbox{
					DisableNetwork: settings.Tools.Bwrap.Network != nil && !*settings.Tools.Bwrap.Network,
					ROBinds:        settings.Tools.Bwrap.ROBinds,
					RWBinds:        settings.Tools.Bwrap.RWBinds,
				},
			})
			if err != nil {
				return &Error{
					Code:    CodeUnsupported,
					Field:   "tools.bash.executor",
					Message: err.Error(),
					Err:     err,
				}
			}
			toolOptions.Executor = executor
		}
		tools[tool] = factory(toolOptions)
		order = append(order, tool)
	}

	r.Options.Tools = tools
	r.Options.ToolOrder = order
	return nil
}

// resolvePolicy builds the permission policy and its asker chain.
//
// The unmatched action comes from the command line, then the settings,
// then the profile. The asker chain is the one the settings name, or
// the one the binary asks by default. The decide asker needs a decider
// model, so a chain that names it without one is a failure.
func (r *Runtime) resolvePolicy(opts BuildOptions) error {
	settings := r.Settings
	var rules []perm.Rule
	for _, rule := range settings.Permission.Rules {
		// A settings file cannot enable a tool the binary lacks: a
		// rule for a tool that is not there matches nothing, and the
		// operator thinks it does something.
		if rule.Tool != "" && !linkedTool(opts, rule.Tool) {
			return &Error{
				Code:    CodeUnsupported,
				Field:   "permission.rules",
				Message: fmt.Sprintf("the rule matches the tool %q, and this binary does not link it", rule.Tool),
			}
		}
		rules = append(rules, perm.Rule{
			Tool:          rule.Tool,
			Class:         poo.ToolClass(rule.Class),
			CommandPrefix: rule.CommandPrefix,
			Action:        perm.Action(rule.Action),
		})
	}
	unmatched := perm.Action(r.Profile.Unmatched)
	if settings.Permission.Unmatched != nil {
		unmatched = perm.Action(*settings.Permission.Unmatched)
	}
	if opts.Overrides.PermissionUnmatched != "" {
		unmatched = perm.Action(opts.Overrides.PermissionUnmatched)
	}
	fallback := perm.DefaultFallback
	if settings.Permission.Fallback != nil {
		fallback = perm.Action(*settings.Permission.Fallback)
	}
	if opts.Overrides.PermissionFallback != "" {
		fallback = perm.Action(opts.Overrides.PermissionFallback)
	}
	doomLoop := perm.DoomLoop{}
	if settings.Permission.DoomLoop.Threshold != nil {
		doomLoop.Threshold = *settings.Permission.DoomLoop.Threshold
	}
	if settings.Permission.DoomLoop.NearThreshold != nil {
		doomLoop.NearThreshold = *settings.Permission.DoomLoop.NearThreshold
	}
	if settings.Permission.DoomLoop.Action != nil {
		doomLoop.Action = perm.Action(*settings.Permission.DoomLoop.Action)
	}
	chainTimeout := perm.DefaultChainTimeout
	if settings.Permission.ChainTimeoutS != nil {
		chainTimeout = time.Duration(*settings.Permission.ChainTimeoutS) * time.Second
	}

	askers, err := r.resolveAskers(opts, settings.Permission.Askers)
	if err != nil {
		return err
	}

	policy, err := perm.New(perm.Options{
		Rules:        rules,
		Unmatched:    unmatched,
		Fallback:     fallback,
		DoomLoop:     doomLoop,
		Askers:       askers,
		ChainTimeout: chainTimeout,
		WorkingDir:   opts.WorkingDir,
	})
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	r.Policy = policy
	r.Askers = askers
	r.Warning = perm.Warning(policy, askers)
	return nil
}

// resolveAskers builds the asker chain.
//
// The chain is the one the settings name, or the one the binary asks by
// default. The decide asker is only in the chain when a decider model
// is configured: a chain that asks a model that is not there would wait
// for an answer that cannot come. The wire asker is only in the chain
// when the binary has a wire client.
func (r *Runtime) resolveAskers(opts BuildOptions, named []string) ([]perm.Asker, error) {
	names := named
	if len(names) == 0 {
		names = opts.Askers
	}
	decider, deciderErr := r.DeciderModel()
	var askers []perm.Asker
	for _, name := range names {
		switch name {
		case "decide":
			if deciderErr != nil {
				if len(named) > 0 {
					// The settings name the decide asker and no
					// decider model is configured: the ask would
					// wait for an answer that cannot come.
					return nil, &Error{
						Code:    CodeUnavailable,
						Field:   "permission.askers",
						Message: "the decide asker needs a decider model: roles.decider names one",
					}
				}
				// The binary's own chain asks the decider, and there
				// is no decider: the chain is the one the binary has.
				continue
			}
			asker, err := r.newDeciderAsker(decider)
			if err != nil {
				return nil, err
			}
			askers = append(askers, asker)
		case "wire":
			if opts.Wire == nil {
				return nil, &Error{
					Code:    CodeUnsupported,
					Field:   "permission.askers",
					Message: "the wire asker needs a wire client, and this binary has none",
				}
			}
			askers = append(askers, perm.NewWireAsker(opts.Wire, 0))
		default:
			return nil, &Error{
				Code:    CodeInvalid,
				Field:   "permission.askers",
				Message: fmt.Sprintf("the asker %q is not one the settings know", name),
			}
		}
	}
	return askers, nil
}

// newDeciderAsker builds the asker that asks a decision model.
func (r *Runtime) newDeciderAsker(model poo.ModelInfo) (perm.Asker, error) {
	decider, err := r.newDecider(model)
	if err != nil {
		return nil, err
	}
	askerOptions := permissive.Options{}
	if value := r.Settings.Permission.Decide.Approve; value != nil {
		askerOptions.Approve = *value
	}
	if value := r.Settings.Permission.Decide.Deny; value != nil {
		askerOptions.Deny = *value
	}
	if value := r.Settings.Permission.Decide.TimeoutMs; value != nil {
		askerOptions.Timeout = time.Duration(*value) * time.Millisecond
	}
	return permissive.New(decider, askerOptions), nil
}

// resolveSession builds the session's store and the session's options.
//
// The store is the session's home in the logged mode, and there is
// none in the ephemeral mode: an ephemeral session writes nothing.
func (r *Runtime) resolveSession(opts BuildOptions, workingDir string) error {
	settings := r.Settings
	mode := DefaultSessionMode
	if settings.Session.Mode != nil {
		mode = *settings.Session.Mode
	}
	if opts.Overrides.SessionMode != "" {
		mode = opts.Overrides.SessionMode
	}
	dir := DefaultSessionDir
	if settings.Session.Dir != nil {
		dir = *settings.Session.Dir
	}
	if opts.Overrides.SessionDir != "" {
		dir = opts.Overrides.SessionDir
	}
	expanded, err := expandHome(dir)
	if err != nil {
		return &Error{Code: CodeInvalid, Field: "session.dir", Message: "the session directory cannot be resolved", Err: err}
	}
	r.SessionDir = expanded
	r.SessionMode = mode

	if mode == "logged" {
		store, err := session.NewStore(expanded, session.WithBuild(opts.Build))
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		r.Store = store
	}

	model, err := r.ChatModel()
	if err != nil {
		if !opts.NoChatModel {
			return err
		}
	} else {
		r.Options.Model = model
		// The session talks to the chat model, so the transport is
		// part of the runtime's options. A consumer that changes the
		// model builds its own transport for it.
		transport, err := r.Transport(model)
		if err != nil {
			return err
		}
		r.Options.Transport = transport
	}
	r.Options.Profile = r.Profile.Name
	r.Options.Profiles = r.Profiles
	r.Options.Models = r.Models
	r.Options.WorkingDir = workingDir
	r.Options.Env = r.Env
	r.Options.ProtectedPaths = r.ProtectedPaths
	r.Options.MaxTurns = settings.Limits.MaxTurns
	r.Options.MaxRunTokens = settings.Limits.MaxTokensPerRun
	if settings.Limits.MaxDurationS > 0 {
		r.Options.MaxDuration = time.Duration(settings.Limits.MaxDurationS) * time.Second
	}
	if settings.Limits.MaxContinuations > 0 {
		r.Options.MaxContinuations = settings.Limits.MaxContinuations
	}
	if settings.Context.NoticeRatio != nil {
		r.Options.NoticeRatio = *settings.Context.NoticeRatio
	}
	if settings.Context.HardRatio != nil {
		r.Options.HardRatio = *settings.Context.HardRatio
	}
	r.Options.Jobs = poo.JobOptions{
		MaxRunning:         settings.Jobs.MaxRunning,
		Timeout:            time.Duration(settings.Jobs.TimeoutS) * time.Second,
		MaxUnattendedWakes: settings.Jobs.MaxUnattendedWakes,
	}
	if settings.Jobs.OnIdle != nil {
		r.Options.Jobs.OnIdle = poo.OnIdle(*settings.Jobs.OnIdle)
	}
	if settings.Tools.Bash.TimeoutS != nil {
		r.Options.Settings.BashTimeoutSeconds = *settings.Tools.Bash.TimeoutS
	}
	r.Options.Settings.ObservationCapChars = bash.ObservationCap(model.ContextLimit)
	r.Options.Settings.ReadOutputCapChars = read.OutputCap(model.ContextLimit)

	strategy := DefaultContextStrategy
	if settings.Context.Strategy != nil {
		strategy = *settings.Context.Strategy
	}
	if strategy == "handoff" {
		handoffOptions := handoff.Options{}
		if settings.Context.Handoff.Ratio != nil {
			handoffOptions.Ratio = *settings.Context.Handoff.Ratio
		}
		if settings.Context.Handoff.MaxTokens != nil {
			handoffOptions.MaxTokens = *settings.Context.Handoff.MaxTokens
		}
		r.Options.Compactor = handoff.New(handoffOptions)
	}
	return nil
}

// resolvePrompt builds the prompt the settings ask for.
//
// The base file and the replacement are mutually exclusive, and the
// check has already refused the settings that hold both. The sections
// are the settings' own, and the removed ones are taken away.
func (r *Runtime) resolvePrompt(opts BuildOptions, workingDir string) error {
	settings := r.Settings
	baseFile := settings.SystemPrompt.BaseFile
	if opts.Overrides.SystemPrompt != "" {
		baseFile = opts.Overrides.SystemPrompt
	}
	appendText := settings.SystemPrompt.Append
	if opts.Overrides.AppendSystemPrompt != "" {
		appendText = opts.Overrides.AppendSystemPrompt
	}
	if settings.SystemPrompt.Replace != "" {
		r.Options.SystemPrompt = settings.SystemPrompt.Replace
	}
	if baseFile != "" {
		text, err := os.ReadFile(baseFile)
		if err != nil {
			return &Error{
				Code:    CodeInvalid,
				Field:   "system_prompt.base_file",
				Message: "the file is not there",
				Err:     err,
			}
		}
		r.Prompt = append(r.Prompt, func(b *prompt.Builder) {
			b.Set(BaseSection, string(text))
		})
	}
	if appendText != "" {
		r.Prompt = append(r.Prompt, func(b *prompt.Builder) {
			b.Append(BaseSection, appendText)
		})
	}
	for name, text := range settings.SystemPrompt.Sections {
		name, text := name, text
		r.Prompt = append(r.Prompt, func(b *prompt.Builder) {
			b.Set(name, text)
		})
	}
	for _, name := range settings.SystemPrompt.Remove {
		name := name
		r.Prompt = append(r.Prompt, func(b *prompt.Builder) {
			b.Remove(name)
		})
	}
	if settings.Finish.SchemaFile != "" {
		data, err := os.ReadFile(settings.Finish.SchemaFile)
		if err != nil {
			return &Error{
				Code:    CodeInvalid,
				Field:   "finish.schema_file",
				Message: "the file is not there",
				Err:     err,
			}
		}
		r.FinishSchema = json.RawMessage(data)
	}
	return nil
}

// CreateSession creates a session in the store and returns it.
//
// The session's records go to the store, and the store holds the
// session's authority until it is closed. A runtime without a store is
// in the ephemeral mode, and it creates no session.
func (r *Runtime) CreateSession(ctx context.Context, created *poo.SessionCreated) (*poo.Session, error) {
	if r.Store == nil {
		return nil, &Error{
			Code:    CodeUnsupported,
			Message: "the session has no store: the settings are in the ephemeral mode",
		}
	}
	if _, err := r.Store.Create(ctx, created); err != nil {
		return nil, err
	}
	opts := r.Options
	opts.Sink = r.Store
	opts.SessionID = created.SessionID
	return poo.New(opts, poo.WithPolicy(r.Policy))
}

// ResumeSession loads a session from the store and restores it.
//
// The restored session writes to the store it was loaded from, and the
// store holds the session's authority until it is closed. The load
// recovers the session: a torn tail is truncated, an unsettled run is
// marked interrupted, and the job results that were never delivered are
// queued again.
func (r *Runtime) ResumeSession(ctx context.Context, id string) (*session.Resumed, error) {
	if r.Store == nil {
		return nil, &Error{
			Code:    CodeUnsupported,
			Message: "the session has no store: the settings are in the ephemeral mode",
		}
	}
	return session.Resume(ctx, r.Store, id, r.Options)
}

// ChatModel returns the model the session talks to.
//
// A chat role the catalog cannot resolve is a failure: a session
// without a chat model cannot run.
func (r *Runtime) ChatModel() (poo.ModelInfo, error) {
	if r.Catalog == nil {
		return poo.ModelInfo{}, &Error{Code: CodeInvalid, Message: "the build has no catalog"}
	}
	model, err := r.Catalog.Role(catalog.RoleChat)
	if err != nil {
		return poo.ModelInfo{}, fmt.Errorf("config: %w", err)
	}
	return model, nil
}

// DeciderModel returns the model the session asks about permissions.
//
// A session without a decider model has no decider: the failure is
// only a failure when the caller asked for one.
func (r *Runtime) DeciderModel() (poo.ModelInfo, error) {
	if r.Catalog == nil {
		return poo.ModelInfo{}, &Error{Code: CodeInvalid, Message: "the build has no catalog"}
	}
	model, err := r.Catalog.Role(catalog.RoleDecider)
	if err != nil {
		return poo.ModelInfo{}, fmt.Errorf("config: %w", err)
	}
	return model, nil
}

// Transport returns the transport for a model.
//
// The transport talks to the endpoint the model is served by, with the
// key the settings' endpoint names. A key is read from the
// environment and from nowhere else.
func (r *Runtime) Transport(model poo.ModelInfo) (poo.ChatTransport, error) {
	apiKey := r.apiKey(model.Name)
	transport := openai.New(openai.Config{
		BaseURL:    model.Endpoint,
		Model:      model.ModelID,
		APIKey:     apiKey,
		ToolsMode:  string(model.Tools.Mode),
		Info:       model,
		Sampling:   model.Sampling,
		HTTPClient: r.httpClient,
	})
	return transport, nil
}

// httpClient is the client the transports use. It is nil unless the
// build named one.
var _ = http.DefaultClient

// Decider returns the decision model and the decider that asks it.
//
// A session without a decider model has no decider: the caller asks
// for one only when the settings name a decider role.
func (r *Runtime) Decider() (poo.ModelInfo, decide.Decider, error) {
	model, err := r.DeciderModel()
	if err != nil {
		return poo.ModelInfo{}, nil, err
	}
	decider, err := r.newDecider(model)
	if err != nil {
		return poo.ModelInfo{}, nil, err
	}
	return model, decider, nil
}

// newDecider builds the decider for a model.
//
// The decider asks the model over the systemone endpoint, and the
// model has to serve that operation: a model that cannot answer a
// question is not a decider.
func (r *Runtime) newDecider(model poo.ModelInfo) (decide.Decider, error) {
	timeout := time.Duration(0)
	if r.Settings.Decider.TimeoutMs != nil {
		timeout = time.Duration(*r.Settings.Decider.TimeoutMs) * time.Millisecond
	}
	decider, err := openai.NewSystemOneDeciderFor(model, openai.SystemOneConfig{
		APIKey:     r.apiKey(model.Name),
		Timeout:    timeout,
		HTTPClient: r.httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return decider, nil
}

// apiKey returns the key the settings' endpoint for a model names.
//
// The key is read from the environment variable the settings name, and
// from nowhere else: a settings file never carries a key.
func (r *Runtime) apiKey(model string) string {
	settings, ok := r.Settings.Models[model]
	if !ok {
		return ""
	}
	endpoint, ok := r.Settings.Endpoints[settings.Endpoint]
	if !ok || endpoint.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(endpoint.APIKeyEnv)
}

// expandHome expands the leading ~ of a path.
func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// SessionDirectory returns the directory the session files live in.
//
// The settings' directory, with the command line's override, and the
// leading ~ expanded.
func SessionDirectory(settings *Settings, overrides Overrides) (string, error) {
	dir := DefaultSessionDir
	if settings.Session.Dir != nil {
		dir = *settings.Session.Dir
	}
	if overrides.SessionDir != "" {
		dir = overrides.SessionDir
	}
	return expandHome(dir)
}

// RetentionPolicy returns the prune policy the settings name.
//
// Retention is opt-in: a policy with neither an age nor a count deletes
// nothing, and the prune fails with the store's no-policy failure.
func (s *Settings) RetentionPolicy() session.Policy {
	policy := session.Policy{}
	if s.Session.Retention.MaxAgeDays != nil {
		policy.MaxAge = time.Duration(*s.Session.Retention.MaxAgeDays) * 24 * time.Hour
	}
	if s.Session.Retention.MaxSessions != nil {
		policy.Keep = *s.Session.Retention.MaxSessions
	}
	return policy
}
