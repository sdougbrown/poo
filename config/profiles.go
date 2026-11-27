package config

import (
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/prompt"
)

// HasProfile reports whether the runtime knows the name, even if settings
// make that profile unavailable to run.
func (r *Runtime) HasProfile(name string) bool {
	_, err := profile.Find(r.profileDefinitions, name)
	return err == nil
}

type profilePromptStep struct {
	// profileName is the custom profile that declared this step, so a
	// read failure names the field that asked for the file.
	profileName string
	// promptFile replaces the profile section when non-empty.
	promptFile string
	// promptAppendFile or promptAppend appends to the profile section.
	promptAppendFile string
	promptAppend     string
}

type resolvedProfile struct {
	FeedsDefaults    *bool
	RegistryDefaults *bool
	ModelFamily      *bool
	DisabledTools    []string
	// mcpServers are the MCP servers the profile selects: its base
	// chain's selection followed by its own, each name once, in the order
	// of first appearance.
	mcpServers []string
	// mcpDefaults is the profile's word on the default mcp directory, or
	// nil when the chain names none.
	mcpDefaults *bool
	promptSteps []profilePromptStep
}

func (r *Runtime) resolveProfileDefinitions(opts BuildOptions) ([]profile.Profile, map[string]resolvedProfile, error) {
	names := slices.Sorted(maps.Keys(r.Settings.Profiles))
	for _, name := range names {
		field := "profiles." + name
		if name == "" {
			return nil, nil, &Error{Code: CodeInvalid, Field: "profiles", Message: "a profile name cannot be empty"}
		}
		if _, err := profile.Find(opts.Profiles, name); err == nil {
			return nil, nil, &Error{Code: CodeInvalid, Field: field, Message: "the name is already used by a linked profile"}
		}
		definition := r.Settings.Profiles[name]
		if err := validateDisabledTools(opts, field+".tools.disabled", definition.Tools.Disabled); err != nil {
			return nil, nil, err
		}
		if definition.PromptAppend != "" && definition.PromptAppendFile != "" {
			return nil, nil, &Error{
				Code:    CodeInvalid,
				Field:   field + ".prompt_append_file",
				Message: "prompt_append and prompt_append_file are mutually exclusive: the append is the inline text or the file's, never both",
			}
		}
		for _, feed := range definition.Feeds {
			if feed == "" {
				return nil, nil, &Error{Code: CodeInvalid, Field: field + ".feeds", Message: "a declared feed name cannot be empty"}
			}
		}
		for _, server := range definition.MCPServers {
			if server == "" {
				return nil, nil, &Error{Code: CodeInvalid, Field: field + ".mcp_servers", Message: "a selected server name cannot be empty"}
			}
		}
	}

	resolvedDefs := make(map[string]profile.Profile, len(names))
	resolvedSettings := make(map[string]resolvedProfile, len(names))
	visiting := make(map[string]bool, len(names))

	var resolve func(name string) (profile.Profile, resolvedProfile, error)
	resolve = func(name string) (profile.Profile, resolvedProfile, error) {
		if def, ok := resolvedDefs[name]; ok {
			return def, resolvedSettings[name], nil
		}
		definition := r.Settings.Profiles[name]
		field := "profiles." + name

		var base profile.Profile
		var parent resolvedProfile
		if linked, err := profile.Find(opts.Profiles, definition.Base); err == nil {
			base = linked
		} else if _, custom := r.Settings.Profiles[definition.Base]; custom {
			visiting[name] = true
			if visiting[definition.Base] {
				delete(visiting, name)
				return profile.Profile{}, resolvedProfile{}, &Error{
					Code:    CodeInvalid,
					Field:   field + ".base",
					Message: fmt.Sprintf("profile inheritance forms a cycle through %q", definition.Base),
				}
			}
			var err error
			base, parent, err = resolve(definition.Base)
			delete(visiting, name)
			if err != nil {
				return profile.Profile{}, resolvedProfile{}, err
			}
		} else {
			return profile.Profile{}, resolvedProfile{}, &Error{
				Code:    CodeInvalid,
				Field:   field + ".base",
				Message: fmt.Sprintf("%q is not a linked or defined profile", definition.Base),
			}
		}

		base.Name = name
		// Clone the base's feeds before appending. This prevents
		// mutating the parent slice when it has spare capacity.
		base.Feeds = slices.Clone(base.Feeds)
		// The variant's feeds add to the base's declaration: a
		// specialization asserts more, never less. A name the base
		// already declares is kept once, so a restatement warns
		// about nothing twice.
		for _, feed := range definition.Feeds {
			if !slices.Contains(base.Feeds, feed) {
				base.Feeds = append(base.Feeds, feed)
			}
		}

		merged := resolvedProfile{
			FeedsDefaults:    parent.FeedsDefaults,
			RegistryDefaults: parent.RegistryDefaults,
			ModelFamily:      parent.ModelFamily,
			DisabledTools:    slices.Clone(parent.DisabledTools),
			mcpServers:       slices.Clone(parent.mcpServers),
			mcpDefaults:      parent.mcpDefaults,
		}
		if definition.FeedsDefaults != nil {
			merged.FeedsDefaults = definition.FeedsDefaults
		}
		if definition.RegistryDefaults != nil {
			merged.RegistryDefaults = definition.RegistryDefaults
		}
		if definition.MCPDefaults != nil {
			merged.mcpDefaults = definition.MCPDefaults
		}
		if definition.ModelFamily != nil {
			merged.ModelFamily = definition.ModelFamily
		}
		// The selection adds to the base chain's, the way the declared feeds
		// do: a specialization starts at least the servers its base starts.
		// A name the chain already selects is kept once, in the order the
		// chain first names it.
		for _, server := range definition.MCPServers {
			if !slices.Contains(merged.mcpServers, server) {
				merged.mcpServers = append(merged.mcpServers, server)
			}
		}
		for _, tool := range definition.Tools.Disabled {
			canonical := canonicalToolName(tool)
			if !slices.Contains(merged.DisabledTools, canonical) {
				merged.DisabledTools = append(merged.DisabledTools, canonical)
			}
		}
		if definition.PromptFile != "" {
			// A prompt file replaces the profile section wholesale, so
			// ancestor prompt files and appends no longer apply.
			merged.promptSteps = []profilePromptStep{{
				profileName:      name,
				promptFile:       definition.PromptFile,
				promptAppendFile: definition.PromptAppendFile,
				promptAppend:     definition.PromptAppend,
			}}
		} else {
			merged.promptSteps = slices.Clone(parent.promptSteps)
			if definition.PromptAppendFile != "" || definition.PromptAppend != "" {
				merged.promptSteps = append(merged.promptSteps, profilePromptStep{
					profileName:      name,
					promptAppendFile: definition.PromptAppendFile,
					promptAppend:     definition.PromptAppend,
				})
			}
		}

		resolvedDefs[name] = base
		resolvedSettings[name] = merged
		return base, merged, nil
	}

	definitions := slices.Clone(opts.Profiles)
	for _, name := range names {
		def, _, err := resolve(name)
		if err != nil {
			return nil, nil, err
		}
		definitions = append(definitions, def)
	}
	return definitions, resolvedSettings, nil
}

