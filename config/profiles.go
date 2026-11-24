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
		if definition.PromptAppend != "" && definition.PromptAppendFile != "" {
			return nil, &Error{
				Code:    CodeInvalid,
				Field:   field + ".prompt_append_file",
				Message: "prompt_append and prompt_append_file are mutually exclusive: the append is the inline text or the file's, never both",
			}
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

// profilePromptFile is the prompt file the session's profile names, or
// empty when it names none. The ownership gate is profilePrompt's: the
// file is one of the sources its early return governs.
func (r *Runtime) profilePromptFile() string {
	return r.Settings.Profiles[r.Profile.Name].PromptFile
}

// profilePromptAppend returns the text the profile appends to the
// profile section: the file's when the profile names one, the inline
// text otherwise. The caller has checked that the settings name only
// one of the two.
func (r *Runtime) profilePromptAppend() (string, error) {
	definition := r.Settings.Profiles[r.Profile.Name]
	if definition.PromptAppendFile != "" {
		text, err := os.ReadFile(definition.PromptAppendFile)
		if err != nil {
			return "", &Error{Code: CodeInvalid, Field: "profiles." + r.Profile.Name + ".prompt_append_file", Err: err}
		}
		return string(text), nil
	}
	return definition.PromptAppend, nil
}

// profilePrompt returns the edits a new session's profile section
// carries: the file's text in place of the section, and the append's
// text after whatever the section holds. Both compose with the linked
// base's own prose, which the build set earlier. A section an explicit
// system_prompt setting owns is left alone: the gate's early return
// governs both sources.
func (r *Runtime) profilePrompt() ([]func(*prompt.Builder), error) {
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
	text, err := r.profilePromptAppend()
	if err != nil {
		return nil, err
	}
	if text != "" {
		edits = append(edits, func(b *prompt.Builder) { b.Append(prompt.ProfileSection, text) })
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
