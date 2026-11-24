// Package config loads poo's settings and turns them into the runtime
// pieces a session needs.
//
// The settings are a file: JSON, TOML or YAML, normalized to JSON and
// decoded strictly into a Settings struct. Every key is optional unless
// the plan marks it required, and an unknown key is an error.
//
// The decoded settings are checked twice. The generated check knows the
// rules that cross fields, and the hand-written checks in checks.go know
// the rules that depend on the binary or on the shape of a value.
//
// The settings build options and nothing else: a consumer that wants a
// session gets the same pieces a Go consumer would build by hand.
package config

import (
	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/catalog"
)

//go:generate go run github.com/umpire-tools/umpire-go-gen@v0.3.1 -i schema/settings.umpire.json -output-file settings_gen.go -pkg config

// Settings are the settings a configuration file holds.
//
// The struct is the file's shape: the keys are the keys the file uses,
// and the pointer fields say whether a key was there at all. A key that
// is absent takes the default the plan names; a key that is present
// says what the operator wants.
type Settings struct {
	// Version is the settings format version. It is 1.
	Version int `json:"version"`
	// Endpoints are the endpoints the models are served by, by name.
	Endpoints map[string]Endpoint `json:"endpoints,omitempty"`
	// Models are the models the operator configured, by name.
	Models map[string]Model `json:"models,omitempty"`
	// Roles assign a model to each role, by role name.
	Roles map[string]string `json:"roles,omitempty"`
	// Decider are the decider's settings.
	Decider Decider `json:"decider,omitempty"`
	// Profile is the name of the profile a session is built from.
	Profile  string                     `json:"profile,omitempty"`
	Profiles map[string]ProfileSettings `json:"profiles,omitempty"`
	// ProfilesFiles name the files that hold profile definitions, read as
	// layers below the settings' own profiles. The settings' word on a
	// profile name wins over a file's, and the first file that names one
	// wins over the later files. Each file uses the settings' own
	// vocabulary under a `profiles` key. Its name's extension says its
	// format: YAML the shape an operator writes, with JSON read as an
	// alternative spelling of it. A relative path resolves against the
	// settings file's own directory. A prompt_file inside a file answers
	// to that file's own directory.
	ProfilesFiles []string `json:"profiles_files,omitempty"`
	// Permission are the permission policy's settings.
	Permission Permission `json:"permission,omitempty"`
	// SystemPrompt are the system prompt's settings.
	SystemPrompt PromptSettings `json:"system_prompt,omitempty"`
	// Context are the context window's settings.
	Context Context `json:"context,omitempty"`
	Tools   Tools   `json:"tools,omitempty"`
	// Limits are the run's limits.
	Limits Limits `json:"limits,omitempty"`
	// Jobs are the background jobs' settings.
	Jobs Jobs `json:"jobs,omitempty"`
	// Reasoning are the reasoning control's settings.
	Reasoning Reasoning `json:"reasoning,omitempty"`
	// Sampling are the per-model sampling overrides, by model name.
	Sampling map[string]Sampling `json:"sampling,omitempty"`
	// Finish are the typed result's settings.
	Finish Finish `json:"finish,omitempty"`
	// CLIRegistry are the cli registry's settings.
	CLIRegistry CLIRegistry `json:"cli_registry,omitempty"`
	// Feeds are the feed manifests' settings.
	Feeds Feeds `json:"feeds,omitempty"`
	// Skills are the skills' settings.
	Skills Skills `json:"skills,omitempty"`
	// Session are the session's settings.
	Session Session `json:"session,omitempty"`
	// UI is the name of the interface a bare invocation of the
	// binary launches. The name is one the binary links; a binary
	// without the named interface shows its usage.
	UI string `json:"ui,omitempty"`
	// ModelsFile names a models file — the endpoints that can be reached, each
	// with the models it serves — that the catalog reads as a layer below these
	// settings. A field the file sets is overridden field by field by the
	// settings, and a model in it is named provider/id, so a role can name one
	// the settings never mention.
	//
	// A relative path resolves against the settings file's own directory. With
	// none named, a models.yaml beside the settings file is read, and a
	// models.json beside it is imported: that is the file pi writes.
	ModelsFile string `json:"models_file,omitempty"`
	// AuthFile names poo's own credential file: a version, and the credential
	// of each provider under it. It is the file poo writes a credential to, and
	// the one it reads.
	//
	// A relative path resolves against the settings file's own directory, like
	// models_file. With neither this nor pi_auth_file named, an auth.json beside
	// the models file is used when it is there, and poo's own file in the user's
	// config directory otherwise.
	AuthFile string `json:"auth_file,omitempty"`
	// PiAuthFile names the auth.json pi keeps its credentials in. It is the
	// compat spelling of AuthFile: pi's file, read and written in pi's shape,
	// which is a file pi is still reading.
	//
	// A relative path resolves against the settings file's own directory, like
	// models_file.
	PiAuthFile string `json:"pi_auth_file,omitempty"`
	// credentialPath is the resolved path of the file the Codex credential is
	// read from, empty when a Settings built by hand named none.
	credentialPath string
	// profilesFileDirs are the directories the profiles files' own
	// relative paths answer to, by the profile name a file declared. The
	// settings' own profiles are not here: their prompt files answer to
	// the settings file's directory.
	profilesFileDirs map[string]string
	// modelsFileLayer is the catalog layer the models file declares, below the
	// settings: the built-in provider when a credential file is there, then what the
	// models file names folded over it, in either shape. It is empty when neither
	// contributed, so it says what the file added and not whether a file was read.
	modelsFileLayer catalog.FileSettings
}

