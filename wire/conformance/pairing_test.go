package conformance

import (
	"testing"

	"github.com/sdougbrown/poo/config"
)

// TestTheSettingsTurnOffEveryDefaultManifestKind pairs the suite's own
// manifest kinds with the ones the config package loads from the
// default directories. A kind config adds without a switch here fails,
// and a switch here that names nothing config loads fails with it: the
// two lists may not drift apart.
func TestTheSettingsTurnOffEveryDefaultManifestKind(t *testing.T) {
	document := map[string]any{}
	turnManifestDefaultsOff(document)
	if len(document) == 0 {
		t.Fatal("the suite turned no manifest defaults off")
	}
	known := map[string]bool{}
	for _, m := range config.DefaultManifests() {
		known[m.SettingsKey] = true
		block, ok := document[m.SettingsKey].(map[string]any)
		if !ok {
			t.Errorf("no defaults-off switch for the %q kind", m.SettingsKey)
			continue
		}
		if block["defaults"] != false {
			t.Errorf("the %q kind's switch is not off", m.SettingsKey)
		}
	}
	for name := range document {
		if !known[name] {
			t.Errorf("the suite turns off %q, and config loads no such default kind", name)
		}
	}
}
