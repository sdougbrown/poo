package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestTheFieldsHoldTheLeavesTheSettingsName covers a list leaf of the
// fields the generated check reads: profiles_files is filled when the
// settings name files, and is absent when they name none.
func TestTheFieldsHoldTheLeavesTheSettingsName(t *testing.T) {
	dir := t.TempDir()
	Chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "profiles.yaml"), []byte("profiles:\n  reviewer:\n    base: code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	named, err := Parse([]byte("version: 1\nprofiles_files: [profiles.yaml]\n"), FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	fields := fieldsOf(named)
	if got, want := fields.ProfilesFiles, []string{"profiles.yaml"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the fields hold %v, want %v", got, want)
	}

	absent, err := Parse([]byte("version: 1\n"), FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	if got := fieldsOf(absent).ProfilesFiles; got != nil {
		t.Errorf("the fields hold %v, want no profiles files: the settings named none", got)
	}
}

// TestTheFieldsKeepAnEmptyChainApart covers the fields the generated
// check reads: a chain the settings name with no askers is present and
// empty, and a chain they do not name is absent.
func TestTheFieldsKeepAnEmptyChainApart(t *testing.T) {
	empty, err := Parse([]byte("version: 1\npermission:\n  askers: []\n"), FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	fields := fieldsOf(empty)
	if fields.PermissionAskers == nil {
		t.Error("the fields hold no asker chain, want an empty one the settings named")
	}
	if len(fields.PermissionAskers) != 0 {
		t.Errorf("the fields hold %v, want no askers", fields.PermissionAskers)
	}

	absent, err := Parse([]byte("version: 1\n"), FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	if got := fieldsOf(absent).PermissionAskers; got != nil {
		t.Errorf("the fields hold %v, want no chain: the settings named none", got)
	}
}
