package piimport

import (
	_ "embed"
	"fmt"
	"os"
)

// builtin.json is the built-in providers' data, in the shape a models
// file takes. A provider without discoverability is declared here
// rather than asked of its endpoint, so the file carries what the
// endpoint cannot enumerate: the models, their limits, and the levels
// each one takes. A model the data does not name is still selectable:
// the settings' models, or a models file, may name it under the same
// provider.
//
//go:embed builtin.json
var builtinData []byte

// Builtin returns the built-in providers, decoded through the same
// read a models file on disk takes.
//
// override names a file read instead of the embedded data, so a binary
// that ships without the newer list can serve the models a newer file
// declares. An empty name reads the data this package carries. The
// providers import with no keys: a built-in provider's credential is
// resolved by its transport, not an api key the settings store.
func Builtin(override string) (File, error) {
	if override != "" {
		data, err := os.ReadFile(override)
		if err != nil {
			return File{}, fmt.Errorf("reading %s: %w", override, err)
		}
		return decode(override, data, nil)
	}
	return decode("builtin.json", builtinData, nil)
}
