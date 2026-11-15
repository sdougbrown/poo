package config

import (
	"os"
	"path/filepath"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config/internal/piimport"
	"github.com/sdougbrown/poo/provider/codex"
)

// ModelsFileName is the name a models file is discovered by, beside a
// settings file.
//
// The name is the file's own, from the shape it carries: the settings name a
// models file with models_file, and one they do not name is read from here.
const ModelsFileName = "models.json"

// importPi imports what pi's files hold: the models file the settings name or
// the one beside them, and the auth.json the Codex provider reads. pi's
// built-in providers are offered when that auth file is there.
//
// The auth path is resolved here, where the settings load, because the models
// file's own directory is what a sibling auth.json answers to. A Settings built
// by hand has no resolved path, and piAuthFile falls back to the setting it
// names.
func (s *Settings) importPi(dir string) error {
	modelsPath, err := s.modelsFilePath(dir)
	if err != nil {
		return err
	}
	// The auth file is resolved before the models file is read, so one path
	// answers for every reader: the Codex credential the transport resolves,
	// and the api keys the models file's providers import.
	s.piAuthPath, err = s.resolvePiAuthFile(dir, modelsPath)
	if err != nil {
		return err
	}
	if modelsPath != "" {
		if err := s.importModelsFile(modelsPath); err != nil {
			return err
		}
	}
	// pi's built-in providers are offered when a pi auth file is there, because
	// they are what its credential can run. A settings file that names none of
	// pi's files gains no models it did not ask for. The models file and the
	// settings' own entries still win on a collision.
	if s.piAuthPath != "" && fileExists(s.piAuthPath) {
		s.applyModelsFile(piimport.BuiltinCodex(codex.DefaultEndpoint))
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
		candidate := filepath.Join(dir, ModelsFileName)
		if !fileExists(candidate) {
			return "", nil
		}
		return candidate, nil
	default:
		return "", nil
	}
}

// importModelsFile imports a models file, and the keys its providers read from
// the auth file the settings resolved.
func (s *Settings) importModelsFile(path string) error {
	file, err := piimport.Read(path, s.piAuthPath)
	if err != nil {
		if s.ModelsFile == "" {
			// The settings named no file, so no setting is at fault: the
			// file beside them is, and the message names it.
			return &Error{
				Code:    CodeInvalid,
				Message: "the models file beside the settings does not read",
				Err:     err,
			}
		}
		// The reader names the file it read; the setting that named it is
		// what a caller branches on.
		return &Error{Code: CodeInvalid, Field: "models_file", Err: err}
	}
	s.applyModelsFile(file)
	return nil
}

// resolvePiAuthFile returns the auth.json the Codex provider reads.
//
// An explicit pi_auth_file wins, and a relative one answers to the settings
// file's directory the way models_file does. A named file that is not there is
// a settings failure: an explicit path is a promise. With none named, auth.json
// beside the models file is the file, whether or not it exists yet. The Codex
// transport reports a credential that is missing, with the path it looked in.
func (s *Settings) resolvePiAuthFile(dir, modelsPath string) (string, error) {
	if s.PiAuthFile == "" {
		if modelsPath == "" {
			return "", nil
		}
		return filepath.Join(filepath.Dir(modelsPath), piimport.AuthFileName), nil
	}
	path, err := expandHome(s.PiAuthFile)
	if err != nil {
		return "", &Error{Code: CodeInvalid, Field: "pi_auth_file", Message: "the pi auth file cannot be resolved", Err: err}
	}
	if !filepath.IsAbs(path) && dir != "" {
		path = filepath.Join(dir, path)
	}
	if _, err := os.Stat(path); err != nil {
		return "", &Error{Code: CodeInvalid, Field: "pi_auth_file", Message: "the file is not there", Err: err}
	}
	return path, nil
}

// piAuthFile returns the auth file the Codex provider reads.
//
// A Settings that was loaded has the path resolved. One built by hand names it,
// and the path answers to the working directory the way every other hand-built
// path does.
func (s *Settings) piAuthFile() string {
	if s.piAuthPath != "" {
		return s.piAuthPath
	}
	if s.PiAuthFile == "" {
		return ""
	}
	path, err := expandHome(s.PiAuthFile)
	if err != nil {
		return s.PiAuthFile
	}
	return path
}

// applyModelsFile fills the providers and models a models file names,
// where the settings do not already hold them.
//
// A model is named by its provider and id together: provider/id, the
// name the file's own tool answers to. The settings' entries win on a
// collision, so a models file is an addition, never a takeover.
func (s *Settings) applyModelsFile(file piimport.File) {
	for name, provider := range file.Providers {
		if _, ok := s.Endpoints[name]; !ok {
			if s.Endpoints == nil {
				s.Endpoints = map[string]Endpoint{}
			}
			s.Endpoints[name] = Endpoint{
				Endpoint:  provider.BaseURL,
				APIKeyEnv: provider.APIKeyEnv,
				APIKey:    provider.APIKey,
				SkipProbe: provider.SkipProbe,
			}
		}
		for _, model := range provider.Models {
			key := name + "/" + model.ID
			if _, ok := s.Models[key]; ok {
				continue
			}
			entry := Model{
				Endpoint:     name,
				ModelID:      model.ID,
				API:          provider.API,
				ToolsMode:    poo.ToolsMode(provider.ToolsMode),
				Inputs:       model.Inputs,
				ContextLimit: model.ContextWindow,
				MaxOutput:    model.MaxTokens,
			}
			if model.Reasoning && model.SupportsReasoningEffort {
				entry.Reasoning = &poo.ReasoningControl{
					Mode:  poo.ReasoningEffort,
					Param: poo.ReasoningPathEffort,
				}
				entry.ReasoningLevels = model.Levels
			}
			if s.Models == nil {
				s.Models = map[string]Model{}
			}
			s.Models[key] = entry
		}
	}
}
