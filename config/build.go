package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
	"github.com/sdougbrown/poo/compact/handoff"
	"github.com/sdougbrown/poo/compact/mine"
	"github.com/sdougbrown/poo/compact/native"
	pointerstrategy "github.com/sdougbrown/poo/compact/pointer"
	"github.com/sdougbrown/poo/compact/summary"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/feed"
	"github.com/sdougbrown/poo/internal/caps"
	"github.com/sdougbrown/poo/mcp/manifest"
	"github.com/sdougbrown/poo/mcp/wire"
	"github.com/sdougbrown/poo/perm"
	permissive "github.com/sdougbrown/poo/perm/decide"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/prompt/agentsmd"
	"github.com/sdougbrown/poo/protect"
	"github.com/sdougbrown/poo/provider/anthropic"
	"github.com/sdougbrown/poo/provider/auth"
	"github.com/sdougbrown/poo/provider/catalog"
	"github.com/sdougbrown/poo/provider/openai"
	"github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/skill"
)

// The names the settings and the profiles use.
const (
	// BaseSection is the name of the prompt's base section.
	BaseSection = "base"
	// ProjectSection is the name of the prompt's project section: the
	// working directory's instructions the model reads.
	ProjectSection = "project"
	// UserSection is the name of the prompt's user section: the
	// operator's own instructions, which the project's follow.
	UserSection = "user"
	// ToolBash is the name of the bash tool.
	ToolBash = "bash"
	// ToolCallMCP is the name of the tool that calls a session's MCP
	// tools.
	ToolCallMCP = cli.CallMCPToolName
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
	// settings do not say. A run that hits the window reconstructs the
	// work from the retained transcript and carries on; a consumer that
	// wants the run to stop instead names handoff.
	DefaultContextStrategy = "mine"
	// DefaultExecutor is the executor the commands run through when
	// the settings do not say.
	DefaultExecutor = "local"
	// DefaultTruncationRetries is how many times a run asks the model to
	// say less when the settings do not say. A model that overruns its
	// output limit usually finishes on the retry. A run that does not
	// retry settles output_truncated on the first overrun.
	DefaultTruncationRetries = 1
)

// The overrides a command line makes.
//
// The precedence is the profile, then the settings file, then these:
// the last one that says something wins, except manifest paths add to
// the settings' paths.
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
	// FeedsPaths are additional feed manifest paths.
	FeedsPaths []string
	// MCPPaths are additional mcp manifest paths.
	MCPPaths []string
	// CLIRegistryPaths are additional cli registry manifest paths.
	CLIRegistryPaths []string
	// SkillsPaths are additional directories of skills.
	SkillsPaths []string
	// CredentialEnvNames are environment names a launcher provisions for
	// poo's own subsystems. They stay in the session environment, which the
	// feed and a cli entry's bindings read, and are removed from the
	// environment a model-invoked tool subprocess receives.
	CredentialEnvNames []string
	// ProjectPrompt asks for the working directory's project
	// instructions, whatever the settings say.
	ProjectPrompt bool
	// UserPromptFile names the file the user's instructions are read
	// from, whatever the settings say.
	UserPromptFile string
}

// The environment variables a shell or a launcher names a build's
// manifests and credentials by.
const (
	// EnvFeedsPath names the feed manifest paths.
	EnvFeedsPath = "POO_FEEDS_PATH"
	// EnvMCPPath names the mcp manifest paths.
	EnvMCPPath = "POO_MCP_PATH"
	// EnvCLIRegistryPath names the cli registry manifest paths.
	EnvCLIRegistryPath = "POO_CLI_REGISTRY_PATH"
	// EnvCredentialEnv names the environment names a launcher declares
	// for poo's own subsystems.
	EnvCredentialEnv = "POO_CREDENTIAL_ENV"
	// EnvSkillsPath names the directories a session offers skills from.
	EnvSkillsPath = "POO_SKILLS_PATH"
)

// EnvOverrideNames are the variables EnvOverrides reads, in the order it
// reads them. The same names are kept out of a tool's child environment.
var EnvOverrideNames = []string{EnvFeedsPath, EnvMCPPath, EnvCLIRegistryPath, EnvCredentialEnv, EnvSkillsPath}

// EnvOverrides adds what the environment names after the caller's own values.
//
// POO_FEEDS_PATH, POO_MCP_PATH, POO_CLI_REGISTRY_PATH and POO_SKILLS_PATH are
// path lists, and POO_CREDENTIAL_ENV is a list of environment names a launcher
// declares for poo's own subsystems. The returned overrides hold fresh slices,
// so appending to them never writes into the caller's.
func EnvOverrides(o Overrides) Overrides {
	paths := func(existing []string, name string) []string {
		result := append([]string{}, existing...)
		if value, ok := os.LookupEnv(name); ok {
			for _, path := range filepath.SplitList(value) {
				if path != "" {
					result = append(result, path)
				}
			}
		}
		return result
	}
	o.FeedsPaths = paths(o.FeedsPaths, EnvFeedsPath)
	o.MCPPaths = paths(o.MCPPaths, EnvMCPPath)
	o.CLIRegistryPaths = paths(o.CLIRegistryPaths, EnvCLIRegistryPath)
	o.CredentialEnvNames = paths(o.CredentialEnvNames, EnvCredentialEnv)
	o.SkillsPaths = paths(o.SkillsPaths, EnvSkillsPath)
	return o
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
	// MCPLauncher starts the MCP servers a session's profile selected.
	//
	// The settings and the profiles stay free of the client: the
	// launcher is the binary's one seam to it, so a binary that links
	// no launcher never links the client at all. A profile that selects
	// a server on a build with no launcher fails the build, because a
	// session of that build could not start what its profile selected.
	MCPLauncher MCPLauncher
	// Profiles are the profiles the binary links.
	Profiles []profile.Profile
	// Executors are the executors the binary links.
	Executors *exec.Registry
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
	// UserPromptFile is the file the binary reads the user's
	// instructions from when the settings name none. An empty value
	// leaves the feature with no default path.
	UserPromptFile string
	// PromptFS is the prompt directory the binary embeds.
	//
	// The files replace the sections they name after the binary's
	// embedded defaults and before the settings, so an operator's
	// `system_prompt` settings keep the last word. A nil FS keeps the
	// embedded defaults alone.
	PromptFS fs.FS
}

// MCPLauncher starts the selected MCP servers for a live session.
//
// The launcher is the binary's link to the client: the settings hold
// the declarations and the selection, and this is what turns the
// selection into the processes the session owns. It runs once per
// session, under the session's own lifetime context, and returns what
// that session closes when it ends. The launch is the launcher's
// transaction: a launcher that fails has started nothing of its own —
// whatever it started before the failure stops — and the failure names
// the server that did not start.
type MCPLauncher func(ctx context.Context, launch MCPLaunchSelection) (MCPRunning, error)

// MCPLaunchSelection is what one session's launch needs from the
// session that asked for it.
type MCPLaunchSelection struct {
	// SessionID is the session's own identifier: the value a
	// manifest's ${session:id} resolves to.
	SessionID string
	// WorkingDir is the directory the session works in, and the
	// directory the servers run in.
	WorkingDir string
	// Scrub is the rule the host environment is scrubbed with. The
	// servers run with the scrubbed environment plus the manifests'
	// own entries, and a manifest's explicit ${NAME} still resolves
	// through the environment lookup the scrub leaves alone.
	Scrub exec.Scrub
	// Servers are the selected servers' declarations, with their
	// templates unresolved: the launch resolves them from the values
	// here.
	Servers []manifest.Server
}

// MCPServer is one server a session started, as the tools over the servers
// read it.
//
// config does not import the tool packages, so the seam is structural: the
// per-server views the binary's call tool dispatches through answer to this,
// the way the generated toolset does. The settings read the same views for
// the session's snapshot, so what a handle lists is what the call reaches,
// and no second list stands between them.
type MCPServer interface {
	// Name is the server's declared name.
	Name() string
	// Tools returns the tools the server listed at startup.
	Tools() []wire.Tool
	// CallTool calls one of them, by the server's own name for it.
	CallTool(ctx context.Context, tool string, args json.RawMessage) (wire.CallResult, error)
}

// MCPRunning is what a launcher started for a session.
//
// The session closes it when it ends. Close is idempotent, and the
// processes it owns are stopped by it — nothing else reaps the
// servers.
type MCPRunning interface {
	// Close stops the server and waits for its process to end.
	Close() error
	// Servers returns the per-server views the tools over the servers read:
	// one per server the launch started, in the selection's order. The
	// snapshot and the dispatch read the same views, so a tool the handle
	// lists is one the call reaches.
	Servers() []MCPServer
	// Subscribes reports whether the named server advertised resource
	// subscriptions at startup: a feed over one of the server's declared
	// resources needs the capability to subscribe to it.
	Subscribes(server string) bool
	// ResourceFeed returns the runner of one resource the named server
	// holds: the feed drains the resource's reads through the filters the
	// feed's manifest declares.
	ResourceFeed(server, uri string) poo.FeedRunner
	// Offered returns the tools the servers listed at startup, as the
	// entries the model reaches through the MCP call: one per tool,
	// qualified with the server's name, naming the call that runs it and
	// the argument that names it. It is the entries of the views Servers
	// returns, derived the way the dispatch's own tool derives them, so the
	// session's search and its prompt list the same value the call
	// dispatches. The build reads them once, at attach.
	Offered() []cli.Offered
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
	// Registry is the cli registry the settings name, or nil when they name none.
	// Availability narrows it to the entries the profile may call. A profile that
	// earns no catalog leaves it as the manifests loaded it, and no tool or
	// prompt section reads it.
	Registry *cli.Catalog
	// Skills are the skills the session offers, or nil when it offers
	// none.
	Skills *skill.Set
	// loaded is the registry as the manifests loaded it, before availability
	// narrows what a profile offers. The profiles the binary lists are judged
	// against it, each under its own profile fact.
	loaded *cli.Catalog
	// registryOffered reports whether the session offers tools over the
	// catalog. A binary may link them, and a profile may name them, without the
	// session offering any.
	registryOffered bool
	// elevatedTools are the catalog's entries the session offers as tools of
	// their own, in name order. It is empty when the session offers the call
	// tool over the catalog instead, and the prompt carries the catalog's
	// section beside it.
	elevatedTools []poo.ToolInfo
	// registryFromDefaults and feedsFromDefaults report that the only
	// manifest paths the build loaded came from the default directories.
	// An error that would say "the settings" names the real source.
	registryFromDefaults bool
	feedsFromDefaults    bool
	// chatModelResolved reports whether the build resolved a chat
	// model: a build that runs without one says so when a profile's
	// opt-in cannot render, instead of naming a model that is not
	// there.
	chatModelResolved bool
	// Feeds are the feed manifests the settings name, or nil when the
	// settings name none.
	Feeds *feed.Set
	// MCPServers are the MCP servers the chosen profile selects, in
	// selection order. Nothing starts without one: the manifests load
	// the declarations, and the selection is what a session starts.
	MCPServers []string
	// mcpSet are the MCP servers the manifests declare, or nil when the
	// settings name no manifest. The selection reads it, and a session
	// starts the servers its profile selects from it.
	mcpSet *manifest.Set
	// mcpLauncher starts one selected server for a live session. It is
	// the binary's link to the client, and nil when the binary links
	// none: a profile that selects a server on such a binary fails the
	// build rather than a session that cannot start what it selected.
	mcpLauncher MCPLauncher
	// toolFactories are the tools the binary links, and toolOptions the
	// settings' own options for them. The attach builds the tools over the
	// servers a session started a second time, from the same two values, so
	// the offering a session reaches is the one it launched and never a
	// second wiring.
	toolFactories map[string]ToolFactory
	toolOptions   ToolOptions
	// envScrub is the rule the MCP servers a session starts are launched
	// with: the tools' scrub, and the names a launcher declared, which a
	// server reaches only through an explicit manifest reference.
	envScrub exec.Scrub
	// executor is the executor the commands run through: the tools
	// that run one, and the feeds. It is nil when nothing runs a
	// command.
	executor exec.Executor
	// deciderModel is the model the session's decider asks, when there
	// is one.
	deciderModel poo.ModelInfo
	// decider is the decider the session and the tools over the
	// registry share. It is nil when the settings name no decider.
	decider decide.Decider
	// deciderErr is why the session has no decider.
	deciderErr error
	// Policy is the permission policy.
	Policy *perm.Policy
	// Askers are the asker chain, in order.
	Askers []perm.Asker
	// Profiles are the profiles the binary offers.
	Profiles           []poo.Profile
	profileDefinitions []profile.Profile
	resolvedProfiles   map[string]resolvedProfile
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
	// Build names the binary that owns the sessions.
	Build poo.BuildInfo
	// Env is the base a tool's child environment is built from, not that
	// environment: each tool scrubs it with the rules it was built with, so a
	// launcher's credential can stay here for the feed and a cli entry's
	// bindings while a model-invoked child never sees it.
	Env []string
	// DeclaredCredentialEnvNames are the environment names a launcher declared
	// for poo's own subsystems. They are the opposite of the endpoint
	// credentials above: those are removed from Env itself, while these stay
	// in Env so the feed and a cli entry's bindings can resolve them, and are
	// removed only from the environment a model-invoked tool receives.
	DeclaredCredentialEnvNames []string
	// ProtectedPaths are the paths the tools must not touch.
	ProtectedPaths []string
	// toolNames are the names of the tools the session offers, in
	// offer order.
	toolNames []string
	// httpClient is the client the transports and the probes use.
	httpClient *http.Client
	// projectPromptField names what asked for the working directory's
	// project instructions, and is empty when nothing did.
	projectPromptField string
}

// WithOverrides returns a copy of the settings with the command line's
// changes applied.
//
// The copy is the effective configuration a command runs with: the
// profile, the permission actions, the session directory and mode, and
// the prompt changes the flags name. The manifest paths are the
// settings' paths followed by the overrides', in fresh lists. The other
// maps and lists are shared, and only the fields an override names are
// replaced.
func WithOverrides(settings *Settings, overrides Overrides) *Settings {
	if settings == nil {
		return nil
	}
	effective := *settings
	if overrides.Profile != "" {
		effective.Profile = overrides.Profile
	}
	if overrides.PermissionUnmatched != "" {
		value := overrides.PermissionUnmatched
		effective.Permission.Unmatched = &value
	}
	if overrides.PermissionFallback != "" {
		value := overrides.PermissionFallback
		effective.Permission.Fallback = &value
	}
	if overrides.SessionDir != "" {
		value := overrides.SessionDir
		effective.Session.Dir = &value
	}
	if overrides.SessionMode != "" {
		value := overrides.SessionMode
		effective.Session.Mode = &value
	}
	if overrides.UserPromptFile != "" {
		value := overrides.UserPromptFile
		effective.Session.UserPromptFile = &value
	}
	if overrides.SystemPrompt != "" {
		effective.SystemPrompt.BaseFile = overrides.SystemPrompt
	}
	if overrides.AppendSystemPrompt != "" {
		effective.SystemPrompt.Append = overrides.AppendSystemPrompt
	}
	if len(overrides.FeedsPaths) > 0 {
		effective.Feeds.Paths = append(append([]string(nil), settings.Feeds.Paths...), overrides.FeedsPaths...)
	}
	if len(overrides.MCPPaths) > 0 {
		effective.MCP.Paths = append(append([]string(nil), settings.MCP.Paths...), overrides.MCPPaths...)
	}
	if len(overrides.CLIRegistryPaths) > 0 {
		effective.CLIRegistry.Paths = append(append([]string(nil), settings.CLIRegistry.Paths...), overrides.CLIRegistryPaths...)
	}
	return &effective
}

