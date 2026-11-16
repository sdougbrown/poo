package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config/internal/piimport"
	"github.com/sdougbrown/poo/provider/catalog"
	"github.com/sdougbrown/poo/provider/codex"
)

// ModelsFileName is the name poo's own models file is discovered by, beside a
// settings file.
const ModelsFileName = "models.yaml"

// PiModelsFileName is the name pi's models file is discovered by, beside a
// settings file. It is imported as the models file: poo reads it for a models
// file that predates its own name.
const PiModelsFileName = "models.json"

// providersField is the key pi's models file declares its providers under. A
// file that names it is pi's shape; a file that names the endpoints is poo's.
const providersField = "providers"

// AuthFileName is the name a credential file goes by: poo's own, in the user's
// config directory, and pi's beside pi's models file.
const AuthFileName = "auth.json"

// importPi imports what pi's files hold: the models file the settings name or
// the one beside them, and the auth.json the Codex provider reads. pi's
// built-in providers are offered when that auth file is there.
//
// The auth path is resolved here, where the settings load, because the models
// file's own directory is what a sibling auth.json answers to. A Settings built
// by hand has no resolved path, and CredentialFile falls back to the setting it
// names.
func (s *Settings) importPi(dir string) error {
	modelsPath, err := s.modelsFilePath(dir)
	if err != nil {
		return err
	}
	// The credential file is resolved before the models file is read, so one path
	// answers for every reader: the Codex credential the transport resolves, and
	// the api keys the models file's providers import.
	s.credentialPath, err = s.resolveCredentialFile(dir, modelsPath)
	if err != nil {
		return err
	}
	// pi's built-in providers are offered when a credential file is there,
	// because they are what its credential can run. A settings file that names
	// no credential file, and has none at the default path, gains no models it
	// did not ask for. They are the layer's fallback: the models file is the
	// operator's word and overrules them by name.
	if s.credentialPath != "" && fileExists(s.credentialPath) {
		s.addLayer(piLayer(piimport.BuiltinCodex(codex.DefaultEndpoint)))
	}
	if modelsPath != "" {
		if err := s.importModelsFile(modelsPath); err != nil {
			return err
		}
	}
	return nil
}

