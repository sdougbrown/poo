package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProject writes a settings file into a project's .poo directory
// and returns the paths.
func writeProject(t *testing.T, root string, body string) string {
	t.Helper()
	dir := filepath.Join(root, ".poo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writing the project directory: %v", err)
	}
	path := filepath.Join(dir, SettingsFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// TestTheExplicitSettingsFileWins covers the first place a command
// looks: a path a flag or the environment names is the file it names.
func TestTheExplicitSettingsFileWins(t *testing.T) {
	path := writeProject(t, t.TempDir(), `{"version":1}`)

	found, err := FindSettings(path, "")
	if err != nil {
		t.Fatalf("the explicit file was refused: %v", err)
	}
	if found != path {
		t.Errorf("the explicit file resolved to %q, want %q", found, path)
	}
}

// TestAnExplicitFileThatIsNotThereIsARefusal covers the promise an
// explicit name makes: a flag or environment path that is missing is
// an error, not a fallthrough to discovery.
func TestAnExplicitFileThatIsNotThereIsARefusal(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.json")
	if _, err := FindSettings(missing, ""); err == nil {
		t.Error("a missing explicit file resolved")
	}
	t.Setenv(EnvSettings, missing)
	if _, err := FindSettings("", ""); err == nil {
		t.Error("a missing environment file resolved")
	}
}

// TestTheProjectSettingsAreFoundWalkingUp covers the project's file:
// the working directory's .poo/settings.json is found, and so is one
// several directories up, the way git finds its repository.
func TestTheProjectSettingsAreFoundWalkingUp(t *testing.T) {
	root := t.TempDir()
	path := writeProject(t, root, `{"version":1}`)
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("writing the nested directories: %v", err)
	}

	found, err := FindSettings("", deep)
	if err != nil {
		t.Fatalf("the project file was refused: %v", err)
	}
	if found != path {
		t.Errorf("the project file resolved to %q, want %q", found, path)
	}
}

// writeSettingsFile writes a settings file named name into dir and returns the
// path.
func writeSettingsFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writing the directory: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// TestTheProjectSearchStopsWhereItStarted covers the walk's start:
// an empty working directory walks up from the current directory.
func TestTheProjectSearchStopsWhereItStarted(t *testing.T) {
	root := t.TempDir()
	path := writeProject(t, root, `{"version":1}`)
	t.Chdir(root)

	found, err := FindSettings("", "")
	if err != nil {
		t.Fatalf("the project file was refused: %v", err)
	}
	if found != path {
		t.Errorf("the project file resolved to %q, want %q", found, path)
	}
}

// TestTheUserSettingsAreTheLastResort covers the user's config file:
// a project with no settings of its own falls through to it, and a
// command that finds neither runs on the defaults.
func TestTheUserSettingsAreTheLastResort(t *testing.T) {
	empty := t.TempDir()
	config := t.TempDir()
	user := filepath.Join(config, "poo", SettingsFileName)
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		t.Fatalf("writing the config directory: %v", err)
	}
	if err := os.WriteFile(user, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", config)

	found, err := FindSettings("", empty)
	if err != nil {
		t.Fatalf("the user file was refused: %v", err)
	}
	if found != user {
		t.Errorf("the user file resolved to %q, want %q", found, user)
	}

	// No file anywhere: the empty path is the defaults' signal.
	if err := os.Remove(user); err != nil {
		t.Fatalf("removing the settings: %v", err)
	}
	if found, err := FindSettings("", empty); err != nil || found != "" {
		t.Errorf("a machine with no settings resolved to %q (%v), want the defaults", found, err)
	}
}

// TestADiscoveredSettingsFileIsReadInItsOwnFormat covers the names: a
// project's file is discovered as YAML or TOML too, and the extension picks
// the parser, so what a command reads is a document it can decode.
func TestADiscoveredSettingsFileIsReadInItsOwnFormat(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "settings.yaml", body: "version: 1\nprofile: code\n"},
		{name: "settings.yml", body: "version: 1\nprofile: code\n"},
		{name: "settings.toml", body: "version = 1\nprofile = \"code\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := writeSettingsFile(t, filepath.Join(root, ProjectDirName), tt.name, tt.body)

			found, err := FindSettings("", root)
			if err != nil {
				t.Fatalf("the file was refused: %v", err)
			}
			if found != path {
				t.Fatalf("the file resolved to %q, want %q", found, path)
			}
			settings, err := Load(found)
			if err != nil {
				t.Fatalf("reading the discovered file: %v", err)
			}
			if settings.Profile != "code" {
				t.Errorf("the file reads as %q, want profile code", settings.Profile)
			}
		})
	}
}

// TestTheFirstSettingsNameWins covers a directory holding several: the names
// are tried in order, so a JSON file shadows a YAML one beside it.
func TestTheFirstSettingsNameWins(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ProjectDirName)
	jsonPath := writeSettingsFile(t, dir, SettingsFileName, `{"version":1}`)
	writeSettingsFile(t, dir, "settings.yaml", "version: 1\n")

	found, err := FindSettings("", root)
	if err != nil {
		t.Fatalf("the file was refused: %v", err)
	}
	if found != jsonPath {
		t.Errorf("the directory resolved to %q, want %q", found, jsonPath)
	}
}

// TestTheUsersSettingsAreFoundUnderAnyName covers the user's config
// directory: the names are tried there too, and the file is read in its own
// format.
func TestTheUsersSettingsAreFoundUnderAnyName(t *testing.T) {
	t.Setenv(EnvSettings, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("reading the config directory: %v", err)
	}
	path := writeSettingsFile(t, filepath.Join(config, "poo"), "settings.toml", "version = 1\n")

	found, err := FindSettings("", t.TempDir())
	if err != nil {
		t.Fatalf("the user file was refused: %v", err)
	}
	if found != path {
		t.Errorf("the user file resolved to %q, want %q", found, path)
	}
	if _, err := Load(found); err != nil {
		t.Errorf("reading the discovered user file: %v", err)
	}
}

// TestTheProjectFileWinsOverTheUserFile covers the order: a project's
// settings shadow the user's.
func TestTheProjectFileWinsOverTheUserFile(t *testing.T) {
	project := writeProject(t, t.TempDir(), `{"version":1}`)
	config := t.TempDir()
	user := filepath.Join(config, "poo", SettingsFileName)
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		t.Fatalf("writing the config directory: %v", err)
	}
	if err := os.WriteFile(user, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv(EnvSettings, "")

	found, err := FindSettings("", filepath.Dir(project))
	if err != nil {
		t.Fatalf("the discovery failed: %v", err)
	}
	if found != project {
		t.Errorf("the project file did not win: %q", found)
	}
	if !strings.HasSuffix(found, filepath.Join(".poo", SettingsFileName)) {
		t.Errorf("the winner is not a project settings file: %q", found)
	}
}