// ToolFactory builds a tool from the settings.
//
// The factory is the binary's: the binary knows what a tool is, and
// the settings know how it is set up. A tool that needs nothing from
// the settings ignores the options.
type ToolFactory func(ToolOptions) poo.Tool

// ToolOptions are the settings' options for the tools.
type ToolOptions struct {
	// Executor is the executor the tools that run a command use:
	// bash, and the registered tools.
	Executor exec.Executor
	// Scrub is the rules for the child environment.
	Scrub exec.Scrub
	// ProtectedPaths are the paths the tools must not touch.
	ProtectedPaths *protect.Set
	// Registry is the cli registry the registry's tools call. It is nil
	// when the settings name no registry, and when the session's profile
	// earns none of the catalog the manifests loaded.
	Registry *cli.Catalog
	// Search reports that the session searches the catalog instead of
	// listing it, so call_tool points an unknown name at the search.
	Search bool
	// Inputs are the modalities the session's model reads, so a tool that can
	// return one does, rather than refusing what the model cannot see.
	Inputs []poo.Modality
	// CodemodeEngine is the engine selected for the codemode tool.
	CodemodeEngine string
	// ToolNames are the names of the tools the session offers. A tool
	// whose refusal points at another tool offers that tool only when
	// this list holds its name.
	ToolNames []string
	// Offered reads the entries the session's models reach through a tool
	// that calls them, as they are now: the catalog's registered commands,
	// which the call tool runs, and the MCP tools the session's servers
	// listed, which the MCP call runs. One read is one snapshot, so a
	// search hit, a refusal's naming and the prompt's catalog line read
	// the same dispatcher and the same argument — and a server that
	// relaunches is read on the generation the session runs, for the
	// handles the offering closes over read the group's current one.
	// A build's offering holds no MCP entry, because a build starts no
	// server; the attach the build set rebuilds the tools over the
	// servers with the session's own.
	Offered cli.Offerings
	// MCP are the MCP servers a live session started, in selection order:
	// the handles the binary's own launcher returned, which the binary's own
	// tools over them read. The settings pass them through and never read
	// them, because the settings hold no MCP client; a build has none, since
	// a build starts no server. The attach the build set replaces the tools
	// over the servers with per-session instances, so a tool built here
	// describes the offering and the session's own describes what it can
	// reach.
	MCP []MCPRunning
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
	// The build's own name is what the runtime's requests call themselves,
	// and the catalog is the first thing built: the probes it carries name
	// the client from the first one, not from the transports alone.
	runtime.Build = opts.Build
	runtime.mcpLauncher = opts.MCPLauncher

	definitions, resolvedProfiles, err := runtime.resolveProfileDefinitions(opts)
	if err != nil {
		return nil, err
	}
	runtime.profileDefinitions = definitions
	runtime.resolvedProfiles = resolvedProfiles

	if err := runtime.resolveRegistry(opts, workingDir); err != nil {
		return nil, err
	}
	if err := runtime.resolveSkills(opts, workingDir); err != nil {
		return nil, err
	}
	if err := runtime.resolveFeeds(opts, workingDir); err != nil {
		return nil, err
	}
	if err := runtime.resolveMCP(opts, workingDir); err != nil {
		return nil, err
	}
	if err := runtime.resolveModels(opts); err != nil {
		return nil, err
	}
	runtime.resolveDecider()
	if err := runtime.resolveProfile(opts, workingDir); err != nil {
		return nil, err
	}
	if err := runtime.resolveTools(opts, workingDir); err != nil {
		return nil, err
	}
	if err := runtime.resolveMCPLauncher(opts); err != nil {
		return nil, err
	}
	if err := runtime.resolveFeedRuns(opts); err != nil {
		return nil, err
	}
	if err := runtime.resolvePolicy(opts, workingDir); err != nil {
		return nil, err
	}
	if err := runtime.resolveSession(opts, workingDir); err != nil {
		return nil, err
	}
	if err := runtime.resolvePrompt(opts, workingDir); err != nil {
		return nil, err
	}
	// The prompt renders once here, so a loaded file that cannot render on
	// this build's own model, tools, directory and settings fails the
	// build rather than a session.
	if err := poo.CheckPrompt(runtime.Options); err != nil {
		return nil, PromptRenderError(err)
	}
	return runtime, nil
}

// PromptRenderError returns the build failure for a prompt that did not
// render.
//
// The prompt's prose is what is wrong, so the failure names the setting
// the prose came through, and the error underneath names the file.
func PromptRenderError(err error) *Error {
	return &Error{
		Code:  CodeInvalid,
		Field: "system_prompt",
		Err:   err,
	}
}

// resolveProfile picks the profile the session is built from.
//
// The command line names the profile, and the settings do, and the
// binary has a default. The profile names the tools the session
// offers, and every tool it names must be one the binary links.
func (r *Runtime) resolveProfile(opts BuildOptions, workingDir string) error {
	// The manifest resolvers read the same profile the session runs
	// under: the name comes from the one resolution they share.
	name := r.profileName(opts)
	if name == "" {
		return &Error{Code: CodeUnsupported, Message: "the build has no profile to run under"}
	}
	definitions := r.profileDefinitions
	chosen, err := profile.Find(definitions, name)
	if err != nil {
		return &Error{
			Code:    CodeUnsupported,
			Field:   "profile",
			Message: err.Error(),
			Err:     err,
		}
	}
	r.Profile = poo.Profile{Name: chosen.Name, Unmatched: chosen.Unmatched, Feeds: slices.Clone(chosen.Feeds)}
	// The declared feeds are a presence assertion, not a switch: the
	// operator's feeds.paths still control which feeds run. A
	// declared feed the settings do not load is skipped with a
	// warning, never a failure. A selected MCP server's resource feed is
	// a feed the session runs, so its name answers the assertion too.
	for _, feedName := range chosen.Feeds {
		if !r.hasFeed(feedName) {
			r.warn(poo.MissingFeedWarning(chosen.Name, feedName))
		}
	}
	// The profile's own prompt section is the profile's, so it goes in
	// before the binary's directory and the settings: a later layer
	// still wins over it.
	if chosen.Prompt != "" {
		text := chosen.Prompt
		r.Prompt = append(r.Prompt, func(b *prompt.Builder) {
			b.Set(prompt.ProfileSection, text)
		})
	}
	for _, tool := range chosen.Tools {
		if !linksTool(opts, tool) {
			return &Error{
				Code:    CodeUnsupported,
				Field:   "profile",
				Message: fmt.Sprintf("the profile %q offers the tool %q, and this binary does not link it", chosen.Name, tool),
			}
		}
		r.Profile.Tools = append(r.Profile.Tools, toolInfo(opts.Tools[tool]))
		r.toolNames = append(r.toolNames, tool)
	}
	// The disabled list is the operator's, so it comes off the profile
	// before the registry joins: a profile whose only side-effecting tool
	// is disabled earns no registry tool, and a disabled registry tool
	// does not come back.
	if err := r.disableTools(opts); err != nil {
		return err
	}
	// The profile's named tools are the operator's choice as well, and the
	// listing does not offer a profile the settings empty, so selecting
	// one cannot run.
	if len(chosen.Tools) > 0 && len(r.Profile.Tools) == 0 {
		return &Error{
			Code:    CodeUnsupported,
			Field:   "tools.disabled",
			Message: fmt.Sprintf("the settings disable every tool the profile %q offers", chosen.Name),
		}
	}
	// The registry is the settings' opt-in: a session whose settings name
	// a registry offers its tools as well.
	if err := r.addRegistryTools(opts); err != nil {
		return err
	}
	// The MCP servers a profile selects are the session's own processes, and
	// the model reaches their tools through one call beside the registry's.
	if err := r.addMCPTools(opts); err != nil {
		return err
	}
	// The core adds the job tools on its own whenever a linked tool may
	// run in the background or the session runs feeds, and the profile
	// offers them with it; the tool that starts an optional feed comes
	// with them.
	r.Profile.Tools = withJobTools(r.Profile.Tools, r.hasFeeds())
	r.Profile.Tools = withSubscribeTool(r.Profile.Tools, r.hasOptionalFeeds())
	profiles, err := r.resolveProfiles(opts, definitions, workingDir)
	if err != nil {
		return err
	}
	r.Profiles = profiles
	return nil
}

// sessionFeeds returns the feeds the session runs: the feed manifests'
// feeds, and the resource feeds the selected MCP servers declare, for a
// resource feed is a feed the session runs. Each entry is the feed's
// name and whether the model may start it: a manifest's feed by its own
// word, and a resource feed never, for the session starts it beside
// itself and the subscribe tool is not the model's to call for one.
func (r *Runtime) sessionFeeds() map[string]bool {
	feeds := map[string]bool{}
	for _, spec := range r.Feeds.Specs() {
		feeds[spec.Name] = spec.Optional
	}
	for _, server := range r.MCPServers {
		for _, decl := range r.mcpSet.Feeds(server) {
			feeds[decl.Name] = false
		}
	}
	return feeds
}

// hasFeed reports whether the session runs a feed by the name.
func (r *Runtime) hasFeed(name string) bool {
	_, ok := r.sessionFeeds()[name]
	return ok
}

// hasFeeds reports whether the session runs feeds.
func (r *Runtime) hasFeeds() bool {
	return len(r.sessionFeeds()) > 0
}

// hasOptionalFeeds reports whether the session loads a feed the model may
// start.
func (r *Runtime) hasOptionalFeeds() bool {
	for _, optional := range r.sessionFeeds() {
		if optional {
			return true
		}
	}
	return false
}

// resolveSkills loads the skills the settings name.
//
// The directories are the person's own — the default one, or the one
// they name — and then the paths beside them, resolved by againstDir. A
// directory that is not there is dropped with a
// warning rather than failing the build: skills are guidance a session
// offers, and a tree without the directory is not a tree a session
// cannot work in. A SKILL.md that is there and does not parse is an
// error, because the directory was offered on purpose.
func (r *Runtime) resolveSkills(opts BuildOptions, workingDir string) error {
	settings := r.Settings.Skills
	dirs := make([]string, 0, len(settings.Paths)+1)
	switch {
	case settings.UserPath != nil && *settings.UserPath != "":
		path, err := r.skillDir("skills.user_path", *settings.UserPath, false)
		if err != nil {
			return err
		}
		if path != "" {
			dirs = append(dirs, path)
		}
	case settings.Default != nil && *settings.Default:
		// The default is a convention, not a promise: a person who has no
		// skills directory is not told about it every session.
		path, err := r.skillDir("skills.default", skill.DefaultDir, true)
		if err != nil {
			return err
		}
		if path != "" {
			dirs = append(dirs, path)
		}
	}
	paths := append(append([]string{}, settings.Paths...), opts.Overrides.SkillsPaths...)
	resolved, err := againstDir(paths, workingDir)
	if err != nil {
		return &Error{
			Code:    CodeInvalid,
			Field:   "skills.paths",
			Message: "the skills paths cannot be resolved",
			Err:     err,
		}
	}
	for _, path := range resolved {
		// A directory that is not there, and a file named where a
		// directory belongs, are both dropped with a warning: skills are
		// guidance a session offers, and a path that is not a directory
		// of them leaves it with less to offer rather than broken.
		info, err := os.Stat(path)
		if err != nil {
			r.warn(fmt.Sprintf("skills.paths: %s holds no skills: %v", path, err))
			continue
		}
		if !info.IsDir() {
			r.warn(fmt.Sprintf("skills.paths: %s is not a directory", path))
			continue
		}
		dirs = append(dirs, path)
	}
	if len(dirs) == 0 {
		return nil
	}
	set, err := skill.Load(dirs...)
	if err != nil {
		return &Error{
			Code:    CodeInvalid,
			Field:   "skills",
			Message: "the skills do not load",
			Err:     err,
		}
	}
	// The read tool reaches the directories that loaded, whatever the
	// catalog shows: a skill the model may not invoke is still one the
	// person may ask it to follow.
	r.Options.ReadRoots = append(r.Options.ReadRoots, set.Dirs()...)
	if set.Len() == 0 {
		return nil
	}
	r.Skills = set
	r.Prompt = append(r.Prompt, func(b *prompt.Builder) {
		b.SetTemplate(skill.SectionName, set.Template())
	})
	return nil
}

// skillDir resolves one skills directory the settings named, and reports
// whether it holds anything to read.
//
// A named directory that is not there is dropped rather than failing the
// build, the rule the paths follow. quiet keeps a convention's absence
// out of the warnings: the default directory is not a promise.
func (r *Runtime) skillDir(field, named string, quiet bool) (string, error) {
	path, err := expandHome(named)
	if err != nil {
		return "", &Error{
			Code:    CodeInvalid,
			Field:   field,
			Message: "the skills directory cannot be read",
			Err:     err,
		}
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		if !quiet {
			r.warn(fmt.Sprintf("%s: %s holds no skills", field, path))
		}
		return "", nil
	}
	return path, nil
}

// warn adds a warning to the runtime's own, so a command prints it with
// the rest.
func (r *Runtime) warn(text string) {
	if text == "" {
		return
	}
	if r.Warning == "" {
		r.Warning = text
		return
	}
	r.Warning += "; " + text
}

// defaultsOn decides whether the default manifest directories load. The
// settings' key speaks first: absent or true means they load. A profile
// that names the toggle speaks last, and its word wins.
func defaultsOn(settingsDefault, profileDefault *bool) bool {
	if profileDefault != nil {
		return *profileDefault
	}
	return settingsDefault == nil || *settingsDefault
}

// manifestWords holds the words the session's own profile resolves for
// the manifest defaults.
type manifestWords struct {
	feeds    bool
	mcp      bool
	registry bool
}

// chosenWords resolves the manifest defaults words the session's own
// profile runs under: the settings' key speaks first, the chosen
// profile's word wins. resolveRegistry, resolveFeeds, resolveMCP and the
// listing's cache seed read this one resolution, so what the session
// loaded and what the seed keys agree by construction.
func (r *Runtime) chosenWords(opts BuildOptions) manifestWords {
	chosen := r.chosenProfile(opts)
	return manifestWords{
		feeds:    defaultsOn(r.Settings.Feeds.Defaults, chosen.FeedsDefaults),
		mcp:      defaultsOn(r.Settings.MCP.Defaults, chosen.mcpDefaults),
		registry: defaultsOn(r.Settings.CLIRegistry.Defaults, chosen.RegistryDefaults),
	}
}

// profileName returns the name of the profile the session runs under:
// the command line's, the settings', the binary's default, or the first
// linked profile's. The resolution is resolveProfile's own, so the
// manifest resolvers read the same profile the session will run under.
func (r *Runtime) profileName(opts BuildOptions) string {
	if opts.Overrides.Profile != "" {
		return opts.Overrides.Profile
	}
	if r.Settings.Profile != "" {
		return r.Settings.Profile
	}
	if opts.DefaultProfile != "" {
		return opts.DefaultProfile
	}
	if len(opts.Profiles) > 0 {
		return opts.Profiles[0].Name
	}
	return ""
}

// chosenProfile returns the resolved settings of the profile the
// session runs under, or the zero value when the name is a linked
// profile alone.
func (r *Runtime) chosenProfile(opts BuildOptions) resolvedProfile {
	return r.resolvedProfiles[r.profileName(opts)]
}

// manifestPaths joins the paths one manifest kind loads with the kind's
// default directory, when the kind's word turns the defaults on. It
// reports whether the default directory was the only source: the empty
// set it produces is then nothing at all, and the errors name it rather
// than the settings.
func manifestPaths(paths []string, settingsKey string, on bool) ([]string, bool) {
	out := append([]string(nil), paths...)
	fromDefaultsOnly := false
	if on {
		if dir, ok := defaultManifestDir(settingsKey); ok {
			out = append([]string{dir}, out...)
			fromDefaultsOnly = len(paths) == 0
		}
	}
	return out, fromDefaultsOnly
}

