package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/provider/catalog"
)

// TestTheExamplesBuild covers the examples: every settings file the
// repository ships builds a runtime from the tool set, profiles and
// executors the command links, so an example stays runnable as the
// binary moves.
//
// An example whose role runs on a built-in provider resolves through
// the machine's credential or key, so a role the catalog cannot
// resolve under an endpoint the settings do not declare themselves is
// a skip and not a failure: the example is the shape to copy, and the
// key is the machine's to hold.
func TestTheExamplesBuild(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.settings.yaml"))
	if err != nil {
		t.Fatalf("listing the examples: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("the examples hold no settings files")
	}
	workDir := t.TempDir()
	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			settings, err := config.Load(file)
			if err != nil {
				t.Fatalf("loading the example: %v", err)
			}
			// A relative models_file resolves against the settings
			// file's own directory, so the load reads the example's
			// directory; the build works in a scratch one, the way a
			// run in a fresh checkout would.
			if _, err := config.Build(config.BuildOptions{
				Settings:       settings,
				WorkingDir:     workDir,
				DefaultProfile: defaultProfile,
				UserPromptFile: defaultUserPromptFile(),
				Tools:          linkedTools(),
				Profiles:       linkedProfiles(),
				Executors:      executorRegistry(),
			}); err != nil {
				// A built-in provider's endpoint arrives through the
				// built-in layer, and the layer's key-read endpoints
				// are offered only when the key is there. An unresolved
				// model under a name the settings do not declare
				// themselves is that gate, and the gate is the
				// machine's.
				var unresolved *catalog.UnresolvedError
				if errors.As(err, &unresolved) {
					if _, declared := settings.Endpoints[endpointOf(unresolved.Name)]; !declared {
						t.Skipf("the built-in provider's key is not here: %v", err)
					}
				}
				t.Fatalf("building the runtime: %v", err)
			}
		})
	}
}

// endpointOf returns the endpoint part of a model's name: the part
// before the slash, or the whole name when it carries none.
func endpointOf(name string) string {
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return name
}