// Endpoint is one endpoint in the settings.
//
// The credential is read from the environment first: the settings name
// the variable that holds it, and the literal the endpoint carries is
// the fallback when that variable holds nothing.
type Endpoint struct {
	// Endpoint is the model's endpoint.
	Endpoint string `json:"endpoint"`
	// APIKeyEnv names the environment variable holding the key.
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// APIKey is a key written in the settings themselves. An
	// environment variable is the better neighbor; the literal is
	// what a models file written for another tool imports as.
	APIKey string `json:"api_key,omitempty"`
	// SessionHeader is the name of a header that carries the session's id,
	// for an endpoint whose gateway routes one conversation's requests
	// together. Empty means the endpoint asks for none.
	SessionHeader string `json:"session_header,omitempty"`
	// SkipProbe says the endpoint is not asked what models it serves.
	//
	// An endpoint whose models are declared — the built-in Codex provider
	// is one — has no model list to answer with, and a probe would be a
	// request it does not understand.
	SkipProbe bool `json:"skip_probe,omitempty"`
	// InstructionsPerQuestion says the endpoint answers what each
	// question asks and rejects a request-level instructions field, so a
	// decision call folds its framing into the questions.
	InstructionsPerQuestion bool `json:"instructions_per_question,omitempty"`
}

// Model is one model in the settings.
//
// A field the settings leave empty is not set: the catalog keeps what
// the curated layer and the discovery layer said.
type Model struct {
	// Endpoint names the endpoint that serves the model.
	Endpoint string `json:"endpoint"`
	// ModelID is what the endpoint calls the model.
	ModelID string `json:"model_id"`
	// API is the wire API the model is reached through. Empty means
	// chat completions.
	API string `json:"api,omitempty"`
	// ToolsMode is how the run asks for tool calls. Empty means
	// whatever the model needs.
	ToolsMode poo.ToolsMode `json:"tools_mode,omitempty"`
	// Ops are the operations the model serves.
	Ops []string `json:"ops,omitempty"`
	// Inputs are the modalities the model reads.
	Inputs []string `json:"inputs,omitempty"`
	// ContextLimit is the model's context window.
	ContextLimit int `json:"context_limit,omitempty"`
	// InputLimit is the largest prompt the endpoint accepts, for a model
	// whose endpoint takes less than its window.
	InputLimit int `json:"input_limit,omitempty"`
	// MaxOutput is the most tokens the model writes in one response.
	MaxOutput int `json:"max_output,omitempty"`
	// Reasoning is how the model's reasoning is controlled.
	Reasoning *poo.ReasoningControl `json:"reasoning,omitempty"`
	// ReasoningLevels are the reasoning levels the model accepts, lowest
	// first, e.g. off, minimal, low, medium, high, xhigh. A model that
	// names none is set by its reasoning control alone.
	ReasoningLevels []string `json:"reasoning_levels,omitempty"`
	// Quirks are the transport's named adaptations this model needs, in the
	// order they apply, e.g. "audio_url" for a server that takes an audio part
	// as a URL. A name the transport does not ship is skipped with a warning.
	Quirks []string `json:"quirks,omitempty"`
	// PromptVariant selects the prompt sections' variant text the model
	// renders in, the model family's among them.
	PromptVariant string `json:"prompt_variant,omitempty"`
}

