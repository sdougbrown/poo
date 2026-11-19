package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	// EnvSettings is the environment variable a settings file is
	// pointed at by.
	EnvSettings = "POO_SETTINGS"
	// EnvBuiltinModels is the environment variable naming the file that
	// replaces the built-in providers' data. An empty value reads the
	// data the binary carries.
	EnvBuiltinModels = "POO_BUILTIN_MODELS"
	// ProjectDirName is the directory a project's settings live in.
	ProjectDirName = ".poo"
	// SettingsFileName is the name a settings file is discovered by, and
	// the first name settingsFileNames tries.
	SettingsFileName = "settings.json"
)

// settingsFileNames are the names a discovered settings file goes by, in the
// order a command tries them in each directory.
//
// The extension picks the parser, so a YAML or TOML file is discovered under
// its own name. A directory holding several is read through the first name it
// holds, and a JSON file therefore shadows a YAML one beside it.
var settingsFileNames = []string{
	SettingsFileName,
	"settings.yaml",
	"settings.yml",
	"settings.toml",
}

// FindSettings returns the path of the settings file a command reads.
//
// An explicit path — a flag or the environment's — is the file it
// names, and a file that is not there is an error: an explicit name
// is a promise. The project's file is found by walking up from the
// working directory, or from the current directory when the working
// directory is empty. The user's config file is the last resort. An
// empty path means no file was found, and the caller runs on the
// defaults.
func FindSettings(explicit, workingDir string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("config: reading %s: %w", explicit, err)
		}
		return explicit, nil
	}
	if env := os.Getenv(EnvSettings); env != "" {
		if _, err := os.Stat(env); err != nil {
			return "", fmt.Errorf("config: reading %s: %w", env, err)
		}
		return env, nil
	}
	if path, ok := findProjectSettings(workingDir); ok {
		return path, nil
	}
	if path, ok := userSettings(); ok {
		return path, nil
	}
	return "", nil
}

// findProjectSettings walks up from the working directory looking for
// the project's settings file.
func findProjectSettings(workingDir string) (string, bool) {
	dir := workingDir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	for {
		if path, ok := settingsFileIn(filepath.Join(dir, ProjectDirName)); ok {
			return path, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// UserConfigDir returns poo's directory under the platform's user config
// directory. It holds the default instructions and is searched first for
// user settings and credentials.
func UserConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "poo"), nil
}

// userConfigFallbackDir finds the macOS fallback without using a relative home.
func userConfigFallbackDir() (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", false
	}
	return filepath.Join(home, ".config", "poo"), true
}

// userSettings searches the native user directory before the macOS fallback.
func userSettings() (string, bool) {
	if dir, err := UserConfigDir(); err == nil {
		if path, ok := settingsFileIn(dir); ok {
			return path, true
		}
	}
	if dir, ok := userConfigFallbackDir(); ok {
		return settingsFileIn(dir)
	}
	return "", false
}

// settingsFileIn returns the settings file in dir, or false when dir holds
// none of the names.
func settingsFileIn(dir string) (string, bool) {
	for _, name := range settingsFileNames {
		path := filepath.Join(dir, name)
		if fileExists(path) {
			return path, true
		}
	}
	return "", false
}

// fileExists reports whether a regular file is at the path.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
