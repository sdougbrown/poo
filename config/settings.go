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

import "github.com/sdougbrown/poo"

//go:generate go run github.com/umpire-tools/umpire-go-gen@v0.2.1 -i schema/settings.umpire.json -output-file settings_gen.go -pkg config

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
	Profile string `json:"profile,omitempty"`
	// Permission are the permission policy's settings.
	Permission Permission `json:"permission,omitempty"`
	// SystemPrompt are the system prompt's settings.
	SystemPrompt PromptSettings `json:"system_prompt,omitempty"`
	// Context are the context window's settings.
	Context Context `json:"context,omitempty"`
	// Tools are the tools' settings.
	Tools Tools `json:"tools,omitempty"`
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
	// Session are the session's settings.
	Session Session `json:"session,omitempty"`
}

// Endpoint is one endpoint in the settings.
//
// The credential is read from the environment: the settings name the
// variable that holds it, and the variable is the only place a key
// comes from.
type Endpoint struct {
	// Endpoint is the model's endpoint.
	Endpoint string `json:"endpoint"`
	// APIKeyEnv names the environment variable holding the key.
	APIKeyEnv string `json:"api_key_env,omitempty"`
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
// rule may match on the command's prefix.
type Rule struct {
	// Tool is the name of the tool the rule matches.
	Tool string `json:"tool,omitempty"`
	// Class is the class of the tools the rule matches: read_only or
	// side_effecting.
	Class string `json:"class,omitempty"`
	// CommandPrefix matches the raw command of a bash call.
	CommandPrefix string `json:"command_prefix,omitempty"`
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

// SystemPrompt are the system prompt's settings.
//
// The base file and the replacement text are mutually exclusive: the
// prompt is either the file's text or the replacement, never both.
type PromptSettings struct {
	// BaseFile is the file holding the prompt's base section.
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
	// NoticeRatio is the share of the window at which the model is
	// told that the context is filling up.
	NoticeRatio *float64 `json:"notice_ratio,omitempty"`
	// HardRatio is the share of the window at which the run stops.
	HardRatio *float64 `json:"hard_ratio,omitempty"`
	// Strategy is how the view is compacted: handoff or none.
	Strategy *string `json:"strategy,omitempty"`
	// Handoff are the handoff strategy's settings.
	Handoff Handoff `json:"handoff,omitempty"`
}

// Handoff are the handoff strategy's settings.
type Handoff struct {
	// Ratio is the share of the window at which the handoff happens.
	Ratio *float64 `json:"ratio,omitempty"`
	// MaxTokens is the handoff's configured limit, in tokens.
	MaxTokens *int `json:"max_tokens,omitempty"`
}

// Tools are the tools' settings.
type Tools struct {
	// Bash are the bash tool's settings.
	Bash Bash `json:"bash,omitempty"`
	// Bwrap are the sandbox's settings, used when the executor is the
	// bubblewrap one.
	Bwrap Bwrap `json:"bwrap,omitempty"`
	// Env are the child environment's rules.
	Env ChildEnv `json:"env,omitempty"`
	// ProtectedPaths are the globs of the paths the tools must not
	// touch.
	ProtectedPaths []string `json:"protected_paths,omitempty"`
}

// Bash are the bash tool's settings.
type Bash struct {
	// TimeoutS bounds one command, in seconds.
	TimeoutS *int `json:"timeout_s,omitempty"`
	// Executor is the executor the commands run through.
	Executor *string `json:"executor,omitempty"`
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
	// validated against.
	SchemaFile string `json:"schema_file,omitempty"`
}

// CLIRegistry are the cli registry's settings.
type CLIRegistry struct {
	// Paths are the registry directories.
	Paths []string `json:"paths,omitempty"`
}

// Session are the session's settings.
type Session struct {
	// Mode is the persistence mode: logged or ephemeral.
	Mode *string `json:"mode,omitempty"`
	// Dir is the directory the session files live in.
	Dir *string `json:"dir,omitempty"`
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