// Decider are the decider's settings.
type Decider struct {
	// TimeoutMS is the per-call latency budget, in milliseconds.
	TimeoutMS *int `json:"timeout_ms,omitempty"`
}

// Permission are the permission policy's settings.
type Permission struct {
	// Unmatched is the action for a side-effecting call no rule
	// matches. The profile's own action applies when it is absent.
	Unmatched *string `json:"unmatched,omitempty"`
	// Fallback is the decision when the asker chain is exhausted.
	Fallback *string `json:"fallback,omitempty"`
	// DoomLoop are the doom loop's settings.
	DoomLoop DoomLoop `json:"doom_loop,omitempty"`
	// Rules are the policy's rules, in order. The first match wins.
	Rules []Rule `json:"rules,omitempty"`
	// Askers are the names of the askers in the chain, in order. An
	// empty list is a chain with no askers; an absent list takes the
	// binary's default chain.
	Askers []string `json:"askers,omitempty"`
	// askersNamed reports that the settings name the asker chain, even
	// an empty one.
	askersNamed bool
	// ChainTimeoutS bounds the whole chain, in seconds.
	ChainTimeoutS *int `json:"chain_timeout_s,omitempty"`
	// Decide are the decision asker's settings.
	Decide Decide `json:"decide,omitempty"`
}

// AskersNamed reports whether the settings name the asker chain.
//
// An empty list names a chain with no askers, so an ask goes straight
// to the fallback. An absent list names no chain, and the binary's own
// askers apply. A chain with names in it was named, whether or not the
// decode recorded the key.
func (p Permission) AskersNamed() bool {
	return p.askersNamed || len(p.Askers) > 0
}

// DoomLoop are the doom loop's settings.
type DoomLoop struct {
	// Threshold is the exact repeat count that triggers the action.
	Threshold *int `json:"threshold,omitempty"`
	// NearThreshold is the near repeat count that triggers the action.
	NearThreshold *int `json:"near_threshold,omitempty"`
	// Action is what a loop does: allow, ask or deny.
	Action *string `json:"action,omitempty"`
}

// Rule is one permission rule.
//
// A rule matches on the tool's name or on the tool's class, and a bash
// rule may match on the command's prefix or on substrings the command
// carries.
type Rule struct {
	// Tool is the name of the tool the rule matches.
	Tool string `json:"tool,omitempty"`
	// Class is the class of the tools the rule matches: read_only or
	// side_effecting.
	Class string `json:"class,omitempty"`
	// CommandPrefix matches the prefix of the call's primary argument:
	// the command of a bash call, or the argument the tool names —
	// the fetch tool's URL, for one. A URL prefix that names no scheme
	// is a hostname rule: the call's scheme decides, so one hostname
	// holds for its http and https URLs alike. A prefix with a scheme
	// keeps holding the scheme it names.
	CommandPrefix string `json:"command_prefix,omitempty"`
	// CommandContains holds substrings the call's primary argument may
	// carry: the command of a bash call, for one. One substring matching
	// is enough, wherever it sits in the command. The substrings are a
	// tripwire and not a safety boundary: a command spelled around a
	// substring — extra spaces, split flags — matches nothing. A rule
	// carries a prefix or substrings, never both.
	CommandContains []string `json:"command_contains,omitempty"`
	// Action is what the rule does with a matching call.
	Action string `json:"action"`
}

// Decide are the decision asker's settings.
type Decide struct {
	// Allow is the probability at which a call is allowed.
	Allow *float64 `json:"allow,omitempty"`
	// Deny is the probability at which a call is refused.
	Deny *float64 `json:"deny,omitempty"`
	// TimeoutMS is the per-call latency budget, in milliseconds.
	TimeoutMS *int `json:"timeout_ms,omitempty"`
}

