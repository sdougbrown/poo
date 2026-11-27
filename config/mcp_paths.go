package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// MCPManifestPaths resolves the mcp.json paths a session loads, in the
// order the session loads them: poo's default mcp directory when the
// word on the defaults keeps it on, then the settings' paths, then the
// extras a flag or the environment adds. Each path is resolved against
// the working directory the way the session resolves the settings' own.
//
// The defaults word is the profile's: the caller resolves it along the
// profile's base chain with ProfileMCPDefaults, so a command that
// reports on the manifests reads the one word the session's own profile
// runs under.
func MCPManifestPaths(settings *Settings, workingDir string, extra []string, profileDefaults *bool) ([]string, error) {
	paths, _ := manifestPaths(
		append(append([]string{}, settings.MCP.Paths...), extra...), "mcp",
		defaultsOn(settings.MCP.Defaults, profileDefaults))
	resolved, err := againstDir(paths, workingDir)
	if err != nil {
		return nil, fmt.Errorf("mcp.paths: %w", err)
	}
	return resolved, nil
}

// MCPManifestDir returns the default mcp manifest directory the
// session's defaults load: poo's own directory under the platform's
// user config directory, or the macOS fallback when that is where poo
// lives. When neither exists yet, the canonical directory is where a
// write goes, and the session's defaults pick it up once it holds a
// manifest.
func MCPManifestDir() (string, error) {
	if dir, ok := defaultManifestDir("mcp"); ok {
		return dir, nil
	}
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mcp"), nil
}

// ProjectDir returns the project's poo directory: the .poo directory of
// the nearest ancestor of workingDir that holds one, or workingDir's own
// .poo when no ancestor does. The directory need not exist yet: a command
// that writes into the project's poo directory creates it there.
func ProjectDir(workingDir string) (string, error) {
	dir := workingDir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	start := dir
	for {
		candidate := filepath.Join(dir, ProjectDirName)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join(start, ProjectDirName), nil
		}
		dir = parent
	}
}

// ProjectSettingsPath returns the settings file the project's poo
// directory holds, walking up from the working directory. A project
// with no settings file of its own has none here: the user's settings
// are not the project's, and a caller that needs the project's own
// reports none rather than reading the user's.
func ProjectSettingsPath(workingDir string) (string, bool) {
	return findProjectSettings(workingDir)
}

// ProfileMCPDefaults returns the mcp defaults word the named profile
// runs under: the nearest mcp_defaults on the profile's base chain, the
// way a build resolves the profile's own word. A name the settings do
// not define — a linked binary profile, or no name at all — leaves the
// settings' word standing, as does a chain that names no mcp_defaults.
func ProfileMCPDefaults(settings *Settings, profileName string) *bool {
	seen := map[string]bool{}
	for name := profileName; name != "" && !seen[name]; name = settings.Profiles[name].Base {
		seen[name] = true
		definition, ok := settings.Profiles[name]
		if !ok {
			break
		}
		if definition.MCPDefaults != nil {
			return definition.MCPDefaults
		}
	}
	return nil
}
