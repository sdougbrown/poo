package config

// fieldsOf returns the generated fields for the settings.
//
// The fields hold one entry per settings leaf. A leaf the settings set
// has a value, and a leaf they did not has none: the check reads the
// fields, and it never changes the settings.
//
// The leaves whose values are objects or lists of objects are not here:
// the permission rules, the models, the endpoints and the sampling
// overrides are checked by hand, in checks.go.
func fieldsOf(s *Settings) SettingsFields {
	var f SettingsFields

	f.Version = number(float64(s.Version))
	f.Ui = text(s.UI)
	f.ModelsFile = text(s.ModelsFile)
	f.AuthFile = text(s.AuthFile)
	f.PiAuthFile = text(s.PiAuthFile)
	f.Profile = text(s.Profile)
	f.ProfilesFiles = list(s.ProfilesFiles)

	f.RolesChat = text(s.Roles["chat"])
	f.RolesDecider = text(s.Roles["decider"])
	f.RolesCompaction = text(s.Roles["compaction"])

	f.DeciderTimeoutMs = count(s.Decider.TimeoutMS)

	f.PermissionUnmatched = pointer(s.Permission.Unmatched)
	f.PermissionFallback = pointer(s.Permission.Fallback)
	f.PermissionDoomLoopThreshold = count(s.Permission.DoomLoop.Threshold)
	f.PermissionDoomLoopNearThreshold = count(s.Permission.DoomLoop.NearThreshold)
	f.PermissionDoomLoopAction = pointer(s.Permission.DoomLoop.Action)
	f.PermissionAskers = askers(s.Permission)
	f.PermissionChainTimeoutS = count(s.Permission.ChainTimeoutS)
	f.PermissionDecideAllow = share(s.Permission.Decide.Allow)
	f.PermissionDecideDeny = share(s.Permission.Decide.Deny)
	f.PermissionDecideTimeoutMs = count(s.Permission.Decide.TimeoutMS)

	f.SystemPromptBaseFile = text(s.SystemPrompt.BaseFile)
	f.SystemPromptReplace = text(s.SystemPrompt.Replace)
	f.SystemPromptAppend = text(s.SystemPrompt.Append)
	f.SystemPromptRemove = list(s.SystemPrompt.Remove)

	f.ContextStrategy = pointer(s.Context.Strategy)
	f.ContextHandoffRatio = share(s.Context.Handoff.Ratio)
	f.ContextHandoffMaxTokens = count(s.Context.Handoff.MaxTokens)
	f.ContextSummaryRatio = share(s.Context.Summary.Ratio)
	f.ContextSummaryKeepRecentTokens = count(s.Context.Summary.KeepRecentTokens)
	f.ContextSummaryMaxTokens = count(s.Context.Summary.MaxTokens)
	f.ContextMineRatio = share(s.Context.Mine.Ratio)
	f.ContextMineKeepRecentTokens = count(s.Context.Mine.KeepRecentTokens)
	f.ContextMineMaxTokens = count(s.Context.Mine.MaxTokens)
	f.ContextMinePointerMaxTokens = count(s.Context.Mine.PointerMaxTokens)

	f.ToolsReadRestrictToWorkingDir = flag(s.Tools.Read.RestrictToWorkingDir)
	f.ToolsGrepRestrictToWorkingDir = flag(s.Tools.Grep.RestrictToWorkingDir)
	f.ToolsEditRestrictToWorkingDir = flag(s.Tools.Edit.RestrictToWorkingDir)
	f.ToolsReadAskOutsideWorkingDir = flag(s.Tools.Read.AskOutsideWorkingDir)
	f.ToolsGrepAskOutsideWorkingDir = flag(s.Tools.Grep.AskOutsideWorkingDir)
	f.ToolsEditAskOutsideWorkingDir = flag(s.Tools.Edit.AskOutsideWorkingDir)
	f.ToolsBashTimeoutS = count(s.Tools.Bash.TimeoutS)
	f.ToolsBashExecutor = pointer(s.Tools.Bash.Executor)
	f.ToolsCodemodeEngine = pointer(s.Tools.Codemode.Engine)
	f.ToolsCodemodeTimeoutS = count(s.Tools.Codemode.TimeoutS)
	f.ToolsCodemodeScriptMaxBytes = count(s.Tools.Codemode.ScriptMaxBytes)
	f.ToolsCodemodeOutputCapChars = count(s.Tools.Codemode.OutputCapChars)
	f.ToolsCodemodeExecSteps = count(s.Tools.Codemode.ExecSteps)
	f.ToolsFetchTimeoutS = count(s.Tools.Fetch.TimeoutS)
	f.ToolsFetchMaxBytes = count(s.Tools.Fetch.MaxBytes)
	f.ToolsFetchUserAgent = pointer(s.Tools.Fetch.UserAgent)
	f.ToolsFetchAllowPrivateHosts = flag(s.Tools.Fetch.AllowPrivateHosts)
	f.ToolsBwrapNetwork = flag(s.Tools.Bwrap.Network)
	f.ToolsBwrapRoBinds = list(s.Tools.Bwrap.ROBinds)
	f.ToolsBwrapRwBinds = list(s.Tools.Bwrap.RWBinds)
	f.ToolsEnvScrub = list(s.Tools.Env.Scrub)
	f.ToolsEnvPassthrough = list(s.Tools.Env.Passthrough)
	f.ToolsProtectedPaths = list(s.Tools.ProtectedPaths)
	f.ToolsDisabled = list(s.Tools.Disabled)

	f.LimitsMaxTokensPerRun = number(float64(s.Limits.MaxTokensPerRun))
	f.LimitsMaxDurationS = number(float64(s.Limits.MaxDurationS))
	f.LimitsMaxTurns = number(float64(s.Limits.MaxTurns))
	f.LimitsMaxContinuations = number(float64(s.Limits.MaxContinuations))

	f.JobsMaxRunning = number(float64(s.Jobs.MaxRunning))
	f.JobsTimeoutS = number(float64(s.Jobs.TimeoutS))
	f.JobsOnIdle = pointer(s.Jobs.OnIdle)
	f.JobsMaxUnattendedWakes = number(float64(s.Jobs.MaxUnattendedWakes))

	f.ReasoningChat = pointer(s.Reasoning.Chat)

	f.FinishSchemaFile = text(s.Finish.SchemaFile)

	f.CliRegistryPaths = list(s.CLIRegistry.Paths)
	f.CliRegistryDefaults = flag(s.CLIRegistry.Defaults)

	f.SkillsDefault = flag(s.Skills.Default)
	f.SkillsUserPath = pointer(s.Skills.UserPath)
	f.SkillsPaths = list(s.Skills.Paths)
	f.FeedsPaths = list(s.Feeds.Paths)
	f.FeedsDefaults = flag(s.Feeds.Defaults)

	f.SessionMode = pointer(s.Session.Mode)
	f.SessionDir = pointer(s.Session.Dir)
	f.SessionProjectPrompt = flag(s.Session.ProjectPrompt)
	f.SessionUserPrompt = flag(s.Session.UserPrompt)
	f.SessionUserPromptFile = pointer(s.Session.UserPromptFile)
	f.SessionRetentionMaxAgeDays = count(s.Session.Retention.MaxAgeDays)
	f.SessionRetentionMaxSessions = count(s.Session.Retention.MaxSessions)

	return f
}