// ProfileSettings defines a named variant of a profile linked by the binary.
type ProfileSettings struct {
	Base string `json:"base"`
	// Feeds are the feeds the profile declares it needs, by manifest
	// name. They add to the base's declared feeds, kept once each.
	// The settings' feeds.paths still control which feeds load, and
	// a declared feed the settings do not load is skipped with a
	// warning.
	Feeds      []string `json:"feeds,omitempty"`
	PromptFile string   `json:"prompt_file,omitempty"`
	// FeedsDefaults says whether the default feeds directory loads for a
	// session under this profile. The profile's word wins over the
	// settings' feeds.defaults; a key the profile does not name leaves
	// the settings' word standing.
	FeedsDefaults *bool `json:"feeds_defaults,omitempty"`
	// RegistryDefaults says the same of the default cli-registry
	// directory, against the settings' cli_registry.defaults.
	RegistryDefaults *bool `json:"cli_registry_defaults,omitempty"`
	// PromptAppendFile is the file whose text the profile appends to the
	// profile section, in place of the inline prompt_append. The two are
	// mutually exclusive, and the path answers to the same directory the
	// prompt file does.
	PromptAppendFile string `json:"prompt_append_file,omitempty"`
	// PromptAppend is the text the profile appends to the profile
	// section, after the prose already there: the linked base's own, or
	// the file's when the profile also names a prompt file. The section
	// an explicit system_prompt setting owns is left alone, the way the
	// prompt file is.
	PromptAppend string `json:"prompt_append,omitempty"`
	// ModelFamily says whether the session's prompt carries the model
	// family's guidance: a text per family, keyed by the model's prompt
	// variant. It is off unless the profile names true, and a family the
	// prompt does not know leaves the prompt unchanged.
	ModelFamily *bool        `json:"model_family,omitempty"`
	Tools       ProfileTools `json:"tools,omitempty"`
}

type ProfileTools struct {
	Disabled []string `json:"disabled,omitempty"`
}

// SystemPrompt are the system prompt's settings.
//
// The base file and the replacement text are mutually exclusive: the
// prompt is either the file's text or the replacement, never both.
type PromptSettings struct {
	// BaseFile is the file holding the prompt's base section. A relative
	// path is beside the settings file, and a leading ~/ is under the
	// home directory.
	BaseFile string `json:"base_file,omitempty"`
	// Replace is the text that takes the place of the whole prompt.
	Replace string `json:"replace,omitempty"`
	// Append is the text appended to the prompt.
	Append string `json:"append,omitempty"`
	// Sections are the sections the prompt carries, by name.
	Sections map[string]string `json:"sections,omitempty"`
	// Remove names the sections the prompt does not carry.
	Remove []string `json:"remove,omitempty"`
}

// Context are the context window's settings.
type Context struct {
	// NoticeRatio is removed and ignored: it no longer has any effect.
	NoticeRatio *float64 `json:"notice_ratio,omitempty"`
	// HardRatio is removed and ignored: it no longer has any effect.
	HardRatio *float64 `json:"hard_ratio,omitempty"`
	// Strategy is how the view is compacted: summary, mine, pointer,
	// handoff, native or none.
	Strategy *string `json:"strategy,omitempty"`
	// Handoff are the handoff strategy's settings.
	Handoff Handoff `json:"handoff,omitempty"`
	// Summary are the summary strategy's settings.
	Summary Summary `json:"summary,omitempty"`
	// Mine are the mine strategy's settings.
	Mine Mine `json:"mine,omitempty"`
	// Pointer are the pointer strategy's settings.
	Pointer Pointer `json:"pointer,omitempty"`
}

// Handoff are the handoff strategy's settings.
type Handoff struct {
	// Ratio is the share of the prompt limit at which the handoff
	// happens.
	Ratio *float64 `json:"ratio,omitempty"`
	// MaxTokens is the handoff's configured limit, in tokens.
	MaxTokens *int `json:"max_tokens,omitempty"`
}