// loadFeeds loads the feed manifests the kind's word on the defaults
// selects. No paths is no set: the resolver returns nil, and the session
// runs none. It reports whether the default directory was the only
// source.
func (r *Runtime) loadFeeds(opts BuildOptions, workingDir string, on bool) (*feed.Set, bool, error) {
	paths, fromDefaultsOnly := manifestPaths(
		append(append([]string{}, r.Settings.Feeds.Paths...), opts.Overrides.FeedsPaths...), "feeds", on)
	if len(paths) == 0 {
		return nil, fromDefaultsOnly, nil
	}
	resolved, err := againstDir(paths, workingDir)
	if err != nil {
		return nil, fromDefaultsOnly, &Error{
			Code:    CodeInvalid,
			Field:   "feeds.paths",
			Message: "the feeds paths cannot be resolved",
			Err:     err,
		}
	}
	set, err := feed.Load(resolved...)
	if err != nil {
		return nil, fromDefaultsOnly, &Error{
			Code:    CodeInvalid,
			Field:   "feeds.paths",
			Message: "the feeds do not load",
			Err:     err,
		}
	}
	return set, fromDefaultsOnly, nil
}

// loadMCP loads the mcp.json manifests the kind's word on the defaults
// selects. No paths is no set: the resolver returns nil, and no server can be
// selected. It reports whether the default directory was the only source.
func (r *Runtime) loadMCP(opts BuildOptions, workingDir string, on bool) (*manifest.Set, bool, error) {
	// The one resolution the session and the mcp commands share; the
	// word is the caller's own, the one chosenWords resolved.
	paths, fromDefaultsOnly, err := MCPManifestPaths(r.Settings, workingDir, opts.Overrides.MCPPaths, on)
	if err != nil {
		return nil, false, &Error{
			Code:    CodeInvalid,
			Field:   "mcp.paths",
			Message: "the mcp paths cannot be resolved",
			Err:     err,
		}
	}
	set, err := manifest.Load(paths...)
	if err != nil {
		return nil, fromDefaultsOnly, &Error{
			Code:    CodeInvalid,
			Field:   "mcp.paths",
			Message: "the mcp manifests do not load",
			Err:     err,
		}
	}
	return set, fromDefaultsOnly, nil
}

// loadRegistry loads the cli catalog the kind's word on the defaults
// selects, with the checks the session's own load carries: a session
// reference the session scope does not carry fails the build, and the
// conditions the manifests declare must be ones poo can supply.
func (r *Runtime) loadRegistry(opts BuildOptions, workingDir string, on bool) (*cli.Catalog, bool, error) {
	paths, fromDefaultsOnly := manifestPaths(
		append(append([]string{}, r.Settings.CLIRegistry.Paths...), opts.Overrides.CLIRegistryPaths...), "cli_registry", on)
	if len(paths) == 0 {
		return nil, fromDefaultsOnly, nil
	}
	resolved, err := againstDir(paths, workingDir)
	if err != nil {
		return nil, fromDefaultsOnly, &Error{
			Code:    CodeInvalid,
			Field:   "cli_registry.paths",
			Message: "the cli registry paths cannot be resolved",
			Err:     err,
		}
	}
	catalog, err := cli.Load(resolved...)
	if err != nil {
		return nil, fromDefaultsOnly, &Error{
			Code:    CodeInvalid,
			Field:   "cli_registry.paths",
			Message: "the cli registry does not load",
			Err:     err,
		}
	}
	// A session reference the session scope does not carry fails the
	// build, the way a feed's does: the error would otherwise reach the
	// model one call at a time instead of the operator once, at the
	// manifest.
	for _, entry := range catalog.Entries() {
		bindings, _ := catalog.EnvBindings(entry.Name)
		for _, binding := range bindings {
			if !binding.Reference || binding.Scope == "" {
				continue
			}
			if err := exec.ValidSessionReference(binding.Scope, binding.From); err != nil {
				return nil, fromDefaultsOnly, &Error{
					Code:    CodeInvalid,
					Field:   "cli_registry.paths",
					Message: fmt.Sprintf("the tool %q declares an unknown session reference", entry.Name),
					Err:     err,
				}
			}
		}
	}
	if err := checkRegistryConditions(catalog.AvailabilityInputs()); err != nil {
		return nil, fromDefaultsOnly, err
	}
	return catalog, fromDefaultsOnly, nil
}

// resolveRegistry loads the cli registries the kind's word on the
// defaults selects. The catalog is loaded here,
// once: the commands a session may call are the ones its registry holds
// when the session is built, and a session does not reload one.
func (r *Runtime) resolveRegistry(opts BuildOptions, workingDir string) error {
	catalog, fromDefaultsOnly, err := r.loadRegistry(opts, workingDir, r.chosenWords(opts).registry)
	if err != nil {
		return err
	}
	// The default directories are the build's own suggestion, so an
	// empty one is no registry at all: a person who created the
	// directory but holds no manifests left the build exactly where it
	// was. Paths the settings or the overrides named keep the behavior
	// those paths have always had.
	if catalog != nil && catalog.Len() == 0 && fromDefaultsOnly {
		catalog = nil
	}
	r.registryFromDefaults = catalog != nil && fromDefaultsOnly
	r.Registry = catalog
	r.loaded = catalog
	return nil
}

// DefaultManifest describes one manifest kind that loads from a default
// directory.
type DefaultManifest struct {
	// Kind is the default directory's name under poo's config directory.
	Kind string
	// SettingsKey is the settings block whose defaults switch turns the
	// kind off.
	SettingsKey string
}

// defaultManifests are the manifest kinds that load from the default
// directories. The list is the kinds' single owner: the resolvers read
// their kinds here, and anything that turns the defaults off turns off
// everything the list names, so a kind added here is isolated with the
// others or loaded nowhere.
var defaultManifests = []DefaultManifest{
	{Kind: "feeds", SettingsKey: "feeds"},
	{Kind: "mcp", SettingsKey: "mcp"},
	{Kind: "cli-registry", SettingsKey: "cli_registry"},
}

// DefaultManifests returns the manifest kinds that load from the default
// directories, and the settings block each kind turns off. A suite that
// writes settings turns off everything this list names.
func DefaultManifests() []DefaultManifest {
	out := make([]DefaultManifest, len(defaultManifests))
	copy(out, defaultManifests)
	return out
}

// defaultManifestDir returns poo's default directory for the manifest
// kind whose settings block is the named key, when the directory is
// there. A missing directory is not an error: a person with no defaults
// sees no change, the way the skills' default directory is a convention
// and not a promise.
func defaultManifestDir(settingsKey string) (string, bool) {
	kind := ""
	for _, m := range defaultManifests {
		if m.SettingsKey == settingsKey {
			kind = m.Kind
			break
		}
	}
	if kind == "" {
		return "", false
	}
	if dir, err := UserConfigDir(); err == nil {
		path := filepath.Join(dir, kind)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path, true
		}
	}
	// The macOS fallback the settings and credentials read: a person
	// whose poo lives under ~/.config keeps their manifests beside
	// them, or the feature is dead for them with nothing said.
	if fallback, ok := userConfigFallbackDir(); ok {
		path := filepath.Join(fallback, kind)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path, true
		}
	}
	return "", false
}

// againstDir resolves each path. ~ and a path that starts with ~/ are
// under the home directory. Any other relative path, ~user included, is
// under dir. An absolute path is kept.
func againstDir(paths []string, dir string) ([]string, error) {
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		path, err := resolvePath(path, dir, false)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, path)
	}
	return resolved, nil
}

// resolveFeeds loads the feed manifests the settings and overrides name.
//
// The paths are resolved by againstDir, and every manifest reachable
// from them joins one set. The set is loaded
// once, when the session is built; a session never reloads it. Nothing
// starts a feed here.
func (r *Runtime) resolveFeeds(opts BuildOptions, workingDir string) error {
	set, fromDefaultsOnly, err := r.loadFeeds(opts, workingDir, r.chosenWords(opts).feeds)
	if err != nil {
		return err
	}
	// An empty set the defaults alone produced is no feed set at all:
	// the session runs nothing, and the build says nothing.
	if set != nil && set.Len() == 0 && fromDefaultsOnly {
		set = nil
	}
	r.feedsFromDefaults = set != nil && fromDefaultsOnly
	r.Feeds = set
	return nil
}

// resolveMCP loads the mcp.json manifests the kind's word on the defaults
// selects, and takes the servers the chosen profile selects from them.
//
// The paths are resolved by againstDir, the warnings the load produced are the
// session's, and nothing starts: the manifests declare the servers a session
// may start, and the profile's selection is what starts one.
func (r *Runtime) resolveMCP(opts BuildOptions, workingDir string) error {
	set, fromDefaultsOnly, err := r.loadMCP(opts, workingDir, r.chosenWords(opts).mcp)
	if err != nil {
		return err
	}
	for _, warning := range set.Warnings() {
		r.warn(warning)
	}
	// An empty set the defaults alone produced is no server set at all: a
	// person who created the directory and holds no manifest left the build
	// where it was. Its warnings are already the session's, so the set itself
	// says nothing more.
	if set != nil && len(set.Names()) == 0 && fromDefaultsOnly {
		set = nil
	}
	r.mcpSet = set
	profile := r.profileName(opts)
	r.MCPServers = append([]string(nil), r.chosenProfile(opts).mcpServers...)
	for _, server := range r.MCPServers {
		if _, ok := set.Lookup(server); ok {
			continue
		}
		r.warn(missingMCPServerWarning(profile, server))
	}
	// The feed names are the settings' own authoring, and their
	// failure is settled before the binary's capability is asked
	// about: a manifest that cannot run says so whatever the binary
	// links.
	if err := r.checkMCPFeedNames(); err != nil {
		return err
	}
	return nil
}

// resolveMCPLauncher holds the launcher requirement a selection
// carries: starting the servers a live session starts is the
// launcher's job, and the requirement is the build's, not the
// session's — every entry point of the poo binary links the launcher,
// so a profile that selects a server builds everywhere the binary
// runs, and an inspection command that never builds a session still
// passes because it builds the same runtime.
//
// It runs after the tools resolve, so a binary that fails on both
// counts names the executor it lacks before the launcher: the
// executor is what the session's own commands need, and the feed a
// selected server declares reaches it first.
func (r *Runtime) resolveMCPLauncher(opts BuildOptions) error {
	if len(r.MCPServers) == 0 {
		return nil
	}
	if opts.MCPLauncher == nil {
		return &Error{
			Code:  CodeUnsupported,
			Field: "mcp.paths",
			Message: fmt.Sprintf("the profile %q selects the mcp servers [%s], and this binary links no launcher to start them",
				r.profileName(opts), strings.Join(r.MCPServers, ", ")),
		}
	}
	// The attach is not set on the runtime's own options: the caller's
	// options apply after the base, so a caller's WithSessionAttach
	// would silently replace the attach there and the profile's
	// selected servers would never start. The session-building entry
	// points append MCPAttachOption after the caller's options — the
	// way the policy is appended — so the build's attach composes with
	// the caller's instead of losing to it.
	return nil
}

// mcpAttach starts the servers the chosen profile selected for one
// live session, binds the tools over them to what this session started,
// and returns what that session runs when it ends.
//
// The attach runs inside the session's construction, under the
// session's own lifetime context, after the id and the working
// directory are final. A selected name the settings could not load is
// a warning the build already said, so the session runs without it.
// The loadable selection goes to the launcher as one launch — the
// launcher's transaction, whose rollback is the client's own: a server
// that fails to launch fails the session, and nothing the launch
// started survives it. The failure names the server that did not
// start.
func (r *Runtime) mcpAttach(ctx context.Context, a *poo.Attach) (func() error, error) {
	var servers []manifest.Server
	for _, name := range r.MCPServers {
		server, ok := r.mcpSet.Lookup(name)
		if !ok {
			continue
		}
		servers = append(servers, server)
	}
	if len(servers) == 0 {
		return nil, nil
	}
	running, err := r.mcpLauncher(ctx, MCPLaunchSelection{
		SessionID:  a.SessionID,
		WorkingDir: a.WorkingDir,
		Scrub:      r.envScrub,
		Servers:    servers,
	})
	if err != nil {
		// The launcher's failure names the server that did not start;
		// the field is the build's.
		return nil, &Error{
			Code:    CodeInvalid,
			Field:   "mcp.paths",
			Message: err.Error(),
			Err:     err,
		}
	}
	// A declared feed is a feed the session runs: its spec joins the
	// session's own, with the started server's subscription behind it
	// and the manifest's filter screening what it emits. A server that
	// declared a feed but advertised no resource subscriptions cannot
	// run it, and the session is not built: whatever started stops
	// first, the way a launch that failed does.
	feeds, err := r.resourceFeedSpecs(servers, running)
	if err != nil {
		_ = running.Close()
		return nil, err
	}
	if len(feeds) > 0 {
		// The options are the session's own copy, but the slice they hold
		// is the build's: the appended feeds go into a clone, so one
		// session's feeds never reach the next.
		a.Options.Feeds = append(slices.Clone(a.Options.Feeds), feeds...)
	}
	// The tools over the servers read what this session started, so they are
	// built again now: the build's instances describe the offering, and these
	// describe what this session can reach. The offering is the session's
	// own, read as it is now, so a search hit after a relaunch names a tool
	// of the generation the session runs; the call's own catalog lists the
	// entries its construction read, for the prompt is rendered once and
	// not again. The search is bound with the call, so a session without
	// the call returns no MCP entry at all — a search that named a tool the
	// model cannot call would be worse than no hit.
	if _, offered := a.Options.Tools[ToolCallMCP]; offered {
		sessionOptions := r.toolOptions
		sessionOptions.MCP = []MCPRunning{running}
		sessionOptions.Offered = offeredView(r.offeredCatalog(), r.elevatedTools, []MCPRunning{running}, r.offersSearch())
		for _, name := range []string{ToolCallMCP, cli.FindToolName} {
			factory := r.toolFactories[name]
			if factory == nil {
				continue
			}
			if _, ok := a.Options.Tools[name]; !ok {
				continue
			}
			a.Options.Tools[name] = factory(sessionOptions)
		}
	}
	return running.Close, nil
}

// MCPAttachOption returns the option that starts the servers the
// chosen profile selected for a live session.
//
// The option is appended after the caller's options, so it composes
// with any attach the caller named: the build's attach runs first, the
// caller's second, and the returned close stops them in the reverse
// order with their errors joined. When the caller's attach fails after
// the build's started, the failure is the caller's own and the close
// the build's attach handed back runs, so nothing it started survives
// the failed construction.
func (r *Runtime) MCPAttachOption() poo.Option {
	mcp := r.mcpAttach
	return func(o *poo.Options) {
		caller := o.SessionAttach
		if caller == nil {
			o.SessionAttach = mcp
			return
		}
		o.SessionAttach = func(ctx context.Context, a *poo.Attach) (func() error, error) {
			mcpClose, err := mcp(ctx, a)
			if err != nil {
				return nil, err
			}
			callerClose, err := caller(ctx, a)
			if err != nil {
				if mcpClose != nil {
					_ = mcpClose()
				}
				return nil, err
			}
			switch {
			case mcpClose == nil:
				return callerClose, nil
			case callerClose == nil:
				return mcpClose, nil
			}
			return func() error {
				return errors.Join(callerClose(), mcpClose())
			}, nil
		}
	}
}

