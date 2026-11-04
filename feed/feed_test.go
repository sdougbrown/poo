package feed_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/feed"
)

// writeManifest writes a manifest with the given feeds and returns its
// directory.
func writeManifest(t *testing.T, feeds map[string]any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"version": 1, "feeds": feeds})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "feeds.json"), body, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return dir
}

// theBrokerFeeds are two feeds a set holds: the names are chosen so the
// name order is not the file order.
func theBrokerFeeds() map[string]any {
	return map[string]any{
		"avenor-broker": map[string]any{
			"description": "Messages from other agents on the avenor broker.",
			"command":     []string{"avenor", "feed", "broker"},
			"env":         map[string]any{"AVENOR_BROKER_TOKEN": "env:AVENOR_BROKER_TOKEN", "AVENOR_BROKER_URL": "https://avenor.example"},
			"restart":     true,
			"on_idle":     "wake",
		},
		"ci-watch": map[string]any{
			"description": "Messages from the CI watcher.",
			"command":     []string{"ci-watch", "--poll"},
			"on_idle":     "hold",
		},
	}
}

// TestALoadedSetHoldsItsSpecsInNameOrder covers the load: every manifest
// the settings name is loaded, and the specs come back in name order.
func TestALoadedSetHoldsItsSpecsInNameOrder(t *testing.T) {
	set, err := feed.Load(writeManifest(t, theBrokerFeeds()))
	if err != nil {
		t.Fatalf("loading the manifests: %v", err)
	}
	specs := set.Specs()
	if len(specs) != 2 {
		t.Fatalf("the set holds %d specs, want 2", len(specs))
	}
	if specs[0].Name != "avenor-broker" || specs[1].Name != "ci-watch" {
		t.Errorf("the specs are %q and %q, want name order", specs[0].Name, specs[1].Name)
	}
	if set.Len() != 2 {
		t.Errorf("Len is %d, want 2", set.Len())
	}
	broker := specs[0]
	if broker.Description != "Messages from other agents on the avenor broker." {
		t.Errorf("the description is %q", broker.Description)
	}
	if strings.Join(broker.Command, " ") != "avenor feed broker" {
		t.Errorf("the command is %v", broker.Command)
	}
	if !broker.Restart || broker.OnIdle != "wake" {
		t.Errorf("the manifest's restart and on_idle did not hold: %+v", broker)
	}
	if len(broker.Env) != 2 || broker.Env[0].Name != "AVENOR_BROKER_TOKEN" || broker.Env[1].Name != "AVENOR_BROKER_URL" {
		t.Errorf("the env bindings are %+v, want name order", broker.Env)
	}
	if !broker.Env[0].Reference || broker.Env[0].From != "AVENOR_BROKER_TOKEN" {
		t.Errorf("the token binding is %+v, want an env: reference", broker.Env[0])
	}
	if broker.Env[1].Reference || broker.Env[1].Value != "https://avenor.example" {
		t.Errorf("the url binding is %+v, want a literal", broker.Env[1])
	}
}

// TestAnEmptySetIsNilSafe covers the reads: a set the settings do not
// name is nil, and reading it holds nothing.
func TestAnEmptySetIsNilSafe(t *testing.T) {
	var set *feed.Set
	if set.Len() != 0 || set.Specs() != nil {
		t.Errorf("the nil set reads as %d specs %v", set.Len(), set.Specs())
	}
}

// TestSpecsAreACopy covers the read: a caller may reorder the list
// without touching the set.
func TestSpecsAreACopy(t *testing.T) {
	set, err := feed.Load(writeManifest(t, theBrokerFeeds()))
	if err != nil {
		t.Fatalf("loading the manifests: %v", err)
	}
	specs := set.Specs()
	specs[0], specs[1] = specs[1], specs[0]
	if set.Specs()[0].Name != "avenor-broker" {
		t.Error("reordering a Specs list reordered the set")
	}
}

// TestResolveEnvCoversLiteralsReferencesAndBenignNames covers the
// resolution: the benign names come from the lookup, the manifest's
// literals pass through, and its references resolve against it.
func TestResolveEnvCoversLiteralsReferencesAndBenignNames(t *testing.T) {
	set, err := feed.Load(writeManifest(t, theBrokerFeeds()))
	if err != nil {
		t.Fatalf("loading the manifests: %v", err)
	}
	lookup := func(name string) (string, bool) {
		switch name {
		case "PATH", "AVENOR_BROKER_TOKEN":
			return "value-of-" + strings.ToLower(name), true
		}
		return "", false
	}
	env, err := set.Specs()[0].ResolveEnv(lookup)
	if err != nil {
		t.Fatalf("resolving the env: %v", err)
	}
	want := []string{
		"PATH=value-of-path",
		"AVENOR_BROKER_TOKEN=value-of-avenor_broker_token",
		"AVENOR_BROKER_URL=https://avenor.example",
	}
	if strings.Join(env, "\n") != strings.Join(want, "\n") {
		t.Errorf("the env is %v, want %v", env, want)
	}
}

