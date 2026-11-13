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
	// SettingsFileName is the name a settings file is discovered by, and
	// the first name SettingsFileNames tries.
	SettingsFileName = "settings.json"
)

// SettingsFileNames are the names a discovered settings file goes by, in the
// order a command tries them in each directory.
//
// The extension picks the parser, so a YAML or TOML file is discovered under
// its own name. A directory holding several is read through the first name it
// holds, and a JSON file therefore shadows a YAML one beside it.
var SettingsFileNames = []string{
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

// userSettings returns the settings file in the user's config
// directory.
func userSettings() (string, bool) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", false
	}
	return settingsFileIn(filepath.Join(config, "poo"))
}

// settingsFileIn returns the settings file in dir, or false when dir holds
// none of the names.
func settingsFileIn(dir string) (string, bool) {
	for _, name := range SettingsFileNames {
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
