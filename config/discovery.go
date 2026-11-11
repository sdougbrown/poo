package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// The places a settings file is discovered at, in the order a command
// reads them. The explicit path wins — the --settings flag or the
// POO_SETTINGS environment variable; then the project's
// `.poo/settings.json`, walking up from the working directory the way
// git finds its repository; then the user's config directory; and a
// command that finds nothing runs on the defaults.
const (
	// EnvSettings is the environment variable a settings file is
	// pointed at by.
	EnvSettings = "POO_SETTINGS"
	// ProjectDirName is the directory a project's settings live in.
	ProjectDirName = ".poo"
	// SettingsFileName is the name the settings go by, in the
	// project directory and in the user's config directory.
	SettingsFileName = "settings.json"
)

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
		path := filepath.Join(dir, ProjectDirName, SettingsFileName)
		if fileExists(path) {
			return path, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// userSettings returns the settings file in the user's config
// directory.
func userSettings() (string, bool) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", false
	}
	path := filepath.Join(config, "poo", SettingsFileName)
	return path, fileExists(path)
}

// fileExists reports whether a regular file is at the path.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