// TestAMissingReferenceNamesTheVariable covers the gate: a manifest
// reference the lookup cannot resolve fails, and the failure names the
// variable.
func TestAMissingReferenceNamesTheVariable(t *testing.T) {
	set, err := feed.Load(writeManifest(t, theBrokerFeeds()))
	if err != nil {
		t.Fatalf("loading the manifests: %v", err)
	}
	_, err = set.Specs()[0].ResolveEnv(func(string) (string, bool) { return "", false })
	if err == nil {
		t.Fatal("a missing reference resolved")
	}
	if !strings.Contains(err.Error(), "AVENOR_BROKER_TOKEN") {
		t.Errorf("the failure %q does not name the variable", err)
	}
}

// TestALoadRefusesBrokenManifests covers the validation: a manifest that
// does not hold fails the load, and the failure names the file and the
// feed.
func TestALoadRefusesBrokenManifests(t *testing.T) {
	tests := []struct {
		name   string
		feeds  map[string]any
		ignore map[string]bool
		want   []string
	}{
		{
			name: "unknown field on the feed",
			feeds: map[string]any{
				"broker": map[string]any{"command": []string{"true"}, "restart_at": 5},
			},
			want: []string{"unknown field", "restart_at"},
		},
		{
			name: "invalid on_idle",
			feeds: map[string]any{
				"broker": map[string]any{"command": []string{"true"}, "on_idle": "sleep"},
			},
			want: []string{"on_idle", `"sleep"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeManifest(t, tt.feeds)
			path := filepath.Join(dir, "feeds.json")
			_, err := feed.Load(path)
			if err == nil {
				t.Fatal("the load accepted a broken manifest")
			}
			if !strings.Contains(err.Error(), "feeds.json") {
				t.Errorf("the failure %q does not name the file", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the failure %q does not carry %q", err, want)
				}
			}
		})
	}
}

// TestALoadRefusesAnUnknownTopLevelField covers the envelope: a field
// the loader does not know at the top of the manifest fails the load.
func TestALoadRefusesAnUnknownTopLevelField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feeds.json")
	body := `{"version": 1, "feeds": {}, "hooks": {}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	_, err := feed.Load(path)
	if err == nil {
		t.Fatal("the load accepted an unknown top-level field")
	}
	if !strings.Contains(err.Error(), "hooks") || !strings.Contains(err.Error(), "feeds.json") {
		t.Errorf("the failure %q does not name the field and the file", err)
	}
}

// TestALoadRefusesAnotherVersion covers the version: a manifest written
// for another loader fails, and the failure names the file.
func TestALoadRefusesAnotherVersion(t *testing.T) {
	dir := t.TempDir()
	body, err := json.Marshal(map[string]any{"version": 2, "feeds": map[string]any{}})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(dir, "feeds.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	_, err = feed.Load(path)
	if err == nil {
		t.Fatal("the load accepted another version")
	}
	if !strings.Contains(err.Error(), "version 2") || !strings.Contains(err.Error(), "feeds.json") {
		t.Errorf("the failure %q does not name the file and the version", err)
	}
}

// TestANameDefinedTwiceNamesBothFiles covers the duplicate: a feed name
// two manifests define fails the load, and the failure names both files.
func TestANameDefinedTwiceNamesBothFiles(t *testing.T) {
	feeds := map[string]any{
		"broker": map[string]any{"command": []string{"true"}},
	}
	one, two := writeManifest(t, feeds), writeManifest(t, feeds)
	_, err := feed.Load(one, two)
	if err == nil {
		t.Fatal("the load accepted a name two manifests define")
	}
	if !strings.Contains(err.Error(), filepath.Base(one)) || !strings.Contains(err.Error(), filepath.Base(two)) {
		t.Errorf("the failure %q does not name both files", err)
	}
}

// TestAMissingPathNamesTheFile covers the discovery: a path that is not
// there fails the load, and the failure names it.
func TestAMissingPathNamesTheFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "feeds.json")
	_, err := feed.Load(missing)
	if err == nil {
		t.Fatal("the load accepted a path that is not there")
	}
	if !strings.Contains(err.Error(), "feeds.json") {
		t.Errorf("the failure %q does not name the file", err)
	}
}

// TestADirectoryHoldsItsManifestFiles covers the directory form: a
// directory's *.json, *.yaml and *.toml files are the manifests, and the
// other files are not.
func TestADirectoryHoldsItsManifestFiles(t *testing.T) {
	dir := writeManifest(t, map[string]any{
		"broker": map[string]any{"command": []string{"true"}},
	})
	yaml := "version: 1\nfeeds:\n  ci-watch:\n    command: [ci-watch, --poll]\n"
	if err := os.WriteFile(filepath.Join(dir, "more.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("writing the yaml manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a manifest"), 0o600); err != nil {
		t.Fatalf("writing the notes: %v", err)
	}
	set, err := feed.Load(dir)
	if err != nil {
		t.Fatalf("loading the directory: %v", err)
	}
	if set.Len() != 2 || set.Specs()[0].Name != "broker" || set.Specs()[1].Name != "ci-watch" {
		t.Errorf("the directory loaded %+v, want the two feeds", set.Specs())
	}
}