// Summary are the summary strategy's settings.
type Summary struct {
	// Ratio is the share of the prompt limit at which the view is
	// summarized.
	Ratio *float64 `json:"ratio,omitempty"`
	// KeepRecentTokens is how much of the newest history stays verbatim.
	KeepRecentTokens *int `json:"keep_recent_tokens,omitempty"`
	// MaxTokens is the summary request's configured limit, in tokens.
	MaxTokens *int `json:"max_tokens,omitempty"`
}

// Mine are the mine strategy's settings.
type Mine struct {
	// Ratio is the share of the prompt limit at which the view is
	// mined.
	Ratio *float64 `json:"ratio,omitempty"`
	// KeepRecentTokens is how much of the newest history stays verbatim.
	KeepRecentTokens *int `json:"keep_recent_tokens,omitempty"`
	// MaxTokens is the mining request's configured limit, in tokens.
	MaxTokens *int `json:"max_tokens,omitempty"`
	// PointerMaxTokens is the pointer pass's configured limit, in
	// tokens.
	PointerMaxTokens *int `json:"pointer_max_tokens,omitempty"`
}

// Pointer are the pointer strategy's settings.
type Pointer struct {
	// Ratio is the share of the prompt limit at which the view is
	// reconstructed.
	Ratio *float64 `json:"ratio,omitempty"`
	// KeepRecentTokens is how much of the newest history stays verbatim.
	KeepRecentTokens *int `json:"keep_recent_tokens,omitempty"`
	// MaxTokens is the compose request's configured limit, in tokens.
	MaxTokens *int `json:"max_tokens,omitempty"`
	// PointerMaxTokens is the pointer pass's configured limit, in
	// tokens.
	PointerMaxTokens *int `json:"pointer_max_tokens,omitempty"`
	// SnippetBudget is the most text the queries may collect, in bytes.
	SnippetBudget *int `json:"snippet_budget,omitempty"`
	// EscalateAfter is the number of the strategy's own compactions a
	// session runs before the rest are the full-span fallback's.
	EscalateAfter *int `json:"escalate_after,omitempty"`
}

// Tools are the tools' settings.
type Tools struct {
	// Read, Grep and Edit configure file-tool reach.
	Read FileTool `json:"read,omitempty"`
	Grep FileTool `json:"grep,omitempty"`
	Edit FileTool `json:"edit,omitempty"`
	// Bash are the bash tool's settings.
	Bash Bash `json:"bash,omitempty"`
	// Codemode are the codemode tool's settings.
	Codemode Codemode `json:"codemode,omitempty"`
	// Fetch are the fetch tool's settings.
	Fetch Fetch `json:"fetch,omitempty"`
	// Bwrap are the sandbox's settings, used when the executor is the
	// bubblewrap one.
	Bwrap Bwrap `json:"bwrap,omitempty"`
	// Env are the child environment's rules.
	Env ChildEnv `json:"env,omitempty"`
	// ProtectedPaths are the globs of the paths the tools must not
	// touch.
	ProtectedPaths []string `json:"protected_paths,omitempty"`
	// Disabled are the names of the tools the session does not offer,
	// whatever profile it runs under. A name the profile does not offer
	// is ignored; a name the binary does not link, or one the core adds
	// by itself, is an error.
	Disabled []string `json:"disabled,omitempty"`
}

// FileTool configures a file tool.
type FileTool struct {
	// RestrictToWorkingDir confines the tool to the working directory.
	RestrictToWorkingDir *bool `json:"restrict_to_working_dir,omitempty"`
	// AskOutsideWorkingDir routes a call whose target lies outside the
	// working directory through the permission asker.
	AskOutsideWorkingDir *bool `json:"ask_outside_working_dir,omitempty"`
}

// Bash are the bash tool's settings.
type Bash struct {
	// TimeoutS bounds one command, in seconds.
	TimeoutS *int `json:"timeout_s,omitempty"`
	// Executor is the executor the commands run through.
	Executor *string `json:"executor,omitempty"`
}

// DefaultCodemodeEngine is the engine a settings file gets when it names
// none.
//
// The check that refuses an engine's limits and the build that selects the
// engine both read it, so the validation path and the construction path
// cannot disagree about which engine an unset setting means.
const DefaultCodemodeEngine = "quickjs"

