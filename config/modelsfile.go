package config

import (
	"path/filepath"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config/internal/piimport"
)

// ModelsFileName is the name a models file is discovered by, beside a
// settings file.
//
// The name is the file's own, from the shape it carries: the settings name a
// models file with models_file, and one they do not name is read from here.
const ModelsFileName = "models.json"

// importModelsFile imports the models file the settings name, or the one
// beside them.
//
// A named file wins over a discovered one, and a relative name resolves
// against dir — the settings file's own directory — rather than the working
// directory. dir is empty for a document held in memory, which is what says
// there is nothing to discover beside it.
func (s *Settings) importModelsFile(dir string) error {
	path := s.ModelsFile
	named := path != ""
	switch {
	case named:
		expanded, err := expandHome(path)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(expanded) && dir != "" {
			expanded = filepath.Join(dir, expanded)
		}
		path = expanded
	case dir != "":
		candidate := filepath.Join(dir, ModelsFileName)
		if !fileExists(candidate) {
			return nil
		}
		path = candidate
	default:
		return nil
	}
	file, err := piimport.Read(path)
	if err != nil {
		if !named {
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