// modelsFilePath returns the models file the settings name, or the one beside
// them, and whether there is one.
//
// A named file wins over a discovered one, and a relative name resolves
// against dir — the settings file's own directory — rather than the working
// directory. dir is empty for a document held in memory, which is what says
// there is nothing to discover beside it.
func (s *Settings) modelsFilePath(dir string) (string, error) {
	path := s.ModelsFile
	switch {
	case path != "":
		expanded, err := expandHome(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(expanded) && dir != "" {
			expanded = filepath.Join(dir, expanded)
		}
		return expanded, nil
	case dir != "":
		own := filepath.Join(dir, ModelsFileName)
		pi := filepath.Join(dir, PiModelsFileName)
		switch {
		case fileExists(own) && fileExists(pi):
			// Both names are read as the models file, so two files beside one
			// settings are one file too many. Refusing says so; picking one
			// would leave the other read by nobody.
			return "", &Error{
				Code:    CodeInvalid,
				Message: fmt.Sprintf("the settings have both a %s and a %s beside them: delete the one poo should not read", ModelsFileName, PiModelsFileName),
			}
		case fileExists(own):
			return own, nil
		case fileExists(pi):
			return pi, nil
		}
		return "", nil
	default:
		return "", nil
	}
}

// importModelsFile reads a models file: poo's own shape, or pi's, which is
// translated into the same layer and whose providers' keys are read from the
// credential file the settings resolved.
func (s *Settings) importModelsFile(path string) error {
	document, err := readModelsDocument(path)
	if err != nil {
		return s.modelsFileError(err)
	}
	shaped, err := modelsFileShape(path, document)
	if err != nil {
		return s.modelsFileError(err)
	}
	if shaped == shapePi {
		file, err := piimport.Read(path, s.credentialPath)
		if err != nil {
			return s.modelsFileError(err)
		}
		s.addLayer(piLayer(file))
		s.importPiKeys(file)
		return nil
	}
	layer, err := modelsLayer(path, document)
	if err != nil {
		return s.modelsFileError(err)
	}
	s.modelsFileLayer = layer
	return nil
}

// modelsFileError names what is at fault: the file beside the settings when no
// setting named one, and the setting otherwise.
//
// The reader names the file it read; the setting that named it is what a caller
// branches on.
func (s *Settings) modelsFileError(err error) error {
	if s.ModelsFile == "" {
		return &Error{
			Code:    CodeInvalid,
			Message: "the models file beside the settings does not read",
			Err:     err,
		}
	}
	return &Error{Code: CodeInvalid, Field: "models_file", Err: err}
}

// shape is the shape a models file is written with.
type shape int

const (
	// shapePoo is poo's own models file: endpoints, each with its models.
	shapePoo shape = iota
	// shapePi is pi's models file: providers, each with its models.
	shapePi
)

// modelsFileShape returns the shape a models file is written with.
//
// The file says which one it is: pi's declares its providers, and poo's
// declares its endpoints. A file that neither declares is read as poo's, which
// is the shape that says what the models are.
func modelsFileShape(path string, document []byte) (shape, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(document, &top); err != nil {
		return shapePoo, fmt.Errorf("%s: %w", path, err)
	}
	if _, ok := top[providersField]; ok {
		return shapePi, nil
	}
	return shapePoo, nil
}

// readModelsDocument reads a models file and returns it as JSON.
//
// One codec over two encodings: the file is YAML, which is what an operator edits,
// and JSON is YAML that an operator may also write. The document normalizes through
// the pipeline the settings use, so a change to how a document is read reaches both.
func readModelsDocument(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	// YAML reads JSON too, so one format answers for both spellings of the file.
	document, err := normalize(stripDocumentBOM(data), FormatYAML, "the models file")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return document, nil
}

// stripDocumentBOM drops a leading UTF-8 byte order mark, the character an
// editor on Windows writes.
func stripDocumentBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte("\ufeff"))
}

// resolveCredentialFile returns the file the Codex credential is read from, and
// that poo writes it to.
//
// The operator's own words come first, then the arrangement pi's files already
// have, then poo's own file:
//
//  1. auth_file names poo's file. A relative path answers to the settings file's
//     directory, like models_file. A file that is not there yet is not a
//     failure: it is the file a login creates.
//  2. pi_auth_file names pi's file. A relative path answers the same way, and a
//     file that is not there is a settings failure, because an explicit path is
//     a promise.
//  3. An auth.json beside the models file is what pi writes beside its own models,
//     so a pi setup keeps working without naming the file. The path is answered
//     whether or not the file is there yet: a long-lived process that resolved it
//     before pi's first login must still find the credential pi wrote after.
//  4. poo's own file in the user's config directory, when there is no models file
//     for a credential to sit beside. It is where poo creates one of its own.
//
// Naming both files is refused: poo reads one credential at a time, and two
// settings naming one apiece is a question the file cannot answer.
func (s *Settings) resolveCredentialFile(dir, modelsPath string) (string, error) {
	if s.AuthFile != "" && s.PiAuthFile != "" {
		return "", &Error{
			Code:    CodeInvalid,
			Field:   "auth_file",
			Message: "auth_file and pi_auth_file both name a credential file, and poo reads one",
		}
	}
	if s.AuthFile != "" {
		return s.resolveCredentialPath(dir, "auth_file", s.AuthFile, false)
	}
	if s.PiAuthFile != "" {
		return s.resolveCredentialPath(dir, "pi_auth_file", s.PiAuthFile, true)
	}
	if modelsPath != "" {
		return filepath.Join(filepath.Dir(modelsPath), piimport.AuthFileName), nil
	}
	return defaultCredentialFile(), nil
}

