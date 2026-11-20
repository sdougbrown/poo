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

func (r *Runtime) resolveProfileDefinitions(opts BuildOptions) ([]profile.Profile, error) {
	definitions := slices.Clone(opts.Profiles)
	for _, name := range slices.Sorted(maps.Keys(r.Settings.Profiles)) {
		field := "profiles." + name
		if name == "" {
			return nil, &Error{Code: CodeInvalid, Field: "profiles", Message: "a profile name cannot be empty"}
		}
		if _, err := profile.Find(opts.Profiles, name); err == nil {
			return nil, &Error{Code: CodeInvalid, Field: field, Message: "the name is already used by a linked profile"}
		}
		definition := r.Settings.Profiles[name]
		base, err := profile.Find(opts.Profiles, definition.Base)
		if err != nil {
			return nil, &Error{Code: CodeInvalid, Field: field + ".base", Message: fmt.Sprintf("%q is not a linked profile", definition.Base)}
		}
		if err := validateDisabledTools(opts, field+".tools.disabled", definition.Tools.Disabled); err != nil {
			return nil, err
		}
		base.Name = name
		// The variant's feeds add to the base's declaration: a
		// specialization asserts more, never less. A name the base
		// already declares is kept once, so a restatement warns
		// about nothing twice.
		for _, feed := range definition.Feeds {
			if feed == "" {
				return nil, &Error{Code: CodeInvalid, Field: field + ".feeds", Message: "a declared feed name cannot be empty"}
			}
			if !slices.Contains(base.Feeds, feed) {
				base.Feeds = append(base.Feeds, feed)
			}
		}
		definitions = append(definitions, base)
	}
	return definitions, nil
}

func (r *Runtime) disabledTools(name string) []string {
	local := r.Settings.Profiles[name].Tools.Disabled
	if len(local) == 0 {
		return r.Settings.Tools.Disabled
	}
	return append(slices.Clone(r.Settings.Tools.Disabled), local...)
}

func (r *Runtime) profilePromptFile() string {
	settings := r.Settings.SystemPrompt
	if _, overridden := settings.Sections[prompt.ProfileSection]; overridden ||
		settings.Replace != "" || slices.Contains(settings.Remove, prompt.ProfileSection) {
		return ""
	}
	return r.Settings.Profiles[r.Profile.Name].PromptFile
}

func (r *Runtime) profilePrompt() (func(*prompt.Builder), error) {
	path := r.profilePromptFile()
	if path == "" {
		return nil, nil
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Code: CodeInvalid, Field: "profiles." + r.Profile.Name + ".prompt_file", Err: err}
	}
	return func(b *prompt.Builder) { b.Set(prompt.ProfileSection, string(text)) }, nil
}

// SessionPrompt reads the prompt sections that belong to a new session rather
// than to the shared runtime.
func (r *Runtime) SessionPrompt(dir string) ([]func(*prompt.Builder), error) {
	role, err := r.profilePrompt()
	if err != nil {
		return nil, err
	}
	project, err := r.ProjectInstructions(dir)
	if err != nil {
		return nil, err
	}
	var edits []func(*prompt.Builder)
	for _, edit := range []func(*prompt.Builder){role, project} {
		if edit != nil {
			edits = append(edits, edit)
		}
	}
	return edits, nil
}
