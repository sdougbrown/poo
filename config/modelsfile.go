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
	if modelsPath != "" {
		if err := s.importModelsFile(modelsPath); err != nil {
			return err
		}
	}
	// pi's built-in providers are offered when a credential file is there,
	// because they are what its credential can run. A settings file that names
	// no credential file, and has none at the default path, gains no models it
	// did not ask for. The models file and the settings' own entries still win
	// on a collision.
	if s.credentialPath != "" && fileExists(s.credentialPath) {
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
	file, err := piimport.Read(path, s.credentialPath)
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
