package config

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/sdougbrown/poo/provider/catalog"
)

// builtin-models.yaml is the built-in providers' record: the models the
// upstream endpoints do not list, declared in poo's own models-file
// vocabulary. It is read through the same path poo's own models file takes,
// so one vocabulary and one decode serve both.
//
//go:embed builtin-models.yaml
var builtinModelsData []byte

// builtinModelsName is what the record's errors name when the data is the
// one this package carries.
const builtinModelsName = "builtin-models.yaml"

// applyBuiltinProviders adds the layer the built-in providers declare.
//
// The providers are offered when a credential file is there, because they
// are what its credential can run. A settings file that names no credential
// file, and has none at the default path, gains no models it did not ask
// for. They are the layer's fallback: the models file is the operator's word
// and overrules them by name.
//
// A replacement the environment names is read wherever it is named, gate or
// not: an explicit name is a promise, and a file that does not read fails
// the load.
func (s *Settings) applyBuiltinProviders(override string) error {
	offered := s.credentialPath != "" && fileExists(s.credentialPath)
	if override == "" && !offered {
		return nil
	}
	layer, err := builtinLayer(override)
	if err != nil {
		return &Error{Code: CodeInvalid, Message: "the built-in providers do not read", Err: err}
	}
	if offered {
		s.addLayer(layer)
	}
	return nil
}

// builtinLayer reads the built-in providers' record — or the file an
// override names — and returns the layer it declares.
func builtinLayer(override string) (catalog.FileSettings, error) {
	source := builtinModelsName
	data := builtinModelsData
	if override != "" {
		source = override
		var err error
		data, err = os.ReadFile(override)
		if err != nil {
			return catalog.FileSettings{}, fmt.Errorf("reading %s: %w", override, err)
		}
	}
	document, err := normalize(stripDocumentBOM(data), FormatYAML, "the built-in providers")
	if err != nil {
		return catalog.FileSettings{}, fmt.Errorf("%s: %w", source, err)
	}
	layer, err := modelsLayer(source, document)
	if err != nil {
		return catalog.FileSettings{}, fmt.Errorf("%s: %w", source, err)
	}
	return layer, nil
}