// Codemode are the codemode tool's settings.
type Codemode struct {
	// Engine selects the script runtime. Unset means the default engine.
	Engine *string `json:"engine,omitempty"`
	// TimeoutS is a script run's wall-clock limit, in seconds.
	TimeoutS *int `json:"timeout_s,omitempty"`
	// ScriptMaxBytes is the longest script a run accepts.
	ScriptMaxBytes *int `json:"script_max_bytes,omitempty"`
	// OutputCapChars is the most printed text a run keeps.
	OutputCapChars *int `json:"output_cap_chars,omitempty"`
	// ExecSteps is the Starlark execution budget.
	ExecSteps *int `json:"exec_steps,omitempty"`
}

// Fetch are the fetch tool's settings.
type Fetch struct {
	// TimeoutS is how long one fetch waits, in seconds.
	TimeoutS *int `json:"timeout_s,omitempty"`
	// MaxBytes is the largest body one fetch reads.
	MaxBytes *int `json:"max_bytes,omitempty"`
	// UserAgent is the user agent a fetch sends. Empty means the
	// tool's own generic agent.
	UserAgent *string `json:"user_agent,omitempty"`
	// AllowPrivateHosts lifts the refusal of the loopback, link-local
	// and private addresses.
	AllowPrivateHosts *bool `json:"allow_private_hosts,omitempty"`
}

// Bwrap are the bubblewrap executor's settings.
type Bwrap struct {
	// Network reports whether the sandbox has the host's network.
	Network *bool `json:"network,omitempty"`
	// ROBinds are the host paths the sandbox mounts read-only.
	ROBinds []string `json:"ro_binds,omitempty"`
	// RWBinds are the host paths the sandbox mounts read-write.
	RWBinds []string `json:"rw_binds,omitempty"`
}

// ChildEnv are the child environment's rules.
//
// The environment a command gets is the session's environment minus the
// names the operator asked to keep out of it. A passthrough list limits
// the environment to exactly the names on it.
type ChildEnv struct {
	// Scrub are the names of the variables to remove.
	Scrub []string `json:"scrub,omitempty"`
	// Passthrough are the names the child keeps, and nothing else.
	Passthrough []string `json:"passthrough,omitempty"`
}

// Limits are the run's limits. A limit of zero is off.
type Limits struct {
	// MaxTokensPerRun is the run's token limit.
	MaxTokensPerRun int `json:"max_tokens_per_run,omitempty"`
	// MaxDurationS is the run's wall-clock limit, in seconds.
	MaxDurationS int `json:"max_duration_s,omitempty"`
	// MaxTurns is the turn limit.
	MaxTurns int `json:"max_turns,omitempty"`
	// MaxContinuations is how many continuations a run may take.
	MaxContinuations int `json:"max_continuations,omitempty"`
}

// Jobs are the background jobs' settings.
type Jobs struct {
	// MaxRunning is how many jobs may run at once.
	MaxRunning int `json:"max_running,omitempty"`
	// TimeoutS bounds one job, in seconds.
	TimeoutS int `json:"timeout_s,omitempty"`
	// OnIdle says what happens to a result that arrives while the
	// session is idle: wake or hold.
	OnIdle *string `json:"on_idle,omitempty"`
	// MaxUnattendedWakes caps the woken runs that follow one another
	// with no client input in between. Zero means no limit.
	MaxUnattendedWakes int `json:"max_unattended_wakes,omitempty"`
}

// Reasoning are the reasoning control's settings.
type Reasoning struct {
	// Chat is the reasoning level the chat model runs at: off, low,
	// medium, high or a budget in tokens.
	Chat *string `json:"chat,omitempty"`
}

// Sampling are one model's sampling overrides.
//
// A field the settings leave out is not sent: the model's own default
// applies.
type Sampling struct {
	// Temperature is the sampling temperature.
	Temperature *float64 `json:"temperature,omitempty"`
	// TopP is the nucleus sampling share.
	TopP *float64 `json:"top_p,omitempty"`
	// TopK is the number of tokens the sampler considers.
	TopK *float64 `json:"top_k,omitempty"`
	// MinP is the minimum probability a token needs.
	MinP *float64 `json:"min_p,omitempty"`
}

// Finish are the typed result's settings.
type Finish struct {
	// SchemaFile is the file holding the JSON schema the result is
	// validated against. A relative path is beside the settings file,
	// and a leading ~/ is under the home directory.
	SchemaFile string `json:"schema_file,omitempty"`
}

