package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sdougbrown/poo/config"
)

// sharedFlags are the flags every command takes.
//
// The settings are a command's configuration, and the flags change what
// the settings say. The precedence is the profile, then the settings
// file, then the flags: the last one that says something wins.
type sharedFlags struct {
	// settingsFile is the settings file the command reads.
	settingsFile string
	// systemPrompt is the file holding the prompt's base section.
	systemPrompt string
	// appendSystemPrompt is the text appended to the prompt's base
	// section.
	appendSystemPrompt string
	// permissionUnmatched is what to do with a side-effecting call no
	// rule covers.
	permissionUnmatched string
	// sessionDir is the directory the session files live in.
	sessionDir string
	// ephemeral runs the command without writing a session.
	ephemeral bool
	// profile is the profile the session is built from.
	profile string
}

// register adds the shared flags to a flag set.
func (f *sharedFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.settingsFile, "settings", "", "The settings file: JSON, TOML or YAML")
	fs.StringVar(&f.systemPrompt, "system-prompt", "", "The file holding the system prompt's base section")
	fs.StringVar(&f.appendSystemPrompt, "append-system-prompt", "", "Text appended to the system prompt, or @file")
	fs.StringVar(&f.permissionUnmatched, "permission-unmatched", "", "What to do with a side-effecting call no rule covers: allow, ask or deny")
	fs.StringVar(&f.sessionDir, "session-dir", "", "The directory the session files live in")
	fs.BoolVar(&f.ephemeral, "ephemeral", false, "Run without writing a session")
	fs.StringVar(&f.profile, "profile", "", "The profile to run under: base, code or search")
}

// settings reads the settings file and returns the settings.
//
// A command that needs no settings runs without one: the settings are
// the defaults, and the flags change them.
func (f *sharedFlags) settings() (*config.Settings, error) {
	settings := &config.Settings{Version: 1}
	if f.settingsFile == "" {
		return settings, nil
	}
	loaded, err := config.Load(f.settingsFile)
	if err != nil {
		return nil, err
	}
	return loaded, nil
}

// overrides returns the overrides the flags make.
func (f *sharedFlags) overrides() config.Overrides {
	overrides := config.Overrides{
		Profile:             f.profile,
		PermissionUnmatched: f.permissionUnmatched,
		SessionDir:          f.sessionDir,
		SystemPrompt:        f.systemPrompt,
		AppendSystemPrompt:  f.appendSystemPrompt,
	}
	if f.ephemeral {
		overrides.SessionMode = "ephemeral"
	}
	return overrides
}

// appendedPrompt returns the text the --append-system-prompt flag
// carries.
//
// The flag takes text, or @file for the text in a file.
func (f *sharedFlags) appendedPrompt() (string, error) {
	if f.appendSystemPrompt == "" {
		return "", nil
	}
	if !strings.HasPrefix(f.appendSystemPrompt, "@") {
		return f.appendSystemPrompt, nil
	}
	path := strings.TrimPrefix(f.appendSystemPrompt, "@")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), nil
}

// checkUnmatched checks the value of the --permission-unmatched flag.
func (f *sharedFlags) checkUnmatched() error {
	switch f.permissionUnmatched {
	case "", "allow", "ask", "deny":
		return nil
	default:
		return errors.New("--permission-unmatched must be allow, ask or deny")
	}
}
