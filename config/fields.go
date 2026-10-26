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
	f.Profile = text(s.Profile)

	f.RolesChat = text(s.Roles["chat"])
	f.RolesDecider = text(s.Roles["decider"])

	f.DeciderTimeoutMs = count(s.Decider.TimeoutMs)

	f.PermissionUnmatched = pointer(s.Permission.Unmatched)
	f.PermissionFallback = pointer(s.Permission.Fallback)
	f.PermissionDoomLoopThreshold = count(s.Permission.DoomLoop.Threshold)
	f.PermissionDoomLoopNearThreshold = count(s.Permission.DoomLoop.NearThreshold)
	f.PermissionDoomLoopAction = pointer(s.Permission.DoomLoop.Action)
	f.PermissionAskers = askers(s.Permission)
	f.PermissionChainTimeoutS = count(s.Permission.ChainTimeoutS)
	f.PermissionDecideApprove = share(s.Permission.Decide.Approve)
	f.PermissionDecideDeny = share(s.Permission.Decide.Deny)
	f.PermissionDecideTimeoutMs = count(s.Permission.Decide.TimeoutMs)

	f.SystemPromptBaseFile = text(s.SystemPrompt.BaseFile)
	f.SystemPromptReplace = text(s.SystemPrompt.Replace)
	f.SystemPromptAppend = text(s.SystemPrompt.Append)
	f.SystemPromptRemove = list(s.SystemPrompt.Remove)

	f.ContextNoticeRatio = share(s.Context.NoticeRatio)
	f.ContextHardRatio = share(s.Context.HardRatio)
	f.ContextStrategy = pointer(s.Context.Strategy)
	f.ContextHandoffRatio = share(s.Context.Handoff.Ratio)
	f.ContextHandoffMaxTokens = count(s.Context.Handoff.MaxTokens)

	f.ToolsBashTimeoutS = count(s.Tools.Bash.TimeoutS)
	f.ToolsBashExecutor = pointer(s.Tools.Bash.Executor)
	f.ToolsBwrapNetwork = flag(s.Tools.Bwrap.Network)
	f.ToolsBwrapRoBinds = list(s.Tools.Bwrap.ROBinds)
	f.ToolsBwrapRwBinds = list(s.Tools.Bwrap.RWBinds)
	f.ToolsEnvScrub = list(s.Tools.Env.Scrub)
	f.ToolsEnvPassthrough = list(s.Tools.Env.Passthrough)
	f.ToolsProtectedPaths = list(s.Tools.ProtectedPaths)

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

	f.CliregistryPaths = list(s.CLIRegistry.Paths)

	f.SessionMode = pointer(s.Session.Mode)
	f.SessionDir = pointer(s.Session.Dir)
	f.SessionRetentionMaxAgeDays = count(s.Session.Retention.MaxAgeDays)
	f.SessionRetentionMaxSessions = count(s.Session.Retention.MaxSessions)

	return f
}

// conditionsOf returns the generated conditions for the settings.
//
// The conditions are the facts the rules are written against: whether
// the settings name a decider, and whether they name a registry.
func conditionsOf(s *Settings) SettingsConditions {
	return SettingsConditions{
		HasDecider:     s.Roles["decider"] != "",
		HasCliregistry: len(s.CLIRegistry.Paths) > 0,
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