// resourceFeedSpecs builds the feed specs the selected servers'
// declared resource feeds become.
//
// The runner is the started server's own, and the wrapper around its
// emits is the filter stage every feed's records pass: the declared
// filter shapes and fans what the resource read returned, and it runs
// the way a command feed's filter does — the same executor and
// directory — with the environment a command feed that declares no env
// gets, for the synthesized spec carries none. A server that declared
// a feed but advertised no resource subscriptions can never grant the
// subscription the feed runs on, and the failure names the server and
// the feed the way a server that did not start does.
func (r *Runtime) resourceFeedSpecs(servers []manifest.Server, running MCPRunning) ([]poo.FeedSpec, error) {
	var specs []poo.FeedSpec
	if r.mcpSet == nil {
		return nil, nil
	}
	for _, server := range servers {
		name := server.Name
		for _, decl := range r.mcpSet.Feeds(name) {
			if !running.Subscribes(name) {
				return nil, &Error{
					Code:    CodeInvalid,
					Field:   "mcp.paths",
					Message: fmt.Sprintf("the mcp server %q advertises no resource subscriptions, and the resource feed %q needs one", name, decl.Name),
				}
			}
			onIdle, err := feedOnIdle(decl.OnIdle, "mcp.paths", fmt.Sprintf("the resource feed %q of the mcp server %q does not say when it may wake a run", decl.Name, name))
			if err != nil {
				return nil, err
			}
			run := running.ResourceFeed(name, decl.Resource)
			specs = append(specs, poo.FeedSpec{
				Name:        decl.Name,
				Description: decl.Description,
				// A resource feed's source is the session's own server: an
				// attempt that ends — a failed read, a refused emit — is
				// one the session brings back, and the restart policy is
				// how it does.
				Restart: true,
				OnIdle:  onIdle,
				// A feed's filter is one of the two kinds, and the core
				// refuses a filter prompt on a feed whose manifest runs the
				// script kind. The question is carried beside it, so a
				// subscription that replaces the operator's question is
				// reported as one.
				ScriptFilter:   decl.Filter.Script(),
				FilterQuestion: feedFilterQuestion(decl.Filter),
				Run: func(ctx context.Context, io poo.FeedIO) error {
					// The spec the stage reads is synthesized: the feed's
					// name and the declared filter alone, with no env of its
					// own, so the filter runs with the environment a command
					// feed with none declared gets.
					_, emit := r.feedFilterStage(ctx, io, feed.Spec{Name: decl.Name, Filter: decl.Filter})
					filtered := io
					filtered.Emit = emit
					return run(ctx, filtered)
				},
			})
		}
	}
	return specs, nil
}

func feedOnIdle(value, field, message string) (poo.OnIdle, error) {
	onIdle, err := onIdleOf(value)
	if err != nil {
		return "", &Error{
			Code:    CodeInvalid,
			Field:   field,
			Message: message,
			Err:     err,
		}
	}
	return onIdle, nil
}

func feedFilterQuestion(filter *feed.Filter) string {
	if filter == nil {
		return ""
	}
	return filter.Question
}

func (r *Runtime) feedFilterStage(ctx context.Context, io poo.FeedIO, spec feed.Spec) (func(scope, name string) (string, error), func(string, string, *poo.FeedImage, json.RawMessage) error) {
	session := exec.ScopedResolver(io.SessionID, io.Scope)
	return session, filterEmitter(ctx, io, spec, r.executor, exec.Lookup(r.Env), session)
}

// missingMCPServerWarning is what the build says when a profile selects a
// server the manifests do not load.
//
// The profile selects; the operator's mcp.paths still control which servers
// load, so the loss is a warning, never a failure, the way a declared feed the
// settings do not load is one.
func missingMCPServerWarning(profile, server string) string {
	return fmt.Sprintf("the profile %q selects the mcp server %q, and the settings load no server by that name", profile, server)
}

// checkMCPFeedNames refuses a resource feed a selected server declares whose
// name a feed manifest's feed already holds.
//
// A feed's name picks one feed, in the session's jobs, its origins and its
// prompt alike: two feeds of one name would leave a subscription and a
// manifest spelling the same name for different sources, so the build refuses
// what it cannot tell apart.
func (r *Runtime) checkMCPFeedNames() error {
	if r.Feeds == nil {
		return nil
	}
	for _, server := range r.MCPServers {
		for _, decl := range r.mcpSet.Feeds(server) {
			if !r.Feeds.Has(decl.Name) {
				continue
			}
			return &Error{
				Code:    CodeInvalid,
				Field:   "mcp.paths",
				Message: fmt.Sprintf("the resource feed %q of the mcp server %q is also a feed the settings load", decl.Name, server),
			}
		}
	}
	return nil
}

// resolveDecider resolves the session's decider once.
//
// The tools over the registry, the catalog's section and the session's
// own options all have to agree about whether there is a decider, so
// they read one answer rather than resolving the role again.
func (r *Runtime) resolveDecider() {
	r.deciderModel, r.decider, r.deciderErr = r.Decider()
}

// findTool reports whether the session searches the catalog instead of
// listing it.
//
// A search needs a decision model to ask, and it pays for itself only
// when the catalog is too long to show: below the threshold the whole
// catalog is in the prompt, and a search would cost a round trip to say
// what the model could already read. The decider is the session's own: a
// decider role the settings name and the catalog cannot resolve gives
// the session none.
func (r *Runtime) findTool() bool {
	return r.Registry != nil && r.findToolFor(r.Registry.Len())
}

// findToolFor reports whether a catalog of count entries is searched instead
// of listed. The session and the profiles the binary lists share the rule, so
// a listing and a session agree about the tool they offer.
func (r *Runtime) findToolFor(count int) bool {
	return count > cli.FindThreshold && r.deciderErr == nil && r.decider != nil
}

// modelInputs returns the modalities the session's chat model declares.
//
// The model's own inputs decide them: a tool that hands over an attachment is
// only useful to a model that reads one, and a session without a resolvable
// chat model is not one to guess for.
func (r *Runtime) modelInputs() []poo.Modality {
	model, err := r.ChatModel()
	if err != nil {
		return nil
	}
	return model.Inputs
}

// offersSearch reports whether the session searches the catalog.
//
// The catalog's own count decides it, and the operator's disabled list decides
// whether the session has the search at all: a session that turned find_tool
// off lists the catalog its prompt shows, and points the model at nothing it
// cannot call. A search another offering brought — the one over the MCP tools
// — says nothing about this catalog: its entries are few enough to list, and a
// catalog that vanished behind a search would cost a decision-model round trip
// for a list the model could read.
func (r *Runtime) offersSearch() bool {
	return r.findTool() && slices.Contains(r.toolNames, cli.FindToolName)
}

// offersRegistry reports whether the session offers the registry's tools.
//
// The catalog is a section of the prompt only when the model can call or
// search what it lists: a tool left out of the build takes its guidance
// with it.
func (r *Runtime) offersRegistry() bool {
	return r.registryOffered
}

// offersCatalogSection reports whether the prompt carries the catalog's own
// section.
//
// The catalog is a section only when the session offers tools over it and the
// model does not already read them: an elevated catalog is offered entry by
// entry, so its entries' names, descriptions and schemas are in the tool list
// and a section listing them again would repeat the tool list. A session whose
// profile earns no catalog carries no section either, because a tool left out
// of the build takes its guidance with it.
func (r *Runtime) offersCatalogSection() bool {
	return r.offersRegistry() && len(r.elevatedTools) == 0
}

// addRegistryTools offers the registry's tools to the session.
//
// The catalog needs the tools that read it, and a binary that links none
// of them cannot honour the settings: the operator is told which tool is
// missing, whatever profile the session runs under, and whether or not the
// profile earns the catalog. The tools themselves join the profile for the
// entries the manifests' availability rules leave available: a registry that
// declares no condition joins a profile that already offers a side-effecting
// tool, and one that declares them is narrowed entry by entry. A tool the
// profile already names is not offered twice. The disabled tools are already
// off the profile, so a tool the settings disable does not come back here.
//
// A catalog short enough to show is offered entry by entry instead of behind
// the call tool: the entries join the profile as tools of their own, and the
// call tool is not offered beside them. The catalog is still required to be
// one the binary's call tool can generate, because that tool is what builds
// them.
func (r *Runtime) addRegistryTools(opts BuildOptions) error {
	if r.Registry == nil {
		return nil
	}
	// The catalog cannot be called without at least this tool, so the binary
	// must link it whatever the profile earns.
	if err := r.requireRegistryTools(opts, registryToolNames(false)); err != nil {
		return err
	}
	offered, err := offeredRegistry(r.loaded, r.Profile.Tools)
	if err != nil {
		return err
	}
	if offered == nil {
		return nil
	}
	// The session's catalog is what its rules leave available, so the prompt
	// lists, and the call tool reaches, only the entries this profile may use.
	// A search over a narrowed catalog is decided on the narrowed count, so the
	// search tool is required only once the rules have left enough to search.
	r.Registry = offered
	find := r.findTool()
	disabled := r.disabledTools(r.Profile.Name)
	if elevated := elevatedRegistryTools(opts, offered, r.Profile.Tools, disabled); len(elevated) > 0 {
		r.elevatedTools = elevated
		r.registryOffered = true
		r.Profile.Tools = append(r.Profile.Tools, elevated...)
		return nil
	}
	names := registryToolNames(find)
	if find {
		if err := r.requireRegistryTools(opts, names); err != nil {
			return err
		}
	}
	for _, name := range names {
		if slices.Contains(disabled, name) {
			continue
		}
		r.registryOffered = true
		if slices.Contains(r.toolNames, name) {
			continue
		}
		r.Profile.Tools = append(r.Profile.Tools, toolInfo(opts.Tools[name]))
		r.toolNames = append(r.toolNames, name)
	}
	return nil
}

// requireRegistryTools reports a linked tool the settings' registry needs and
// the binary does not link.
func (r *Runtime) requireRegistryTools(opts BuildOptions, names []string) error {
	for _, name := range names {
		if linksTool(opts, name) {
			continue
		}
		switch {
		case r.registryFromDefaults:
			return &Error{
				Code:  CodeUnsupported,
				Field: "cli_registry.paths",
				Message: fmt.Sprintf("the default cli-registry directory names a cli registry, and this binary does not link the %q tool; "+
					"set cli_registry.defaults to false, or name the registries you want under cli_registry.paths", name),
			}
		}
		source := "the settings"
		if len(opts.Overrides.CLIRegistryPaths) > 0 {
			source = "the settings or POO_CLI_REGISTRY_PATH / --cli-registry-path"
		}
		return &Error{
			Code:    CodeUnsupported,
			Field:   "cli_registry.paths",
			Message: fmt.Sprintf("%s name a cli registry, and this binary does not link the %q tool", source, name),
		}
	}
	return nil
}

// registryToolNames names the tools over a registry.
//
// The catalog is searched when there is a decision model and enough
// entries to need one; otherwise the whole catalog is listed, and
// call_tool is all the model needs.
func registryToolNames(find bool) []string {
	if find {
		return []string{cli.CallToolName, cli.FindToolName}
	}
	return []string{cli.CallToolName}
}

// addMCPTools offers the tools over the MCP servers a profile selects.
//
// A selection is a set of processes the session starts, and the model reaches
// their tools through one call: a binary that links none cannot honour the
// selection and says which tool is missing — the requirement is the declared
// selection's, the way the launcher requirement is, whatever the manifests
// load. The tools the session offers are the ones over the servers it will
// start: a selection the manifests do not load starts nothing, so it earns
// neither the call nor the search, and the listing judges the same way. The
// search comes with the call when the session has a decision model, because a
// server may list more tools than the prompt holds. Both are the profile's
// offering, so a tool the settings disable does not come back here.
//
// Adding MCP tools never changes what the catalog's entries are offered as: a
// session whose catalog is short enough to show still offers each entry as a
// tool of its own.
func (r *Runtime) addMCPTools(opts BuildOptions) error {
	if len(r.MCPServers) == 0 {
		return nil
	}
	disabled := r.disabledTools(r.Profile.Name)
	names := r.mcpToolNames(disabled)
	// A selected server is a process the session starts, and the model
	// reaches its tools through this call alone: a binary that links no call
	// would start servers nothing could use.
	for _, name := range names {
		if slices.Contains(disabled, name) || linksTool(opts, name) {
			continue
		}
		return &Error{
			Code:  CodeUnsupported,
			Field: "mcp.paths",
			Message: fmt.Sprintf("the profile %q selects the mcp servers [%s], and this binary does not link the %q tool",
				r.profileName(opts), strings.Join(r.MCPServers, ", "), name),
		}
	}
	// The offering is what the session reaches: the attach starts the
	// servers the manifests load, judged the way the listing judges, so a
	// selection that loads none offers no tools over one.
	if !mcpSelectionOffers(r.mcpSet, r.MCPServers) {
		return nil
	}
	for _, name := range names {
		if slices.Contains(disabled, name) || slices.Contains(r.toolNames, name) {
			continue
		}
		r.Profile.Tools = append(r.Profile.Tools, toolInfo(opts.Tools[name]))
		r.toolNames = append(r.toolNames, name)
	}
	return nil
}

// mcpToolNames names the tools over a session's MCP servers.
//
// The call is the one a selection needs. The search comes with it when the
// session has a decision model to ask, because a server may list more tools
// than the prompt holds; a session without one lists the tools the servers
// offer instead. The search is bound with the call, so the operator's disabled
// call leaves it out too: a session with no way to call an MCP tool needs no way
// to search for one, and a search that named a tool the model cannot call would
// be worse than no hit.
func (r *Runtime) mcpToolNames(disabled []string) []string {
	names := []string{ToolCallMCP}
	if slices.Contains(disabled, ToolCallMCP) {
		return names
	}
	if r.deciderErr == nil && r.decider != nil {
		names = append(names, cli.FindToolName)
	}
	return names
}

// mcpSelectionOffers reports whether a selection starts a server the set
// loaded under the profile's own defaults word holds.
//
// A selected name the set does not hold is a server the build warns about
// and the session runs without, so a selection that holds none of the set
// starts nothing. The listing judges the word and the set the way the
// session does, so a profile the defaults would have loaded a server for —
// one that turns them off and names that server — is offered no call over
// servers it never starts.
func mcpSelectionOffers(set *manifest.Set, selection []string) bool {
	return slices.ContainsFunc(selection, func(name string) bool {
		_, ok := set.Lookup(name)
		return ok
	})
}

// offersSideEffects reports whether a tool set can change anything.
func offersSideEffects(tools []poo.ToolInfo) bool {
	for _, tool := range tools {
		if tool.Class == poo.ClassSideEffecting {
			return true
		}
	}
	return false
}