// resolveCredentialPath resolves the path a setting names: ~ first, then the
// settings file's own directory for a relative one. mustExist is for the
// setting that promised a file there.
func (s *Settings) resolveCredentialPath(dir, field, named string, mustExist bool) (string, error) {
	path, err := expandHome(named)
	if err != nil {
		return "", &Error{Code: CodeInvalid, Field: field, Message: "the credential file cannot be resolved", Err: err}
	}
	if !filepath.IsAbs(path) && dir != "" {
		path = filepath.Join(dir, path)
	}
	if mustExist {
		if _, err := os.Stat(path); err != nil {
			return "", &Error{Code: CodeInvalid, Field: field, Message: "the file is not there", Err: err}
		}
	}
	return path, nil
}

// defaultCredentialFile is poo's own credential file in the user's config
// directory.
//
// A machine whose config directory cannot be resolved has no default file, and
// the Codex provider reports the credential it could not find. The settings are
// not a failure over a file only one provider reads.
func defaultCredentialFile() string {
	dir, err := UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, AuthFileName)
}

// CredentialFile returns the file the Codex credential is read from: the one
// auth_file or pi_auth_file named, else the auth.json beside the models file,
// else poo's own file in the user's config directory.
//
// A Settings that was loaded has the path resolved. One built by hand names it,
// and the path answers to the working directory the way every other hand-built
// path does, with the default file as the answer when it names none.
func (s *Settings) CredentialFile() string {
	if s.credentialPath != "" {
		return s.credentialPath
	}
	if s.AuthFile != "" {
		return s.expandSetting(s.AuthFile)
	}
	if s.PiAuthFile != "" {
		return s.expandSetting(s.PiAuthFile)
	}
	return defaultCredentialFile()
}

// expandSetting resolves the leading ~ of a path a hand-built settings names.
//
// The other rules a loaded settings resolves — the settings file's directory,
// and the default file — need a file the settings were loaded from, and a
// Settings built by hand has none.
func (s *Settings) expandSetting(path string) string {
	expanded, err := expandHome(path)
	if err != nil {
		return path
	}
	return expanded
}

// modelsFile is poo's own models file: the endpoints it declares, and the
// models each of them serves.
//
// The vocabulary is the settings' own, so nothing is expressible only here. The
// document is decoded strictly: a key this file does not have is a failure that
// names it, rather than a setting with no effect.
type modelsFile struct {
	// Endpoints are the endpoints the file declares, by name.
	Endpoints map[string]modelsEndpoint `json:"endpoints"`
}

// modelsEndpoint is one endpoint a models file declares: where it is, how it is
// reached, and the models it serves.
type modelsEndpoint struct {
	// Endpoint is the endpoint's base URL.
	Endpoint string `json:"endpoint"`
	// API is the wire API the endpoint speaks, as poo names it. Empty is chat
	// completions.
	API string `json:"api,omitempty"`
	// APIKeyEnv names the environment variable that holds the key.
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// APIKey is a key written in the file. It is refused: a models file is one
	// an operator shares or checks in.
	APIKey *string `json:"api_key,omitempty"`
	// SkipProbe says the endpoint's models are declared rather than discovered,
	// so no probe is sent to it.
	SkipProbe bool `json:"skip_probe,omitempty"`
	// Models are the models the endpoint serves, by the id it calls them.
	Models map[string]modelsModel `json:"models"`
}

