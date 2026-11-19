package config

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/sdougbrown/poo/provider/auth"
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
// A layer endpoint that names a key's variable is held to a sharper rule:
// it is offered when that key is there — the variable, or the credential
// file's entry for it. The file is one gate for every provider it may hold,
// so a file holding another provider's credential does not wake an endpoint
// this key cannot reach.
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
		s.addLayer(s.gateKeyEndpoints(layer))
	}
	return nil
}

// gateKeyEndpoints drops the layer's key-read endpoints whose key is not
// there, and keeps every other endpoint as it is.
//
// The key is there through the same rungs the key's own resolution reads, in
// the same order: the variable it names, the credential file's entry for the
// provider that reads it, and the literal the settings carry for an endpoint
// of the same name. A read of the file that fails is a file that holds
// nothing: the variable and the literal are the other ways in.
func (s *Settings) gateKeyEndpoints(layer catalog.FileSettings) catalog.FileSettings {
	for name, endpoint := range layer.Endpoints {
		if endpoint.APIKeyEnv == "" {
			continue
		}
		if os.Getenv(endpoint.APIKeyEnv) != "" || s.credentialHolds(endpoint.APIKeyEnv) {
			continue
		}
		if named, ok := s.Endpoints[name]; ok && named.APIKey != "" {
			continue
		}
		delete(layer.Endpoints, name)
		for model, entry := range layer.Models {
			if entry.Endpoint == name {
				delete(layer.Models, model)
			}
		}
	}
	return layer
}

// credentialHolds reports whether the credential file holds a key entry for
// the provider that reads the named environment variable.
func (s *Settings) credentialHolds(env string) bool {
	for _, provider := range auth.KeyProviders() {
		if provider.Env != env {
			continue
		}
		held, err := (auth.Credentials{Path: s.credentialPath}).Holds(provider.ID)
		return err == nil && held
	}
	return false
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
