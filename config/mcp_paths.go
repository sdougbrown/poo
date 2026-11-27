package config

import (
	"os"
	"path/filepath"
)

// MCPManifestPaths resolves the mcp.json paths a session loads, in the
// order the session loads them: poo's default mcp directory when the
// word on the defaults keeps it on, then the settings' paths, then the
// extras a flag or the environment adds. Each path is resolved against
// the working directory the way the session resolves the settings' own.
// It reports whether the default directory was the only source, the way
// the session's own load reports it.
//
// The word is the caller's own resolution — for a session the profile
// word its build chose, for a command the one MCPDefaultsOn resolves
// from the profile's word — so the paths and the word a command reads
// are the session's own.
func MCPManifestPaths(settings *Settings, workingDir string, extra []string, on bool) ([]string, bool, error) {
	paths, fromDefaultsOnly := manifestPaths(
		append(append([]string{}, settings.MCP.Paths...), extra...), "mcp", on)
	if len(paths) == 0 {
		return nil, fromDefaultsOnly, nil
	}
	resolved, err := againstDir(paths, workingDir)
	if err != nil {
		return nil, fromDefaultsOnly, err
	}
	return resolved, fromDefaultsOnly, nil
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
// runs under, resolved by the profile resolution a build itself runs:
// the nearest mcp_defaults on the profile's base chain, with cycles and
// unknown bases failing the way the build's own would. A name no
// settings profile defines — a linked binary profile, or no name at
// all — leaves the settings' word standing, as does a chain that names
// no mcp_defaults.
func ProfileMCPDefaults(settings *Settings, opts BuildOptions, profileName string) (*bool, error) {
	runtime := &Runtime{Settings: settings}
	_, resolved, err := runtime.resolveProfileDefinitions(opts)
	if err != nil {
		return nil, err
	}
	if profile, ok := resolved[profileName]; ok {
		return profile.mcpDefaults, nil
	}
	return nil, nil
}

// MCPDefaultsOn applies the defaults word the way a session does: the
// profile's word wins, the settings' word stands when the profile names
// none, and the defaults load when neither says off. A command pairs it
// with ProfileMCPDefaults to read the word the session's own profile
// runs under.
func MCPDefaultsOn(settingsDefault, profileDefault *bool) bool {
	return defaultsOn(settingsDefault, profileDefault)
}
