package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// MCPManifestPaths resolves the mcp.json paths a session loads, in the
// order the session loads them: poo's default mcp directory when the
// settings' word does not turn the defaults off, then the settings'
// paths, then the extras a flag adds. Each path is resolved against the
// working directory the way the session resolves the settings' own.
//
// A command that reports on or edits the manifests reads this one
// resolution, so what it shows is what a session in the same directory
// and settings would load.
func MCPManifestPaths(settings *Settings, workingDir string, extra []string) ([]string, error) {
	paths, _ := manifestPaths(
		append(append([]string{}, settings.MCP.Paths...), extra...), "mcp",
		defaultsOn(settings.MCP.Defaults, nil))
	resolved, err := againstDir(paths, workingDir)
	if err != nil {
		return nil, fmt.Errorf("mcp.paths: %w", err)
	}
	return resolved, nil
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