// needsExecutor reports whether an offered tool or a configured feed
// runs a command.
//
// bash and call_tool run a command through the executor, and so does every
// catalog entry the session offers as a tool of its own; a feed is a command
// of its own, so a session that runs one builds the executor too. A read-only
// profile with no feeds builds none, and a settings file naming an executor
// this binary lacks is only an error when something would run in it.
func (r *Runtime) needsExecutor() bool {
	if r.hasFeeds() || len(r.elevatedTools) > 0 {
		return true
	}
	for _, name := range r.toolNames {
		if name == ToolBash || name == cli.CallToolName {
			return true
		}
	}
	return false
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

// coreToolNames are the tools the core adds by itself.
//
// They are not the profile's to give up, so the settings cannot disable
// one.
var coreToolNames = []string{profile.JobsToolName, profile.CancelJobToolName, poo.SubscribeFeedToolName, poo.FinishToolName}

// withJobTools adds the core's job tools to a profile's tools.
//
// The core adds the job tools whenever a linked tool may run in the
// background or the session runs feeds, so the profile offers them
// with it.
func withJobTools(tools []poo.ToolInfo, feeds bool) []poo.ToolInfo {
	offered := feeds
	for _, tool := range tools {
		if tool.Background != poo.BackgroundNever {
			offered = true
		}
	}
	if !offered {
		return tools
	}
	return append(tools,
		poo.ToolInfo{Name: profile.JobsToolName, Class: poo.ClassReadOnly, Background: poo.BackgroundNever},
		poo.ToolInfo{Name: profile.CancelJobToolName, Class: poo.ClassReadOnly, Background: poo.BackgroundNever})
}

// withSubscribeTool adds the core's subscribe tool to a profile's tools.
//
// The core adds the tool when the session loads a feed the model may
// start, so the profile offers it with the tool it takes.
func withSubscribeTool(tools []poo.ToolInfo, optionalFeed bool) []poo.ToolInfo {
	if !optionalFeed {
		return tools
	}
	return append(tools, poo.ToolInfo{Name: poo.SubscribeFeedToolName, Class: poo.ClassReadOnly, Background: poo.BackgroundNever})
}

// disableTools removes the tools the settings disable.
//
// A disabled name the profile does not offer removes nothing: the setting
// is a preference over whatever profile the session runs under, so a list
// that disables a tool one profile lacks still builds under it. The names
// the setting cannot have are the binary's own: a tool the binary does not
// link cannot be offered or removed, and the tools the core adds by itself
// are not the profile's to give up.
//
// It runs before the registry joins, so the tools that are left are the
// ones the registry and the core judge.
func (r *Runtime) disableTools(opts BuildOptions) error {
	if err := validateDisabledTools(opts, "tools.disabled", r.Settings.Tools.Disabled); err != nil {
		return err
	}
	disabled := r.disabledTools(r.Profile.Name)
	r.toolNames = withoutToolNames(r.toolNames, disabled)
	r.Profile.Tools = withoutDisabled(r.Profile.Tools, disabled)
	return nil
}

func validateDisabledTools(opts BuildOptions, field string, disabled []string) error {
	for _, raw := range disabled {
		name := canonicalToolName(raw)
		if slices.Contains(coreToolNames, name) {
			return &Error{
				Code:    CodeUnsupported,
				Field:   field,
				Message: fmt.Sprintf("the settings disable the %q tool, and the core adds that tool by itself", raw),
			}
		}
		if !linksTool(opts, name) {
			return &Error{
				Code:    CodeUnsupported,
				Field:   field,
				Message: fmt.Sprintf("the settings disable the %q tool, and this binary does not link it", raw),
			}
		}
	}
	return nil
}

// canonicalToolName resolves retired tool names to the current name so
// existing settings files keep working without advertising the old name.
func canonicalToolName(name string) string {
	if name == "call_tool" {
		return cli.CallToolName
	}
	return name
}

// withoutToolNames returns the names a session still builds a tool for.
func withoutToolNames(names, disabled []string) []string {
	return slices.DeleteFunc(names, func(name string) bool {
		return slices.Contains(disabled, name)
	})
}

// withoutDisabled returns the tools a session still offers.
func withoutDisabled(tools []poo.ToolInfo, disabled []string) []poo.ToolInfo {
	return slices.DeleteFunc(tools, func(tool poo.ToolInfo) bool {
		return slices.Contains(disabled, tool.Name)
	})
}

// resolveProfiles resolves every profile the binary links.
//
// A profile that names a tool the binary does not link offers the
// tools it has: the profile cannot be run with the others, and a
// profile that cannot be run is not one the binary offers.
//
// The disabled list and the registry follow the same order here as they
// do for the session running, so the listing and the session agree: the
// disabled tools come off a profile before it is judged runnable, a
// profile the settings empty is not offered, and a profile earns the
// registry tools its own entries leave for it.
func (r *Runtime) resolveProfiles(opts BuildOptions, definitions []profile.Profile, workingDir string) ([]poo.Profile, error) {
	type manifestKey struct{ feeds, registry, mcp bool }
	type manifestPair struct {
		feeds    *feed.Set
		mcp      *manifest.Set
		registry *cli.Catalog
	}
	// The listing judges a profile's tools by the feeds it resolves — the
	// feed manifests', and the resource feeds the selected servers
	// declare — and by the registry it resolves: a feed the session will
	// run is one the listing offers the job tools for, whatever manifest
	// holds it. The selection itself still names what a session starts,
	// and no listed tool stands for a server. The tool that calls them
	// joins the listing too: a person reading what a profile offers reads
	// what a session of it offers.
	words := r.chosenWords(opts)
	chosenKey := manifestKey{feeds: words.feeds, registry: words.registry, mcp: words.mcp}
	sets := map[manifestKey]manifestPair{chosenKey: {feeds: r.Feeds, registry: r.loaded, mcp: r.mcpSet}}
	pairFor := func(key manifestKey) (manifestPair, error) {
		if pair, ok := sets[key]; ok {
			return pair, nil
		}
		// A load that fails leaves what loaded earlier in the pair: the
		// listing offers a variant whose one manifest fails without
		// losing the feeds and tools its other manifests earned.
		var pair manifestPair
		feeds, _, err := r.loadFeeds(opts, workingDir, key.feeds)
		if err != nil {
			return pair, err
		}
		pair.feeds = feeds
		mcpSet, _, err := r.loadMCP(opts, workingDir, key.mcp)
		if err != nil {
			return pair, err
		}
		pair.mcp = mcpSet
		registry, _, err := r.loadRegistry(opts, workingDir, key.registry)
		if err != nil {
			return pair, err
		}
		pair.registry = registry
		sets[key] = pair
		return pair, nil
	}
	var out []poo.Profile
	for _, definition := range definitions {
		// A settings variant carries the toggle words; a linked profile's
		// name is not in the map, so it follows the settings' word.
		variant := r.resolvedProfiles[definition.Name]
		pair, err := pairFor(manifestKey{
			feeds:    defaultsOn(r.Settings.Feeds.Defaults, variant.FeedsDefaults),
			mcp:      defaultsOn(r.Settings.MCP.Defaults, variant.mcpDefaults),
			registry: defaultsOn(r.Settings.CLIRegistry.Defaults, variant.RegistryDefaults),
		})
		if err != nil {
			// A variant whose manifests fail the load is offered with
			// whatever loaded: the build does not fail for manifests
			// sessions under other profiles never run. The session's
			// own pair is in the cache already, so the failure is
			// always a variant's.
			r.warn(fmt.Sprintf("profiles.%s: the manifests its defaults word selects do not load: %v", definition.Name, err))
		}
		feeds := pair.feeds.Len() > 0
		optionalFeed := false
		for _, spec := range pair.feeds.Specs() {
			if spec.Optional {
				optionalFeed = true
				break
			}
		}
		// A selected server's resource feed is a feed the session runs, so
		// it earns the job tools the way a manifest's feed does; it is
		// never optional, so it earns no subscribe tool. A feed whose name
		// a manifest's feed already holds is one the session refuses
		// (checkMCPFeedNames), so that server's feeds earn no tools here:
		// the profile is offered without them, and the warning is the
		// manifest-load failure's. The servers a selection the manifests
		// do not load starts nothing, so it earns neither the feeds'
		// tools nor the call over them.
		for _, server := range variant.mcpServers {
			decls := pair.mcp.Feeds(server)
			if len(decls) == 0 {
				continue
			}
			collides := false
			for _, decl := range decls {
				if pair.feeds.Has(decl.Name) {
					collides = true
					r.warn(fmt.Sprintf("profiles.%s: the resource feed %q of the mcp server %q is also a feed the settings load, and the profile is offered without it", definition.Name, decl.Name, server))
				}
			}
			if !collides {
				feeds = true
			}
		}
		disabled := r.disabledTools(definition.Name)
		resolved := poo.Profile{Name: definition.Name, Unmatched: definition.Unmatched, Feeds: slices.Clone(definition.Feeds)}
		for _, tool := range definition.Tools {
			if !linksTool(opts, tool) {
				continue
			}
			resolved.Tools = append(resolved.Tools, toolInfo(opts.Tools[tool]))
		}
		resolved.Tools = withoutDisabled(resolved.Tools, disabled)
		if len(definition.Tools) > 0 && len(resolved.Tools) == 0 {
			continue
		}
		offered, err := offeredRegistry(pair.registry, resolved.Tools)
		if err != nil {
			return nil, err
		}
		if offered != nil {
			if elevated := elevatedRegistryTools(opts, offered, resolved.Tools, disabled); len(elevated) > 0 {
				resolved.Tools = append(resolved.Tools, elevated...)
			} else {
				for _, name := range registryToolNames(r.findToolFor(offered.Len())) {
					if !linksTool(opts, name) || slices.Contains(disabled, name) {
						continue
					}
					if slices.ContainsFunc(resolved.Tools, func(tool poo.ToolInfo) bool { return tool.Name == name }) {
						continue
					}
					resolved.Tools = append(resolved.Tools, toolInfo(opts.Tools[name]))
				}
			}
		}
		if mcpSelectionOffers(pair.mcp, variant.mcpServers) && linksTool(opts, ToolCallMCP) {
			// A session of this profile reaches its servers through these
			// tools, so the listing names them the way that session does.
			for _, name := range r.mcpToolNames(disabled) {
				if !linksTool(opts, name) || slices.Contains(disabled, name) {
					continue
				}
				if slices.ContainsFunc(resolved.Tools, func(tool poo.ToolInfo) bool { return tool.Name == name }) {
					continue
				}
				resolved.Tools = append(resolved.Tools, toolInfo(opts.Tools[name]))
			}
		}
		resolved.Tools = withJobTools(resolved.Tools, feeds)
		resolved.Tools = withSubscribeTool(resolved.Tools, optionalFeed)
		out = append(out, resolved)
	}
	return out, nil
}

// CatalogOptions are the catalog options the settings describe: the settings' own layer, the
// models file's layer below it, and what the endpoints last reported below that — read from the
// cache, never probed, because a build does not reach the network.
//
// The cached layer is for the build-only paths — a session's runtime, and a report on what
// resolves. A path that probes after building (catalog.Build, `poo models`) strips Cached:
// the refresh applies the layer itself, and reading it twice warns twice when the cache is
// unreadable.
//
// One caller builds a catalog from the settings, so the layers a catalog merges
// are the settings' own answer rather than something each caller reassembles.
func (s *Settings) CatalogOptions() catalog.Options {
	return catalog.Options{
		Settings: s.CatalogSettings(),
		File:     s.modelsFileLayer,
		Cached:   true,
		// The probes ask with the key the endpoint's credential resolves to, so
		// a hosted provider's list reaches the catalog with the key the operator
		// just signed in with.
		EndpointKey: s.EndpointKey,
	}
}

// CatalogSettings returns the catalog's view of the settings.
//
// The catalog reads the endpoints, the models and the roles from the settings,
// and each model's sampling override with it. The settings say nothing else
// about the models.
func (s *Settings) CatalogSettings() catalog.Settings {
	settings := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{},
		Models:    map[string]catalog.ModelSettings{},
		Roles:     map[string]string{},
	}
	for name, endpoint := range s.Endpoints {
		settings.Endpoints[name] = catalog.EndpointSettings{
			Endpoint:      endpoint.Endpoint,
			APIKeyEnv:     endpoint.APIKeyEnv,
			APIKey:        endpoint.APIKey,
			SessionHeader: endpoint.SessionHeader,
			// An endpoint whose models are declared is not asked what it serves:
			// it has no model list, and a probe would reach it with no
			// credential. The models file declares its own, and the built-in
			// Codex provider is one of them.
			SkipProbe:               endpoint.SkipProbe,
			InstructionsPerQuestion: endpoint.InstructionsPerQuestion,
		}
	}

	for name, model := range s.Models {
		settings.Models[name] = catalog.ModelSettings{
			Endpoint:        model.Endpoint,
			ModelID:         model.ModelID,
			API:             poo.API(model.API),
			ToolsMode:       model.ToolsMode,
			Ops:             ops(model.Ops),
			Inputs:          modalities(model.Inputs),
			ContextLimit:    model.ContextLimit,
			InputLimit:      model.InputLimit,
			MaxOutput:       model.MaxOutput,
			Reasoning:       model.Reasoning,
			ReasoningLevels: model.ReasoningLevels,
			PromptVariant:   model.PromptVariant,
			Quirks:          model.Quirks,
			Sampling:        sampling(s.Sampling[name]),
		}
	}
	for role, model := range s.Roles {
		settings.Roles[role] = model
	}
	if s.Reasoning.Chat != nil {
		settings.Reasoning = *s.Reasoning.Chat
	}
	return settings
}

// ReloadModels holds the models the catalog serves now.
//
// It is the one place the runtime's model map is filled, so a caller that rebuilds the
// catalog after a reprobe — the server's refresh — holds the same answer the build does.
// A mapping the build puts between the catalog and the map belongs here, where both
// readers of it meet.
func (r *Runtime) ReloadModels() {
	list := r.Catalog.List()
	models := make(map[string]poo.ModelInfo, len(list))
	for _, model := range list {
		models[model.Name] = model
	}
	r.Models = models
}

// resolveModels builds the catalog and resolves the roles.
//
// The catalog is built from the settings and the models file, as the settings
// describe them. The chat model is the one a session cannot run without, so a
// chat role the catalog cannot resolve is a failure.
func (r *Runtime) resolveModels(opts BuildOptions) error {
	catalogOptions := r.Settings.CatalogOptions()
	catalogOptions.Logger = opts.Logger
	catalogOptions.HTTPClient = opts.HTTPClient
	catalogOptions.UserAgent = r.Build.UserAgent()
	cat := catalog.New(catalogOptions)
	if err := cat.CheckRoles(); err != nil {
		if !opts.NoChatModel || !isChatRole(err) {
			return fmt.Errorf("config: %w", err)
		}
	}
	r.Catalog = cat
	r.ReloadModels()
	return nil
}

// linksTool reports whether the binary links a tool.
//
// A map entry with no constructor links nothing: the binary has no way
// to build the tool, so it does not offer it.
func linksTool(opts BuildOptions, name string) bool {
	factory, ok := opts.Tools[name]
	return ok && factory != nil
}

// linkedTool reports whether a settings rule may name a tool.
//
// The core's own job and feed tools are linked by the core, so a rule may
// name them.
func linkedTool(opts BuildOptions, name string) bool {
	if name == profile.JobsToolName || name == profile.CancelJobToolName || name == poo.SubscribeFeedToolName {
		return true
	}
	return linksTool(opts, name)
}

