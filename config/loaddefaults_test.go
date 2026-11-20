package config_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/provider/auth"
	"github.com/sdougbrown/poo/provider/catalog"
)

// TestLoadDefaultsLoadsWhatAWrittenFileLoads covers the parity the
// defaults promise: the settings a run has when discovery finds no file
// build the catalog a written settings file in the same directory
// builds, so the welcome front door's picker and every command's report
// see one catalog on one machine.
func TestLoadDefaultsLoadsWhatAWrittenFileLoads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	// A key in the environment is another way in, and neither side of the
	// parity reads one: the credential file alone is the fixture.
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("POO_SETTINGS", "")

	dir, err := config.UserConfigDir()
	if err != nil {
		t.Skipf("the user config directory is not resolvable here: %v", err)
	}
	authPath, err := config.DefaultAuthFile()
	if err != nil {
		t.Skipf("the default credential file is not resolvable here: %v", err)
	}
	credentials := auth.Credentials{Path: authPath}
	if _, err := credentials.Set(context.Background(), auth.Opencode, auth.Credential{Kind: auth.KindAPIKey, Key: "test-key"}, true); err != nil {
		t.Fatalf("writing the credential fixture: %v", err)
	}
	path := filepath.Join(dir, config.SettingsFileName)
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatalf("writing the settings fixture: %v", err)
	}

	defaults, err := config.LoadDefaults()
	if err != nil {
		t.Fatalf("the defaults do not load: %v", err)
	}
	written, err := config.Load(path)
	if err != nil {
		t.Fatalf("the written settings do not load: %v", err)
	}

	defaultsModels := catalogModels(defaults)
	writtenModels := catalogModels(written)
	// The credential file wakes the endpoints the key can reach, and the
	// two sides read the same file, so one list of models is the parity.
	if !slices.Equal(defaultsModels, writtenModels) {
		t.Errorf("the defaults build %v, and the written file builds %v", defaultsModels, writtenModels)
	}
	if len(defaultsModels) == 0 {
		t.Errorf("the defaults build no models, want the ones the credential file wakes")
	}
}

// catalogModels returns the model names a settings' catalog holds.
func catalogModels(settings *config.Settings) []string {
	cat := catalog.New(settings.CatalogOptions())
	names := make([]string, 0, len(cat.List()))
	for _, info := range cat.List() {
		names = append(names, info.Name)
	}
	slices.Sort(names)
	return names
}
