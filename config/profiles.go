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
		// The base's feeds belong to the linked profile, so the
		// variant's additions grow a copy: a base whose slice has
		// spare capacity is never written through.
		base.Feeds = slices.Clone(base.Feeds)
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

func (r *Runtime) profilePromptFile() string {
	if r.profileSectionOwned() {
		return ""
	}
	return r.Settings.Profiles[r.Profile.Name].PromptFile
}

// profilePrompt returns the edits a new session's profile section
// carries: the file's text in place of the section, and the append's
// text after whatever the section holds. Both compose with the linked
// base's own prose, which the build set earlier.
func (r *Runtime) profilePrompt() (func(*prompt.Builder), error) {
	if r.profileSectionOwned() {
		return nil, nil
	}
	var edits []func(*prompt.Builder)
	if path := r.profilePromptFile(); path != "" {
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, &Error{Code: CodeInvalid, Field: "profiles." + r.Profile.Name + ".prompt_file", Err: err}
		}
		edits = append(edits, func(b *prompt.Builder) { b.Set(prompt.ProfileSection, string(text)) })
	}
	if text := r.Settings.Profiles[r.Profile.Name].PromptAppend; text != "" {
		edits = append(edits, func(b *prompt.Builder) { b.Append(prompt.ProfileSection, text) })
	}
	if len(edits) == 0 {
		return nil, nil
	}
	return func(b *prompt.Builder) {
		for _, edit := range edits {
			edit(b)
		}
	}, nil
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