// conditionsOf returns the generated conditions for the settings.
//
// The conditions are the facts the rules are written against: whether
// the settings name a decider, and whether they name a registry. A
// capability condition — has_wire_client — is not the settings' to
// answer: the load answers it for a runtime that has every capability,
// and the build for the runtime it built.
func conditionsOf(s *Settings) SettingsConditions {
	return SettingsConditions{
		HasDecider:     s.Roles["decider"] != "",
		HasCliRegistry: len(s.CLIRegistry.Paths) > 0,
	}
}

// text returns a pointer to a value, or nil when the value is empty.
//
// An empty value is a value the settings do not hold: the key was
// absent, or it was there and empty.
func text(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// number returns a pointer to a value, or nil when the value is zero.
func number(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}

// pointer returns a copy of a value, or nil when the value is absent.
//
// The settings hold a pointer where the key's absence matters, and the
// fields hold a pointer of their own: the check never changes the
// settings.
func pointer(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// share returns a pointer to a value, or nil when the value is absent.
func share(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// count returns a pointer to a value, or nil when the value is absent.
//
// A set zero is a value the settings hold: the keys that use this
// helper are pointers, so zero is not the same as an omitted key, and
// the generated minimum rules see it.
func count(value *int) *float64 {
	if value == nil {
		return nil
	}
	number := float64(*value)
	return &number
}

// flag returns a pointer to a value, or nil when the value is absent.
func flag(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// list returns a copy of a list, or nil when the list is empty.
//
// A list the settings do not hold is a list that was absent: the
// check reads the difference between the two.
func list(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

// askers returns the asker chain the fields hold.
//
// The chain is the one list whose emptiness says something: a chain the
// settings name with no askers is present and empty, and the fields
// hold an empty list for it rather than none.
func askers(permission Permission) []string {
	if !permission.AskersNamed() {
		return nil
	}
	return append([]string{}, permission.Askers...)
}
