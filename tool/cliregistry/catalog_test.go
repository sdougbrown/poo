package cliregistry_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo/tool/cliregistry"
)

// TestTheCatalogHoldsBothCLIs covers the load: the manifest's tools are
// the catalog's entries, in name order, with what the prompt and the
// model need.
func TestTheCatalogHoldsBothCLIs(t *testing.T) {
	catalog := fixtureCatalog(t)
	if catalog.Len() != 2 {
		t.Fatalf("the catalog holds %d entries, want 2", catalog.Len())
	}
	names := catalog.Names()
	if len(names) != 2 || names[0] != "list" || names[1] != "say" {
		t.Errorf("the catalog names %v, want [list say]", names)
	}
	entry, ok := catalog.Lookup("say")
	if !ok {
		t.Fatal("the catalog does not know the say CLI")
	}
	if entry.Description != "Say hello to a name" {
		t.Errorf("the say entry says %q", entry.Description)
	}
	if entry.Schema == nil {
		t.Error("the say entry has no argument schema")
	}
	if _, ok := catalog.Lookup("nope"); ok {
		t.Error("the catalog knows a CLI it was not given")
	}
}

// TestADuplicateNameFailsTheLoad covers the one name a model calls: two
// manifests that define it make the load fail, and the failure says
// which files disagree.
func TestADuplicateNameFailsTheLoad(t *testing.T) {
	dir := t.TempDir()
	first := writeManifest(t, dir, "first.json", map[string]any{"say": sayTool(t)})
	second := writeManifest(t, dir, "second.json", map[string]any{"say": sayTool(t)})

	catalog, err := cliregistry.Load(first, second)
	if err == nil {
		t.Fatal("the load accepted a duplicate name")
	}
	if catalog != nil {
		t.Error("a failed load returned a catalog")
	}
	for _, path := range []string{first, second} {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("the error %q does not name %q", err, path)
		}
	}
}

// TestADuplicateNameInsideADirectoryFailsTheLoad covers the same rule
// when the paths are directories rather than files.
func TestADuplicateNameInsideADirectoryFailsTheLoad(t *testing.T) {
	one := t.TempDir()
	two := t.TempDir()
	writeManifest(t, one, "one.json", map[string]any{"say": sayTool(t)})
	writeManifest(t, two, "two.json", map[string]any{"say": sayTool(t)})

	_, err := cliregistry.Load(one, two)
	if err == nil {
		t.Fatal("the load accepted a duplicate name across directories")
	}
	if !strings.Contains(err.Error(), "first") && !strings.Contains(err.Error(), "one.json") {
		t.Errorf("the error %q does not name the first file", err)
	}
}

// TestTheCatalogSectionListsTheEntries covers the prompt section: one
// line per entry, `name: description`, in the same order every time.
func TestTheCatalogSectionListsTheEntries(t *testing.T) {
	catalog := fixtureCatalog(t)
	section := catalog.Section(false)

	lines := strings.Split(section, "\n")
	var entries []string
	for _, line := range lines {
		if strings.HasPrefix(line, "list: ") || strings.HasPrefix(line, "say: ") {
			entries = append(entries, line)
		}
	}
	want := []string{
		"list: Print the words it is given, one per line",
		"say: Say hello to a name",
	}
	if len(entries) != len(want) {
		t.Fatalf("the section lists %v, want %v", entries, want)
	}
	for i, line := range entries {
		if line != want[i] {
			t.Errorf("the line %d is %q, want %q", i, line, want[i])
		}
	}
	if !strings.Contains(section, cliregistry.CallToolName) {
		t.Errorf("the section %q does not say how to call an entry", section)
	}
	// The same catalog renders the same section.
	if again := catalog.Section(false); again != section {
		t.Error("the section changed between two renders")
	}
}

// TestTheSearchedCatalogSectionKeepsTheEntriesOut covers the other
// shape: a catalog behind a search says so, and does not spend the
// prompt on the list the search replaces.
func TestTheSearchedCatalogSectionKeepsTheEntriesOut(t *testing.T) {
	catalog := fixtureCatalog(t)
	section := catalog.Section(true)

	if !strings.Contains(section, cliregistry.FindToolName) {
		t.Errorf("the section %q does not name the search", section)
	}
	for _, name := range catalog.Names() {
		if strings.Contains(section, name+":") {
			t.Errorf("the section %q lists the entry %q", section, name)
		}
	}
}