// primaryArgOf returns the named tool's primary argument, the one a
// rule's prefix matches.
//
// A tool the profile offered is already built, and its spec answers. A
// linked one the profile did not offer is built for its spec alone:
// the factories are constructors, and the tool is dropped at once. The
// job tools the core adds have no factory and no primary argument.
func primaryArgOf(opts BuildOptions, tools map[string]poo.Tool, name string) string {
	if tool, ok := tools[name]; ok {
		return tool.Spec().PrimaryArg
	}
	factory, ok := opts.Tools[name]
	if !ok {
		return ""
	}
	return factory(ToolOptions{}).Spec().PrimaryArg
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

// sampling returns the settings' sampling override as the catalog reads one.
func sampling(override Sampling) poo.Sampling {
	return poo.Sampling{
		Temperature: override.Temperature,
		TopP:        override.TopP,
		TopK:        override.TopK,
		MinP:        override.MinP,
	}
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
// The tools are the ones the profile names. The tools that run a command
// — bash and the registered tools — run it through the executor the
// settings name, with the sandbox's options and the protected paths the
// settings name. The child environment is the session's own, less the
// names the settings scrub, the names the endpoints' credentials are read
// from, and the names a launcher declared for its own subsystems
// (Overrides.CredentialEnvNames), which stay in the session environment
// because the feed and a cli entry's bindings resolve against it.
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

	scrub := exec.Scrub{
		Names:       append([]string(nil), settings.Tools.Env.Scrub...),
		Passthrough: append([]string(nil), settings.Tools.Env.Passthrough...),
	}
	scrub.Names = append(scrub.Names, EnvOverrideNames...)
	if r.Catalog != nil {
		scrub.Names = append(scrub.Names, r.Catalog.CredentialEnvNames()...)
	}
	// A key provider's variable is scrubbed whether its endpoint is offered
	// or not. The variable names a credential, and a model-invoked child
	// must not read one because the catalog happens to hold no endpoint
	// that reads it.
	for _, provider := range auth.KeyProviders() {
		if !slices.Contains(scrub.Names, provider.Env) {
			scrub.Names = append(scrub.Names, provider.Env)
		}
	}
	r.Env = exec.ScrubEnv(os.Environ(), scrub)
	// A launcher's credentials are different: poo's own subsystems need them,
	// and a model-invoked subprocess must not see them. They stay in Env and
	// join the list the tools' children are scrubbed with.
	credentialScrub := exec.Scrub{
		Names:       append([]string(nil), scrub.Names...),
		Passthrough: append([]string(nil), scrub.Passthrough...),
	}
	// The settings scrub and the endpoint credentials are gone from Env as
	// well; these are not, so the names are recorded under a name that says so
	// and scrubbed from the tools' children and the session's MCP servers.
	r.DeclaredCredentialEnvNames = append([]string(nil), opts.Overrides.CredentialEnvNames...)
	credentialScrub.Names = append(credentialScrub.Names, r.DeclaredCredentialEnvNames...)
	// The rule itself is kept, not just the environment it produced: an MCP
	// server a session starts is launched with the host environment scrubbed
	// by the tools' rule, and the launch happens per session, long after the
	// build. This is that rule, assembled once — the tools' children get the
	// same value below, so the session's servers and the session's tools
	// never scrub by different rules. A declared credential is scrubbed here
	// too — a server inherits nothing, and reaches the name only through an
	// explicit reference, which resolves through the host lookup rather than
	// this rule.
	r.envScrub = credentialScrub

	executorName := DefaultExecutor
	if settings.Tools.Bash.Executor != nil {
		executorName = *settings.Tools.Bash.Executor
	}
	// A settings file cannot set up a tool the binary lacks: every bash
	// leaf belongs to the bash tool, so a binary without the tool
	// refuses the leaf by name.
	if !linksTool(opts, ToolBash) {
		if settings.Tools.Bash.Executor != nil {
			return &Error{
				Code:    CodeUnsupported,
				Field:   "tools.bash.executor",
				Message: fmt.Sprintf("the settings name an executor for the %q tool, and this binary does not link it", ToolBash),
			}
		}
		if settings.Tools.Bash.TimeoutS != nil {
			return &Error{
				Code:    CodeUnsupported,
				Field:   "tools.bash.timeout_s",
				Message: fmt.Sprintf("the settings name a timeout for the %q tool, and this binary does not link it", ToolBash),
			}
		}
	}

	// A binary that links bash, or offers a tool that runs a command,
	// must be able to resolve the executor's name whatever profile runs:
	// a typo is a settings failure, not a failure to discover later.
	if linksTool(opts, ToolBash) || r.needsExecutor() {
		feedSource := ""
		if r.hasFeeds() {
			switch {
			case len(r.Feeds.Specs()) == 0:
				// The feed manifests named nothing, so the feed the session
				// runs is a resource feed a selected mcp server declares:
				// the source is the selection, not the feeds the settings
				// never named.
				feedSource = "a selected mcp server declares a feed, and "
			case r.feedsFromDefaults:
				feedSource = "the default feeds directory names a feed, and "
			default:
				feedSource = "the settings name a feed, and "
				if len(opts.Overrides.FeedsPaths) > 0 {
					feedSource = "the settings or POO_FEEDS_PATH / --feeds-path name a feed, and "
				}
			}
		}
		if opts.Executors == nil {
			return &Error{
				Code:    CodeUnsupported,
				Field:   "tools.bash.executor",
				Message: feedSource + "this binary links no executor",
			}
		}
		if !opts.Executors.Has(executorName) {
			return &Error{
				Code:    CodeUnsupported,
				Field:   "tools.bash.executor",
				Message: fmt.Sprintf("%sthis binary links no executor named %q", feedSource, executorName),
			}
		}
	}

	// The names the session offers are the build's, not the binary's:
	// a profile, or a disabled setting, removes a tool the binary
	// links, and a refusal that says "call it directly" has to be true
	// for this session.
	offered := make([]string, 0, len(r.toolNames))
	for _, name := range r.toolNames {
		if linksTool(opts, name) {
			offered = append(offered, name)
		}
	}

	toolOptions := ToolOptions{
		Scrub:          credentialScrub,
		ProtectedPaths: protected,
		Registry:       r.offeredCatalog(),
		Offered:        offeredView(r.offeredCatalog(), r.elevatedTools, nil, r.offersSearch()),
		Search:         r.offersSearch(),
		Inputs:         r.modelInputs(),
		// A file that names an engine keeps it; an unset one gets the
		// default, the same engine the settings check judges limits by.
		CodemodeEngine: DefaultCodemodeEngine,
		ToolNames:      offered,
	}
	if settings.Tools.Codemode.Engine != nil {
		toolOptions.CodemodeEngine = *settings.Tools.Codemode.Engine
	}
	if r.needsExecutor() {
		executor, err := opts.Executors.New(executorName, exec.Options{
			Masker: protected,
			Sandbox: exec.SandboxOptions{
				Network: settings.Tools.Bwrap.Network,
				ROBinds: settings.Tools.Bwrap.ROBinds,
				RWBinds: settings.Tools.Bwrap.RWBinds,
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
		r.executor = executor
	}
	tools := map[string]poo.Tool{}
	var order []string
	for _, tool := range r.toolNames {
		if !linksTool(opts, tool) {
			continue
		}
		factory := opts.Tools[tool]
		tools[tool] = factory(toolOptions)
		order = append(order, tool)
	}
	tools, order = r.addElevatedTools(opts, toolOptions, tools, order)

	// The attach builds the tools over the servers a session starts from these
	// two values, so the offering a session reaches is one wiring and not a
	// second one written beside it.
	r.toolFactories = opts.Tools
	r.toolOptions = toolOptions
	r.Options.Tools = tools
	r.Options.ToolOrder = order
	return nil
}

// addElevatedTools builds the catalog's entries as tools of their own.
//
// The tools come from the tool the session's call tool factory builds over the
// same catalog: one factory, one executor and one set of entry definitions, so
// an entry offered directly runs exactly as the entry behind a call runs, and
// the two shapes cannot drift. The names are the ones the build decided on, in
// name order, so the tool list and the profile agree even if a factory offered
// something else.
func (r *Runtime) addElevatedTools(opts BuildOptions, toolOptions ToolOptions, tools map[string]poo.Tool, order []string) (map[string]poo.Tool, []string) {
	if len(r.elevatedTools) == 0 {
		return tools, order
	}
	factory := opts.Tools[cli.CallToolName]
	if factory == nil {
		return tools, order
	}
	set, ok := factory(toolOptions).(generatedToolset)
	if !ok {
		return tools, order
	}
	byName := map[string]poo.Tool{}
	for _, tool := range set.Tools() {
		byName[tool.Spec().Name] = tool
	}
	for _, info := range r.elevatedTools {
		tool, ok := byName[info.Name]
		if !ok {
			continue
		}
		tools[info.Name] = tool
		order = append(order, info.Name)
	}
	return tools, order
}

// resolveFeedRuns builds the feeds the session runs.
//
// A feed is a command of the session's own, so it runs the way the
// session's other commands do: through the executor the settings name,
// in the directory the session works in, with the session's scrubbed
// environment behind the manifest's own entries. A name the settings
// scrub is not there for a manifest's `env:` reference to reach.
//
// The feeds are the session's options, so a new session and a restored
// one run them alike.
func (r *Runtime) resolveFeedRuns(opts BuildOptions) error {
	if !r.hasFeeds() {
		return nil
	}
	// A feed is a command of the session's own, so the executor the
	// session resolves above is the one the feed's command runs through:
	// the binary that reaches here with no executor has already failed
	// there.
	lookup := exec.Lookup(r.Env)
	specs := make([]poo.FeedSpec, 0, r.Feeds.Len())
	for _, spec := range r.Feeds.Specs() {
		onIdle, err := onIdleOf(spec.OnIdle)
		if err != nil {
			return &Error{
				Code:    CodeInvalid,
				Field:   "feeds.paths",
				Message: fmt.Sprintf("the feed %q does not say when it may wake a run", spec.Name),
				Err:     err,
			}
		}
		run := feed.Runner(spec, r.executor, lookup)
		filterQuestion := ""
		if spec.Filter != nil {
			filterQuestion = spec.Filter.Question
		}
		specs = append(specs, poo.FeedSpec{
			Name:        spec.Name,
			Description: spec.Description,
			Restart:     spec.Restart,
			OnIdle:      onIdle,
			MaxPending:  spec.MaxPending,
			Optional:    spec.Optional,
			Scope:       scopeFields(spec.Scope),
			// A feed's filter is one of the two kinds, and the core
			// refuses a filter prompt on a feed whose manifest runs the
			// script kind. The question is carried beside it, so a
			// subscription that replaces the operator's question is
			// reported as one.
			ScriptFilter:   spec.Filter.Script(),
			FilterQuestion: filterQuestion,
			// The session hands its own working directory to the run
			// it calls, so a session created in another directory
			// than the build's runs its feed there.
			Run: func(ctx context.Context, io poo.FeedIO) error {
				// The scoped resolver carries the session's own values and
				// the ones the subscription named, so a manifest's
				// `session:id` and `scope:NAME` references both resolve
				// beside the session the instance runs beside. A filter
				// resolves the feed's own entries through it, so the
				// filter reaches what the feed reaches.
				session := exec.ScopedResolver(io.SessionID, io.Scope)
				emit := filterEmitter(ctx, io, spec, r.executor, lookup, session)
				// The feed package's image is the envelope's own shape;
				// the session's is the seam's. The two differ in the
				// JSON tags alone: a field added to one is added to the
				// other, and copied here. A nil image is a text-only
				// message — the conversion would name an image with no
				// path — so it passes through as nil.
				return run(ctx, io.Dir, session, func(from, text string, image *feed.Image) error {
					if image == nil {
						return emit(from, text, nil, nil)
					}
					return emit(from, text, &poo.FeedImage{Path: image.Path, MediaType: image.MediaType}, nil)
				}, io.Log)
			},
		})
	}
	r.Options.Feeds = specs
	return nil
}

// scopeFields maps a manifest's scope schema onto the core's.
//
// The values are the loader's and the core's are its own, so the mapping
// is here: a field either side gains without the other fails the build
// rather than being passed along or dropped.
func scopeFields(fields []feed.ScopeField) []poo.FeedScopeField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]poo.FeedScopeField, 0, len(fields))
	for _, field := range fields {
		out = append(out, poo.FeedScopeField{Name: field.Name, Type: field.Type, Pattern: field.Pattern})
	}
	return out
}

// onIdleOf maps a manifest's on_idle onto the core's idle policy.
//
// The values are the loader's and the core's are its own, so the mapping
// is one switch here: a value either side gains without the other fails
// the build rather than being passed along or ignored.
func onIdleOf(value string) (poo.OnIdle, error) {
	switch value {
	case "":
		return "", nil
	case feed.OnIdleWake:
		return poo.OnIdleWake, nil
	case feed.OnIdleHold:
		return poo.OnIdleHold, nil
	}
	return "", fmt.Errorf("the on_idle %q is neither %q nor %q", value, feed.OnIdleWake, feed.OnIdleHold)
}

// resolvePolicy builds the permission policy and its asker chain.
//
// The unmatched action comes from the command line, then the settings,
// then the profile. The asker chain is the one the settings name, or
// the one the binary asks by default. The decide asker needs a decider
// model, so a chain that names it without one is a failure.
func (r *Runtime) resolvePolicy(opts BuildOptions, workingDir string) error {
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
		// A prefix matches the tool's primary argument, and a tool
		// without one matches nothing: a deny the operator meant would
		// never fire. The policy validates that beside its matcher, from
		// the primary arguments resolved below.
		rules = append(rules, perm.Rule{
			Tool:            rule.Tool,
			Class:           poo.ToolKind(rule.Class),
			CommandPrefix:   rule.CommandPrefix,
			CommandContains: rule.CommandContains,
			Action:          perm.Action(rule.Action),
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

	// The primary arguments the command rules need are resolved once,
	// so the policy validates a rule beside the matcher that reads it.
	primaryArgs := map[string]string{}
	for _, rule := range settings.Permission.Rules {
		if (rule.CommandPrefix != "" || len(rule.CommandContains) > 0) && rule.Tool != "" {
			primaryArgs[rule.Tool] = primaryArgOf(opts, r.Options.Tools, rule.Tool)
		}
	}

	// The tools that ask about calls outside the working directory name
	// the argument that carries the path. The file tools take
	// theirs in "path"; a tool's primary argument is not it — grep's
	// is "pattern", which the doom loop matches, not the policy's
	// path boundary. A test pins "path" to each tool's schema.
	askOutside := map[string]string{}
	for _, tool := range []struct {
		name   string
		target *FileTool
	}{
		{"read", &settings.Tools.Read},
		{"grep", &settings.Tools.Grep},
		{"edit", &settings.Tools.Edit},
		{"write", &settings.Tools.Write},
	} {
		if tool.target.AskOutsideWorkingDir != nil && *tool.target.AskOutsideWorkingDir {
			askOutside[tool.name] = "path"
		}
	}

	askers, err := r.resolveAskers(opts, settings.Permission)
	if err != nil {
		return err
	}

	policy, err := perm.New(perm.Options{
		Rules:        rules,
		PrimaryArgs:  primaryArgs,
		AskOutside:   askOutside,
		Unmatched:    unmatched,
		Fallback:     fallback,
		DoomLoop:     doomLoop,
		Askers:       askers,
		ChainTimeout: chainTimeout,
		WorkingDir:   workingDir,
	})
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	r.Policy = policy
	r.Askers = askers
	r.warn(perm.Warning(policy, askers))
	return nil
}

// resolveAskers builds the asker chain.
//
// The chain is the one the settings name, or the one the binary asks by
// default. An explicit empty chain is a chain with no askers: an ask
// goes straight to the fallback. A chain the settings do not name is
// absent, and the binary's own askers apply. The decide asker is only
// in the chain when a decider model is configured: a chain that asks a
// model that is not there would wait for an answer that cannot come.
// The wire asker is only in the chain when the runtime has a wire
// client. One that has none leaves the asker out and warns instead of
// refusing the settings: the file is the operator's, and the front
// doors with a wire accept it.
func (r *Runtime) resolveAskers(opts BuildOptions, permission Permission) ([]perm.Asker, error) {
	names := permission.Askers
	if !permission.AskersNamed() {
		names = opts.Askers
	}
	decider, deciderErr := r.DeciderModel()
	var askers []perm.Asker
	var droppedWire bool
	for _, name := range names {
		switch name {
		case "decide":
			if deciderErr != nil {
				if permission.AskersNamed() {
					// The settings name the decide asker and no
					// decider model resolves: the ask would wait for
					// an answer that cannot come. The schema's rule
					// reports a settings file with no decider role;
					// here the role may name a model the catalog does
					// not hold, so the failure carries what the
					// catalog said.
					return nil, &Error{
						Code:    CodeUnavailable,
						Field:   "permission.askers",
						Message: "the decide asker has no decider model to ask",
						Err:     deciderErr,
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
				droppedWire = true
				continue
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
	if droppedWire {
		r.warn(r.droppedWireWarning())
	}
	return askers, nil
}

// droppedWireWarning is what the build says when it leaves the wire asker
// out of the chain.
//
// The settings schema owns the wording: its fairWhen rule names the
// capability the asker needs, and the check reports that rule for the asker
// this build dropped. The chain judged here is that one name, so the reason
// is the wire rule's alone: a chain the binary names itself may hold other
// askers the same build leaves out for other reasons.
func (r *Runtime) droppedWireWarning() string {
	fields := fieldsOf(r.Settings)
	fields.PermissionAskers = []string{"wire"}
	status := Check(fields, conditionsOf(r.Settings), SettingsFields{}).PermissionAskers
	if len(status.Reasons) == 0 {
		return ""
	}
	return status.Reasons[0]
}

// newDeciderAsker builds the asker that asks a decision model.
func (r *Runtime) newDeciderAsker(model poo.ModelInfo) (perm.Asker, error) {
	decider, err := r.newDecider(model)
	if err != nil {
		return nil, err
	}
	askerOptions := permissive.Options{}
	if value := r.Settings.Permission.Decide.Allow; value != nil {
		askerOptions.Allow = *value
	}
	if value := r.Settings.Permission.Decide.Deny; value != nil {
		askerOptions.Deny = *value
	}
	if value := r.Settings.Permission.Decide.TimeoutMS; value != nil {
		askerOptions.Timeout = time.Duration(*value) * time.Millisecond
	}
	return permissive.New(decider, askerOptions), nil
}

// resolveSession builds the session's store and the session's options.
//
// The store is the session's home in the logged mode, and there is
// none in the ephemeral mode: an ephemeral session writes nothing.

// CompactorOf builds the strategy the settings name, with the settings
// each strategy wires. It is the one dispatch table for strategy
// construction: a strategy added here, or a setting one gains, is
// built the same way everywhere a strategy is built.
func CompactorOf(settings *Settings) poo.Compactor {
	strategy := DefaultContextStrategy
	if settings.Context.Strategy != nil {
		strategy = *settings.Context.Strategy
	}
	switch strategy {
	case "handoff":
		handoffOptions := handoff.Options{}
		if settings.Context.Handoff.Ratio != nil {
			handoffOptions.Ratio = *settings.Context.Handoff.Ratio
		}
		if settings.Context.Handoff.MaxTokens != nil {
			handoffOptions.MaxTokens = *settings.Context.Handoff.MaxTokens
		}
		return handoff.New(handoffOptions)
	case "native":
		return native.New(native.Options{})
	case "summary":
		summaryOptions := summary.Options{}
		if settings.Context.Summary.Ratio != nil {
			summaryOptions.Ratio = *settings.Context.Summary.Ratio
		}
		if settings.Context.Summary.KeepRecentTokens != nil {
			summaryOptions.KeepRecentTokens = *settings.Context.Summary.KeepRecentTokens
		}
		if settings.Context.Summary.MaxTokens != nil {
			summaryOptions.MaxTokens = *settings.Context.Summary.MaxTokens
		}
		return summary.New(summaryOptions)
	case "pointer":
		pointerOptions := pointerstrategy.Options{}
		if settings.Context.Pointer.Ratio != nil {
			pointerOptions.Ratio = *settings.Context.Pointer.Ratio
		}
		if settings.Context.Pointer.KeepRecentTokens != nil {
			pointerOptions.KeepRecentTokens = *settings.Context.Pointer.KeepRecentTokens
		}
		if settings.Context.Pointer.MaxTokens != nil {
			pointerOptions.MaxTokens = *settings.Context.Pointer.MaxTokens
		}
		if settings.Context.Pointer.PointerMaxTokens != nil {
			pointerOptions.PointerMaxTokens = *settings.Context.Pointer.PointerMaxTokens
		}
		if settings.Context.Pointer.SnippetBudget != nil {
			pointerOptions.SnippetBudget = *settings.Context.Pointer.SnippetBudget
		}
		if settings.Context.Pointer.EscalateAfter != nil {
			pointerOptions.EscalateAfter = *settings.Context.Pointer.EscalateAfter
		}
		return pointerstrategy.New(pointerOptions)
	case "mine":
		mineOptions := mine.Options{}
		if settings.Context.Mine.Ratio != nil {
			mineOptions.Ratio = *settings.Context.Mine.Ratio
		}
		if settings.Context.Mine.KeepRecentTokens != nil {
			mineOptions.KeepRecentTokens = *settings.Context.Mine.KeepRecentTokens
		}
		if settings.Context.Mine.MaxTokens != nil {
			mineOptions.MaxTokens = *settings.Context.Mine.MaxTokens
		}
		if settings.Context.Mine.PointerMaxTokens != nil {
			mineOptions.PointerMaxTokens = *settings.Context.Mine.PointerMaxTokens
		}
		return mine.New(mineOptions)
	default:
		// The strategy named none, or nothing: the view is never
		// compacted.
		return nil
	}
}

func (r *Runtime) resolveSession(opts BuildOptions, workingDir string) error {
	settings := r.Settings
	mode := SessionMode(settings, opts.Overrides)
	expanded, err := SessionDirectory(settings, opts.Overrides)
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
		r.chatModelResolved = true
		r.Options.Model = model
		// The prompt renders in the chat model's own variant: the model
		// family's section among them. A switch to another model
		// re-renders with that model's variant.
		r.Options.PromptVariant = model.PromptVariant
		// The session talks to the chat model, so the transport is
		// part of the runtime's options. A consumer that changes the
		// model builds its own transport for it.
		transport, err := r.Transport(model)
		if err != nil {
			return err
		}
		r.Options.Transport = transport
	}
	// The compaction role gives the strategies' own requests a model of
	// their own. A fork stays on the chat model, because it re-sends
	// the last main request and only the chat model's transport can
	// stand in for that; the ask is what moves.
	if _, ok := settings.Roles["compaction"]; ok {
		model, err := r.Catalog.Role(catalog.RoleCompaction)
		if err != nil {
			return err
		}
		transport, err := r.Transport(model)
		if err != nil {
			return err
		}
		r.Options.SideTransport = transport
		r.Options.SideModel = model
	}
	// The decider is the session's as well as the policy's: the tools over
	// a cli registry ask it which entries fit, and a session without one
	// lists the whole catalog instead.
	r.Options.DeciderModel = r.deciderModel
	r.Options.Decider = r.decider
	r.Options.Profile = r.Profile.Name
	r.Options.Profiles = r.Profiles
	r.Options.Models = r.Models
	r.Options.WorkingDir = workingDir
	r.Options.Env = r.Env
	r.Options.ProtectedPaths = r.ProtectedPaths
	r.Options.RestrictRead = settings.Tools.Read.RestrictToWorkingDir != nil && *settings.Tools.Read.RestrictToWorkingDir
	r.Options.RestrictGrep = settings.Tools.Grep.RestrictToWorkingDir != nil && *settings.Tools.Grep.RestrictToWorkingDir
	r.Options.RestrictEdit = settings.Tools.Edit.RestrictToWorkingDir != nil && *settings.Tools.Edit.RestrictToWorkingDir
	r.Options.RestrictWrite = settings.Tools.Write.RestrictToWorkingDir != nil && *settings.Tools.Write.RestrictToWorkingDir
	r.Options.MaxTurns = settings.Limits.MaxTurns
	r.Options.MaxRunTokens = settings.Limits.MaxTokensPerRun
	if settings.Limits.MaxDurationS > 0 {
		r.Options.MaxDuration = time.Duration(settings.Limits.MaxDurationS) * time.Second
	}
	if settings.Limits.MaxContinuations > 0 {
		r.Options.MaxContinuations = settings.Limits.MaxContinuations
	}
	// The retry is on by default: a model that overruns its output limit
	// usually finishes when asked to say less. A settings file that names
	// 0 turns it off.
	r.Options.TruncationRetries = DefaultTruncationRetries
	if settings.Limits.TruncationRetries != nil {
		r.Options.TruncationRetries = *settings.Limits.TruncationRetries
	}
	if settings.Context.NoticeRatio != nil {
		r.warn("context.notice_ratio is removed and no longer has any effect")
	}
	if settings.Context.HardRatio != nil {
		r.warn("context.hard_ratio is removed and no longer has any effect")
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
		r.Options.Settings.BashTimeoutS = *settings.Tools.Bash.TimeoutS
	}
	if settings.Tools.Codemode.TimeoutS != nil {
		r.Options.Settings.CodemodeTimeoutS = *settings.Tools.Codemode.TimeoutS
	}
	if settings.Tools.Codemode.ScriptMaxBytes != nil {
		r.Options.Settings.CodemodeScriptMaxBytes = *settings.Tools.Codemode.ScriptMaxBytes
	}
	if settings.Tools.Codemode.OutputCapChars != nil {
		r.Options.Settings.CodemodeOutputCapChars = *settings.Tools.Codemode.OutputCapChars
	}
	if settings.Tools.Codemode.ExecSteps != nil {
		r.Options.Settings.CodemodeExecSteps = *settings.Tools.Codemode.ExecSteps
	}
	if settings.Tools.Fetch.TimeoutS != nil {
		r.Options.Settings.FetchTimeoutS = *settings.Tools.Fetch.TimeoutS
	}
	if settings.Tools.Fetch.MaxBytes != nil {
		r.Options.Settings.FetchMaxBytes = *settings.Tools.Fetch.MaxBytes
	}
	if settings.Tools.Fetch.UserAgent != nil {
		r.Options.Settings.FetchUserAgent = *settings.Tools.Fetch.UserAgent
	}
	if settings.Tools.Fetch.AllowPrivateHosts != nil {
		r.Options.Settings.FetchAllowPrivateHosts = *settings.Tools.Fetch.AllowPrivateHosts
	}
	r.Options.Settings.ObservationCapChars = caps.Observation(model.PromptLimit(), poo.CharsPerToken)
	r.Options.Settings.ReadOutputCapChars = caps.ReadOutput(model.PromptLimit(), poo.CharsPerToken)

	strategy := DefaultContextStrategy
	if settings.Context.Strategy != nil {
		strategy = *settings.Context.Strategy
	}
	r.Options.Compactor = CompactorOf(settings)
	if strategy == native.Strategy {
		return r.checkNativeCompaction()
	}
	return nil
}

// checkNativeCompaction reports that the resolved model and transport cannot
// serve the native strategy.
//
// Both are known at build time, so a request for native compaction without
// the capability fails here, not at the first window. A model switch is the
// session's business. The core refuses a native compaction for a model that
// does not declare the operation, so a run that switched fails safely there.
func (r *Runtime) checkNativeCompaction() error {
	// A build that resolved no transport has no model to check. A command
	// that only renders the prompt needs none, and the later session is what
	// compacts.
	if r.Options.Transport == nil {
		return nil
	}
	model := r.Options.Model
	name := model.Name
	if name == "" {
		name = model.ModelID
	}
	if !model.HasOp(poo.OpCompact) {
		return &Error{
			Code:  CodeUnsupported,
			Field: "context.strategy",
			Message: fmt.Sprintf("context.strategy is native, and the model %q does not declare the %q operation: "+
				"add compact to models.%s.ops", name, poo.OpCompact, name),
		}
	}
	if _, ok := r.Options.Transport.(poo.NativeCompactor); !ok {
		return &Error{
			Code:  CodeUnsupported,
			Field: "context.strategy",
			Message: fmt.Sprintf("context.strategy is native, and the transport for %q cannot compact the provider's "+
				"own state: use the openai-codex provider, or context.strategy: summary", name),
		}
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
	// The catalog is the default for its section: the binary's own
	// prompt directory and the settings' own sections come after it, so
	// an operator who writes one keeps it.
	var catalogTemplate *prompt.Template
	if r.Registry != nil && r.offersCatalogSection() {
		catalogTemplate = r.Registry.Template(r.offersSearch())
		r.Prompt = append(r.Prompt, func(b *prompt.Builder) {
			b.SetTemplate(cli.SectionName, catalogTemplate)
		})
	}
	// The binary's own prompt directory replaces the defaults that are
	// already in the builder. The settings come after it.
	if opts.PromptFS != nil {
		dir, err := prompt.Load(opts.PromptFS, promptSources(opts, r))
		if err != nil {
			return &Error{
				Code:    CodeInvalid,
				Field:   "system_prompt",
				Message: err.Error(),
				Err:     err,
			}
		}
		r.Prompt = append(r.Prompt, func(b *prompt.Builder) {
			dir.Apply(b)
			// A catalog the session cannot reach is not guidance
			// the model can use. The file still loads, so one
			// directory serves a code profile and a read-only one.
			if !r.offersCatalogSection() {
				b.Remove(cli.SectionName)
			}
		})
	}
	// The replacement is a setting, so it applies after the directory's
	// own prose. Clearing the sections first keeps the replacement whole
	// even when the directory filled them, while a later setting still
	// edits the base it leaves behind. The catalog is not the directory's:
	// it goes back, as it was before the directory.
	if opts.PromptFS != nil && settings.SystemPrompt.Replace != "" {
		replace := settings.SystemPrompt.Replace
		r.Prompt = append(r.Prompt, func(b *prompt.Builder) {
			b.Replace("")
			b.Set(BaseSection, replace)
			if catalogTemplate != nil {
				b.SetTemplate(cli.SectionName, catalogTemplate)
			}
		})
	}
	// The family section follows the model's prompt variant, and poo's
	// own default leaves it out: the profile's word turns it on. Unlike
	// the manifest defaults, absent means off — the family guidance is
	// an opt-in.
	if family := r.chosenProfile(opts).ModelFamily; family != nil && *family {
		r.Options.ModelFamily = true
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
	if err := r.resolveUserPrompt(opts); err != nil {
		return err
	}
	// The project's instructions are the working directory's AGENTS.md,
	// or CLAUDE.md when there is none, and they fill the project
	// section. Either the setting or the flag asks for them, and
	// ProjectInstructions reads them for the directory a session works
	// in: a session over the wire names its own, which need not be this
	// process's.
	if opts.Overrides.ProjectPrompt {
		r.projectPromptField = "--project-prompt"
	} else if settings.Session.ProjectPrompt != nil && *settings.Session.ProjectPrompt {
		r.projectPromptField = "session.project_prompt"
	}
	// The prompt the settings ask for is the session's prompt: the
	// changes are the session's own, and a command's own changes come
	// after them.
	r.Options.Prompt = append(r.Options.Prompt, r.Prompt...)

	// The profile that opted in hears why when the guidance cannot
	// render: the family's own warning, prefixed with the profile the
	// word belongs to. The chat model's own variant picks the text, and
	// a prompt directory that ships a family section renders its own; a
	// session that renders in another variant decides at its own start.
	if r.Options.ModelFamily {
		if !r.chatModelResolved {
			r.warn(fmt.Sprintf("profiles.%s: the build has no chat model, so the model family's guidance does not render", r.profileName(opts)))
		} else if warning, ok := prompt.FamilyWarning(poo.PromptBuilder(r.Options), r.Options.PromptVariant); ok {
			// A served session carries the note on its own start, so
			// the process does not announce the same guidance gap
			// twice. The other commands have no session surface to
			// print it with, and the build says it here.
			if opts.Wire == nil {
				r.warn(fmt.Sprintf("profiles.%s: %s", r.profileName(opts), warning))
			}
		}
	}
	return nil
}

// promptSources returns the sections the binary's prompt directory may
// replace, with the data their templates render with.
//
// The sections are the ones the build can put in a prompt: the core's
// own, every tool the binary links, the core's job tools and the cli
// registry's. The catalog renders with the registry's own data, so a
// replacement file renders the entries the way the default does.
//
// The set is the binary's, not the session's: a tool section whose tool
// the session's profile does not offer still loads, and it does not
// render, because another profile in the same binary may offer that
// tool.
func promptSources(opts BuildOptions, r *Runtime) prompt.Sources {
	sources := prompt.Sources{
		"persona":      nil,
		"base":         nil,
		"profile":      nil,
		"family":       nil,
		"environment":  nil,
		"skills":       nil,
		UserSection:    nil,
		ProjectSection: nil,
	}
	for name, factory := range opts.Tools {
		if factory == nil {
			continue
		}
		sources[prompt.ToolSection(name)] = nil
	}
	sources[prompt.ToolSection(profile.JobsToolName)] = nil
	sources[prompt.ToolSection(profile.CancelJobToolName)] = nil
	sources[prompt.ToolSection(poo.SubscribeFeedToolName)] = nil
	linksCall := linksTool(opts, cli.CallToolName)
	if r.Registry != nil || linksCall {
		sources[prompt.ToolSection(cli.CallToolName)] = nil
		sources[prompt.ToolSection(cli.FindToolName)] = nil
		// A binary that links the tools still knows the section, so a prompt
		// directory that carries it loads. A session offering no catalog
		// renders it empty, and it is removed once the directory has applied.
		sources[cli.SectionName] = r.offeredCatalog().Template(r.offersSearch()).Data()
	}
	if r.Skills != nil {
		sources[skill.SectionName] = r.Skills.Template().Data()
	}
	return sources
}

// OpenStore opens the store a session's records go to.
//
// The store holds one session open at a time, so a caller that serves
// several sessions at once opens one store for each of them. The mode
// is the session's persistence mode: the logged mode writes a file and
// the ephemeral mode keeps the records in memory.
//
// A process in the ephemeral mode serves no logged session: the mode it
// announced is the only one it has.
func (r *Runtime) OpenStore(mode, dir string) (session.Store, error) {
	if mode == string(session.ModeLogged) {
		if r.SessionMode != string(session.ModeLogged) {
			return nil, &Error{
				Code:    CodeUnsupported,
				Field:   "session.mode",
				Message: "this process is in the ephemeral mode: it serves no logged session",
			}
		}
		if dir == "" {
			dir = r.SessionDir
		}
		return session.NewStore(dir, session.WithBuild(r.Build))
	}
	return session.NewMemory(), nil
}

// ProjectInstructions returns the mutation that fills the prompt's
// project section with the instructions a directory holds, or nil when
// nothing asked for them or the directory holds none.
//
// The instructions are the session's, not the process's: a session over
// the wire works in the directory its header names, which need not be
// the one this process was built in. A file that cannot be read is an
// error — the instructions were asked for, and an empty section would
// say the project has none.
func (r *Runtime) ProjectInstructions(dir string) (func(*prompt.Builder), error) {
	if r.projectPromptField == "" {
		return nil, nil
	}
	text, err := agentsmd.Load(dir)
	if err != nil {
		return nil, &Error{
			Code:    CodeInvalid,
			Field:   r.projectPromptField,
			Message: "the project's instructions cannot be read",
			Err:     err,
		}
	}
	if text == "" {
		return nil, nil
	}
	return func(b *prompt.Builder) {
		b.Set(ProjectSection, text)
	}, nil
}

// resolveUserPrompt fills the prompt's user section with the operator's
// own instructions.
//
// A file that is named asks for them: the flag's path, then the
// settings', then the binary's default, the way every other override
// works. Naming a path is a promise, and a file that is not there fails
// the build; the binary's own default is a convention, and a file that
// is not there holds no instructions.
//
// The file is read with the build, not per session: a user's
// instructions are the same wherever the session works, so a session
// created over the wire has no reason to read them again.
func (r *Runtime) resolveUserPrompt(opts BuildOptions) error {
	settings := r.Settings
	path, field := "", ""
	named := true
	if opts.Overrides.UserPromptFile != "" {
		path, field = opts.Overrides.UserPromptFile, "--user-prompt-file"
	} else if value := settings.Session.UserPromptFile; value != nil && *value != "" {
		path, field = *value, "session.user_prompt_file"
	} else if settings.Session.UserPrompt != nil && *settings.Session.UserPrompt {
		// The setting asks for the instructions and leaves the file to
		// the binary, whose default is a convention rather than a
		// promise: a file that is not there holds no instructions.
		named = false
		path, field = opts.UserPromptFile, "session.user_prompt"
		if path == "" {
			return &Error{
				Code:    CodeInvalid,
				Field:   field,
				Message: "no file holds the user's instructions",
			}
		}
	} else {
		return nil
	}
	expanded, err := expandHome(path)
	if err != nil {
		return &Error{
			Code:    CodeInvalid,
			Field:   field,
			Message: "the user's instructions cannot be read",
			Err:     err,
		}
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		if !named && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return &Error{
			Code:    CodeInvalid,
			Field:   field,
			Message: "the user's instructions cannot be read",
			Err:     err,
		}
	}
	if len(data) == 0 {
		return nil
	}
	text := string(data)
	r.Prompt = append(r.Prompt, func(b *prompt.Builder) {
		b.Set(UserSection, text)
	})
	return nil
}

// CreateSession creates a session in the store and returns it.
//
// The session's records go to the store, and the store holds the
// session's authority until it is closed. A caller that serves several
// sessions at once opens a store for each of them.
//
// The options are the session's own: a consumer that changes them —
// the observer that sends the events to a client, say — passes them
// along.
func (r *Runtime) CreateSession(ctx context.Context, store session.Store, created *poo.SessionCreatedRecord, fns ...poo.Option) (*poo.Session, error) {
	if store == nil {
		return nil, &Error{
			Code:    CodeUnsupported,
			Message: "the session has no store: the settings are in the ephemeral mode",
		}
	}
	opts := r.Options
	// The feed fault logs live beside the session's files, so the
	// session's directory is the store's own.
	if dir := session.StoreDir(store); dir != "" {
		opts.SessionDir = dir
	}
	// The session works in the directory its header names: the tools
	// work there, and the prompt names it.
	if created.WorkingDir != "" {
		opts.WorkingDir = created.WorkingDir
	}
	// The project's instructions are read from the directory the
	// session works in: the build read none, because it does not know
	// which one this session names.
	edits, err := r.SessionPrompt(opts.WorkingDir)
	if err != nil {
		return nil, err
	}
	if _, err := store.Create(ctx, created); err != nil {
		return nil, err
	}
	if len(edits) != 0 {
		opts.Prompt = append(slices.Clone(opts.Prompt), edits...)
	}
	opts.Sink = store
	opts.SessionID = created.SessionID
	sess, err := poo.New(opts, append(fns, poo.WithPolicy(r.Policy), r.MCPAttachOption())...)
	if err != nil {
		if errors.Is(err, poo.ErrPromptRender) {
			return nil, PromptRenderError(err)
		}
		return nil, err
	}
	return sess, nil
}

// ResumeSession loads a session from the store and restores it.
//
// The restored session writes to the store it was loaded from, and the
// store holds the session's authority until it is closed. The load
// recovers the session: a torn tail is truncated, an unsettled run is
// marked interrupted, and the job results that were never delivered are
// queued again.
//
// The options are the ones the session is built with: the hooks and the
// observers a new session would carry.
func (r *Runtime) ResumeSession(ctx context.Context, store session.Store, id string, fns ...poo.Option) (*session.Resumed, error) {
	if store == nil {
		return nil, &Error{
			Code:    CodeUnsupported,
			Message: "the session has no store: the settings are in the ephemeral mode",
		}
	}
	opts := r.Options
	// The feed fault logs live beside the session's files, so the
	// session's directory is the store's own.
	if dir := session.StoreDir(store); dir != "" {
		opts.SessionDir = dir
	}
	// A restored session continues in the directory it was recorded
	// in: the core takes the recorded directory when the options name
	// none.
	opts.WorkingDir = ""
	return session.Resume(ctx, store, id, opts, append(fns, poo.WithPolicy(r.Policy), r.MCPAttachOption(), poo.WithPromptLoader(r.SessionPrompt))...)
}

// PromptOverrides are the prompt changes a client asks for.
//
// The shape is the settings' system_prompt block: the same keys and the
// same meaning. A client that changes the prompt of a session names the
// changes it wants, and the binary applies them.
type PromptOverrides struct {
	// BaseFile is the path of a file holding the base prompt. The
	// binary reads it, so a client names a file the binary may read.
	BaseFile string
	// Replace is the text that takes over the whole prompt.
	Replace string
	// Append is the text appended to the prompt's base section.
	Append string
	// Sections are the sections the client sets, by name.
	Sections map[string]string
	// Remove are the sections the client drops, by name.
	Remove []string
}

// ApplyPromptOverrides returns the prompt changes the overrides ask for.
//
// The changes are the settings' own prompt changes: the replacement
// takes over the whole prompt, the file's text is the base section,
// the appended text is added to the base section, the sections are set
// and the removed ones are dropped.
//
// The file is read here, so the binary reads it and the caller never
// does.
func (r *Runtime) ApplyPromptOverrides(o PromptOverrides) ([]func(*prompt.Builder), error) {
	var edits []func(*prompt.Builder)
	if o.Replace != "" {
		text := o.Replace
		edits = append(edits, func(b *prompt.Builder) { b.Replace(text) })
	}
	if o.BaseFile != "" {
		data, err := os.ReadFile(o.BaseFile)
		if err != nil {
			return nil, &Error{
				Code:    CodeInvalid,
				Field:   "prompt_overrides.base_file",
				Message: "the file is not there",
				Err:     err,
			}
		}
		text := string(data)
		edits = append(edits, func(b *prompt.Builder) { b.Set(BaseSection, text) })
	}
	if o.Append != "" {
		text := o.Append
		edits = append(edits, func(b *prompt.Builder) { b.Append(BaseSection, text) })
	}
	names := make([]string, 0, len(o.Sections))
	for name := range o.Sections {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		name, text := name, o.Sections[name]
		edits = append(edits, func(b *prompt.Builder) { b.Set(name, text) })
	}
	for _, name := range o.Remove {
		name := name
		edits = append(edits, func(b *prompt.Builder) { b.Remove(name) })
	}
	return edits, nil
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

// CompactionModel returns the model the session's compaction strategies
// reconstruct with.
//
// A session without a compaction role has none: the failure is only a
// failure when the caller asked for one.
func (r *Runtime) CompactionModel() (poo.ModelInfo, error) {
	if r.Catalog == nil {
		return poo.ModelInfo{}, &Error{Code: CodeInvalid, Message: "the build has no catalog"}
	}
	model, err := r.Catalog.Role(catalog.RoleCompaction)
	if err != nil {
		return poo.ModelInfo{}, fmt.Errorf("config: %w", err)
	}
	return model, nil
}

// Transport returns the transport for a model.
//
// The transport talks to the endpoint the model is served by, with the
// key EndpointKey resolves for it.
func (r *Runtime) Transport(model poo.ModelInfo) (poo.ChatTransport, error) {
	// The provider the catalog resolved decides the transport, and the wire
	// API it speaks says whether that transport can carry the model at all.
	// A provider added later is a case here, not a name every call site
	// recognises.
	switch model.Provider {
	case auth.ProviderCodex:
		return r.codexTransport(model)
	}
	// The header a session travels by is the endpoint's own declaration:
	// a gateway that requires one names it where the endpoint is declared,
	// whatever the endpoint is called. Every transport reads the one
	// lookup, so a change to how the header is resolved is made once.
	session := ""
	if endpoint, ok := r.Catalog.Endpoints()[model.Provider]; ok {
		session = endpoint.SessionHeader
	}
	switch model.API {
	case "", poo.APIChatCompletions:
		return openai.New(openai.Options{
			Endpoint:      model.Endpoint,
			Model:         model.ModelID,
			APIKey:        r.EndpointKey(model),
			SessionHeader: session,
			ToolsMode:     string(model.Tools.Mode),
			ModelInfo:     model,
			Sampling:      model.Sampling,
			HTTPClient:    r.httpClient,
			UserAgent:     r.Build.UserAgent(),
		}), nil
	case poo.APIAnthropicMessages:
		return anthropic.New(anthropic.Options{
			Endpoint:      model.Endpoint,
			Model:         model.ModelID,
			APIKey:        r.EndpointKey(model),
			SessionHeader: session,
			ModelInfo:     model,
			Sampling:      model.Sampling,
			HTTPClient:    r.httpClient,
			UserAgent:     r.Build.UserAgent(),
		}), nil
	default:
		return nil, &Error{
			Code:  CodeUnsupported,
			Field: "models." + model.Name + ".api",
			Message: fmt.Sprintf("no transport speaks the %s API for %s",
				model.API, servedBy(model)),
		}
	}
}

// servedBy names what serves a model, for a message a reader can act on: the
// provider when the catalog named one, else the endpoint itself.
func servedBy(model poo.ModelInfo) string {
	if model.Provider != "" {
		return model.Provider
	}
	return model.Endpoint
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

// DeciderFor returns the decision model and the decider that asks it.
//
// The model is the one the name picks, and the decider is the one that
// asks it. A session without a decider model has no decider: the
// caller asks for one only when the settings name a decider role.
func (r *Runtime) DeciderFor(name string) (poo.ModelInfo, decide.Decider, error) {
	if r.Catalog == nil {
		return poo.ModelInfo{}, nil, &Error{Code: CodeInvalid, Message: "the build has no catalog"}
	}
	model, err := r.Catalog.Resolve(name)
	if err != nil {
		return poo.ModelInfo{}, nil, fmt.Errorf("config: %w", err)
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
	// The Codex provider serves chat turns only, so a decider built for it would
	// ask an endpoint a question it does not answer. The settings failure names
	// the provider, which is what the operator can act on.
	if model.Provider == auth.ProviderCodex {
		return nil, &Error{
			Code:  CodeUnsupported,
			Field: "roles.decider",
			Message: fmt.Sprintf("the model %q is served by the %s provider, which does not answer decision questions",
				model.Name, auth.ProviderCodex),
		}
	}
	timeout := time.Duration(0)
	if r.Settings.Decider.TimeoutMS != nil {
		timeout = time.Duration(*r.Settings.Decider.TimeoutMS) * time.Millisecond
	}
	// The endpoint declares how a decision call's instructions ride: a
	// request-level field, or folded into the questions.
	perQuestion := false
	if endpoint, ok := r.Catalog.Endpoints()[model.Provider]; ok {
		perQuestion = endpoint.InstructionsPerQuestion
	}
	decider, err := openai.NewSystemOneDeciderFor(model, openai.SystemOneConfig{
		APIKey:                  r.EndpointKey(model),
		Timeout:                 timeout,
		HTTPClient:              r.httpClient,
		UserAgent:               r.Build.UserAgent(),
		InstructionsPerQuestion: perQuestion,
	})
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return decider, nil
}

// EndpointKey returns the key the endpoint that serves a model names.
//
// The endpoint is the model's provider, as the catalog resolved it: a model the
// settings never named is served by the endpoint the models file declared for
// it, and its key is that endpoint's. The resolution is the settings'
// endpointKey, so one rule answers for a model whichever layer declared it.
func (r *Runtime) EndpointKey(model poo.ModelInfo) string {
	endpoint, ok := r.Catalog.Endpoints()[model.Provider]
	if !ok {
		return ""
	}
	return r.Settings.EndpointKey(model.Provider, endpoint.Credential)
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

// SessionMode returns the mode a session runs in: whether it writes a
// session file or nothing at all.
//
// The settings' mode, with the command line's override.
func SessionMode(settings *Settings, overrides Overrides) string {
	mode := DefaultSessionMode
	if settings.Session.Mode != nil {
		mode = *settings.Session.Mode
	}
	if overrides.SessionMode != "" {
		mode = overrides.SessionMode
	}
	return mode
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
