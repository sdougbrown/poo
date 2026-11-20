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

// endpointKey resolves the key an endpoint reads: the environment variable
// the endpoint names, then the credential poo's file holds for the provider
// that reads the same variable, then the literal the settings carry for an
// endpoint of the same name. The catalog holds no secret, so the literal is
// read where it is written.
//
// The order is the operator's intent, most explicit first: a variable set in
// the environment is the answer the process was told to use, and a stored
// credential must not override it silently. The operator's own sign-in
// outranks a literal that was written before it.
func (s *Settings) EndpointKey(name string, credential catalog.Credential) string {
	if credential.Source == catalog.CredentialEnv {
		if key := os.Getenv(credential.Env); key != "" {
			return key
		}
		// The environment variable's name is the one fact an endpoint's declaration and a
		// provider's declaration both carry, so it is the join: an endpoint that reads the
		// variable a provider stores its key under is served by that provider's credential,
		// and no endpoint name is hardcoded here.
		for _, provider := range auth.KeyProviders() {
			if credential.Env != provider.Env {
				continue
			}
			// An absent or unreadable credential is not a failure here: the next rung answers.
			stored, err := (auth.Credentials{Path: s.CredentialFile()}).Read(provider)
			if err == nil && stored.Key != "" {
				return stored.Key
			}
		}
	}
	if named, ok := s.Endpoints[name]; ok {
		return named.APIKey
	}
	return ""
}

// EndpointKeyPresent reports whether the key an endpoint reads is in reach,
// and an endpoint that reads none is served as it stands. It is the presence
// answer to endpointKey's resolution: a caller that filters models by what an
// operator can run asks what a session would find, and finds it without
// holding any secret.
func (s *Settings) EndpointKeyPresent(name string, credential catalog.Credential) bool {
	if credential.Source == catalog.CredentialNone {
		return true
	}
	return s.EndpointKey(name, credential) != ""
}

// gateKeyEndpoints drops the layer's endpoints whose credential is not
// there, and keeps every other endpoint as it is.
//
// An endpoint that names a key variable takes the key through the same
// rungs the key's own resolution reads, in the same order: the variable it
// names, the credential file's entry for the provider that reads it, and
// the literal the settings carry for an endpoint of the same name. A read
// of the file that fails is a file that holds nothing: the variable and
// the literal are the other ways in. An endpoint that names no variable
// reads the credential poo's file holds under its own name.
func (s *Settings) gateKeyEndpoints(layer catalog.FileSettings) catalog.FileSettings {
	for name, endpoint := range layer.Endpoints {
		if endpoint.APIKeyEnv == "" {
			// An endpoint that names no key variable reads the credential
			// poo's file holds under its own name: the Codex transport's
			// oauth pair. A file that holds none leaves the endpoint
			// unoffered.
			held, err := (auth.Credentials{Path: s.CredentialFile()}).Holds(name)
			if err == nil && held {
				continue
			}
			delete(layer.Endpoints, name)
			for model, entry := range layer.Models {
				if entry.Endpoint == name {
					delete(layer.Models, model)
				}
			}
			continue
		}
		credential := catalog.Credential{Source: catalog.CredentialEnv, Env: endpoint.APIKeyEnv}
		if s.EndpointKeyPresent(name, credential) {
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