func (r *Runtime) disabledTools(name string) []string {
	local := r.resolvedProfiles[name].DisabledTools
	out := make([]string, 0, len(r.Settings.Tools.Disabled)+len(local))
	for _, tool := range r.Settings.Tools.Disabled {
		out = append(out, canonicalToolName(tool))
	}
	out = append(out, local...)
	return out
}

// profileSectionOwned reports whether an explicit system_prompt setting
// owns the profile section: a replacement, a section the settings set
// themselves, or one they removed. A section the settings own is not
// one a profile's file or append may touch.
func (r *Runtime) profileSectionOwned() bool {
	settings := r.Settings.SystemPrompt
	if _, overridden := settings.Sections[prompt.ProfileSection]; overridden ||
		settings.Replace != "" || slices.Contains(settings.Remove, prompt.ProfileSection) {
		return true
	}
	return false
}

// profilePrompt returns the prompt edits for a new session's profile
// section. Each prompt file replaces the section, and each append
// follows it in ancestor-to-leaf order. Both compose with the linked
// base's prose, which the build set earlier. An explicit system_prompt
// setting owns the section and skips all profile prompt sources.
func (r *Runtime) profilePrompt() ([]func(*prompt.Builder), error) {
	if r.profileSectionOwned() {
		return nil, nil
	}
	var edits []func(*prompt.Builder)
	for _, step := range r.resolvedProfiles[r.Profile.Name].promptSteps {
		if step.promptFile != "" {
			text, err := os.ReadFile(step.promptFile)
			if err != nil {
				return nil, &Error{Code: CodeInvalid, Field: "profiles." + step.profileName + ".prompt_file", Err: err}
			}
			body := string(text)
			edits = append(edits, func(b *prompt.Builder) { b.Set(prompt.ProfileSection, body) })
		}
		var appendText string
		if step.promptAppendFile != "" {
			text, err := os.ReadFile(step.promptAppendFile)
			if err != nil {
				return nil, &Error{Code: CodeInvalid, Field: "profiles." + step.profileName + ".prompt_append_file", Err: err}
			}
			appendText = string(text)
		} else {
			appendText = step.promptAppend
		}
		if appendText != "" {
			body := appendText
			edits = append(edits, func(b *prompt.Builder) { b.Append(prompt.ProfileSection, body) })
		}
	}
	return edits, nil
}

// SessionPrompt reads the prompt sections that belong to a new session rather
// than to the shared runtime. The sources that hold nothing contribute no
// edit: the composition appends each source's edits to one slice.
func (r *Runtime) SessionPrompt(dir string) ([]func(*prompt.Builder), error) {
	role, err := r.profilePrompt()
	if err != nil {
		return nil, err
	}
	project, err := r.ProjectInstructions(dir)
	if err != nil {
		return nil, err
	}
	edits := append([]func(*prompt.Builder){}, role...)
	if project != nil {
		edits = append(edits, project)
	}
	return edits, nil
}