// CLIRegistry are the cli registry's settings.
type CLIRegistry struct {
	// Paths are the registry directories, resolved against the working
	// directory; a path that starts with ~/ is under the home directory
	// instead.
	Paths []string `json:"paths,omitempty"`
	// Defaults says whether the manifests in poo's default cli-registry
	// directory load beside the paths. They load when the key is absent
	// or true; a false turns them off, so a binary or a test runs with
	// exactly the manifests it names.
	Defaults *bool `json:"defaults,omitempty"`
}

// Feeds are the feed manifests' settings.
type Feeds struct {
	// Paths are the feed manifest directories, resolved against the
	// working directory; a path that starts with ~/ is under the home
	// directory instead.
	Paths []string `json:"paths,omitempty"`
	// Defaults says whether the manifests in poo's default feeds
	// directory load beside the paths. They load when the key is absent
	// or true; a false turns them off, so a binary or a test runs with
	// exactly the manifests it names.
	Defaults *bool `json:"defaults,omitempty"`
}

// Skills are the skills' settings.
//
// A session names the skills it offers, and the prompt says what
// exists: each skill's name and what it does. The guidance itself is a
// file the model reads when the description fits, so the prompt pays
// for a catalog line and no more.
type Skills struct {
	// Default offers the skills in the directory the rest of the family
	// keeps them in, so a person who already has skills has them without
	// saying where. It is mutually exclusive with UserPath.
	Default *bool `json:"default,omitempty"`
	// UserPath is the directory the person's skills are in, when they
	// keep them somewhere of their own. It is mutually exclusive with
	// Default.
	UserPath *string `json:"user_path,omitempty"`
	// Paths are further directories of skills, resolved against the
	// working directory; a path that starts with ~/ is under the home
	// directory instead. A path that is not there is dropped with a
	// warning: skills are guidance a session offers, and a directory
	// that is missing leaves it with less to offer rather than broken.
	Paths []string `json:"paths,omitempty"`
}

// Session are the session's settings.
type Session struct {
	// Mode is the persistence mode: logged or ephemeral.
	Mode *string `json:"mode,omitempty"`
	// Dir is the directory the session files live in.
	Dir *string `json:"dir,omitempty"`
	// ProjectPrompt includes the working directory's project
	// instructions — AGENTS.md, or CLAUDE.md when there is no
	// AGENTS.md — in the prompt. The loader reads the working
	// directory only; it never walks up to a parent.
	ProjectPrompt *bool `json:"project_prompt,omitempty"`
	// UserPrompt asks for the user's own instructions from the binary's
	// default path. It is the operator's guidance, not the project's:
	// the file is read with the build, not once per session.
	//
	// Naming a file in UserPromptFile asks for the same instructions
	// without this, the way the --user-prompt-file flag does.
	UserPrompt *bool `json:"user_prompt,omitempty"`
	// UserPromptFile is the file the user's instructions are read from.
	// Naming one asks for the instructions on its own. An empty value
	// leaves them to the binary's default path, which UserPrompt
	// reaches.
	UserPromptFile *string `json:"user_prompt_file,omitempty"`
	// ReplProfile is the name of the profile the repl runs under when
	// its command names none. The key is the repl's own word, not the
	// session's: run and serve keep the profile the command line or
	// Profile names, so the conversation the operator opens and the
	// sessions a consumer drives can follow different profiles. The
	// word reaches the repl's server only for a conversation the repl
	// starts: a reopened session runs under the profile its log
	// records. The name is one the binary links, and the build a
	// session runs under refuses one it does not.
	ReplProfile *string `json:"repl_profile,omitempty"`
	// Retention says which sessions a prune deletes.
	Retention Retention `json:"retention,omitempty"`
}

// Retention says which sessions a prune deletes.
//
// Retention is opt-in: a policy with neither an age nor a count deletes
// nothing.
type Retention struct {
	// MaxAgeDays is how old a session's last activity must be.
	MaxAgeDays *int `json:"max_age_days,omitempty"`
	// MaxSessions is how many sessions the prune keeps.
	MaxSessions *int `json:"max_sessions,omitempty"`
}