// modelsModel is one model an endpoint serves.
type modelsModel struct {
	// ModelID is what the endpoint calls the model. Empty takes the key the
	// model is declared under.
	ModelID string `json:"model_id,omitempty"`
	// Inputs are the modalities the model reads.
	Inputs []string `json:"inputs,omitempty"`
	// ContextLimit is the model's context window.
	ContextLimit int `json:"context_limit,omitempty"`
	// MaxOutput is the most tokens the model writes in one response.
	MaxOutput int `json:"max_output,omitempty"`
	// Reasoning is how the model's reasoning is controlled.
	Reasoning *poo.ReasoningControl `json:"reasoning,omitempty"`
	// ReasoningLevels are the reasoning levels the model accepts, lowest first.
	ReasoningLevels []string `json:"reasoning_levels,omitempty"`
	// ToolsMode is how the run asks for tool calls.
	ToolsMode poo.ToolsMode `json:"tools_mode,omitempty"`
	// Quirks are the transport's named adaptations this model needs.
	Quirks []string `json:"quirks,omitempty"`
	// Sampling are the model's own sampling defaults.
	Sampling poo.Sampling `json:"sampling,omitempty"`
}

// modelsLayer reads poo's own models file and returns the layer it declares.
func modelsLayer(path string, document []byte) (catalog.FileSettings, error) {
	var file modelsFile
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return catalog.FileSettings{}, fmt.Errorf("%s: %w", path, err)
	}
	return file.layer()
}

// layer turns the file into the catalog layer it declares: the endpoints, and
// each model under the name provider/id.
//
// A file that declares nothing poo can run is refused rather than read as an
// empty layer: a models file that imports halfway is worse than one that says
// what is wrong with it.
func (f modelsFile) layer() (catalog.FileSettings, error) {
	if len(f.Endpoints) == 0 {
		return catalog.FileSettings{}, fmt.Errorf("the file declares no endpoints")
	}
	out := catalog.FileSettings{
		Endpoints: make(map[string]catalog.EndpointSettings, len(f.Endpoints)),
		Models:    map[string]catalog.ModelSettings{},
	}
	names := make([]string, 0, len(f.Endpoints))
	for name := range f.Endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		endpoint := f.Endpoints[name]
		if endpoint.Endpoint == "" {
			return catalog.FileSettings{}, fmt.Errorf("the endpoint %s declares no endpoint", name)
		}
		if endpoint.APIKey != nil {
			return catalog.FileSettings{}, fmt.Errorf(
				"the endpoint %s writes a literal api_key, and a models file is one an operator shares: name api_key_env, or a credential in the auth file",
				name)
		}
		if len(endpoint.Models) == 0 {
			return catalog.FileSettings{}, fmt.Errorf("the endpoint %s serves no models", name)
		}
		out.Endpoints[name] = catalog.EndpointSettings{
			Endpoint:  endpoint.Endpoint,
			APIKeyEnv: endpoint.APIKeyEnv,
			SkipProbe: endpoint.SkipProbe,
		}
		ids := make([]string, 0, len(endpoint.Models))
		for id := range endpoint.Models {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			model := endpoint.Models[id]
			modelID := model.ModelID
			if modelID == "" {
				modelID = id
			}
			inputs, err := modalitiesNamed(name, modelID, model.Inputs)
			if err != nil {
				return catalog.FileSettings{}, err
			}
			out.Models[name+"/"+id] = catalog.ModelSettings{
				Endpoint:        name,
				ModelID:         modelID,
				API:             poo.API(endpoint.API),
				ToolsMode:       model.ToolsMode,
				Inputs:          inputs,
				ContextLimit:    model.ContextLimit,
				MaxOutput:       model.MaxOutput,
				Reasoning:       model.Reasoning,
				ReasoningLevels: model.ReasoningLevels,
				Quirks:          model.Quirks,
				Sampling:        model.Sampling,
			}
		}
	}
	return out, nil
}

// modalitiesNamed reads a model's input modalities, and refuses one poo does
// not read.
//
// A modality nothing reads would be a model that says it takes images while
// nothing sends it one, so the name is refused where it is written.
func modalitiesNamed(endpoint, modelID string, names []string) ([]poo.Modality, error) {
	out := make([]poo.Modality, 0, len(names))
	for _, name := range names {
		if !poo.ModalityKnown(name) {
			return nil, fmt.Errorf("the model %s/%s reads %q, which poo does not know", endpoint, modelID, name)
		}
		out = append(out, poo.Modality(name))
	}
	return out, nil
}
