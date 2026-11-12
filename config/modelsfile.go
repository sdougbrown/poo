package config

import (
	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config/internal/piimport"
)

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
				Endpoint: provider.BaseURL,
				APIKey:   provider.APIKey,
			}
		}
		for _, model := range provider.Models {
			key := name + "/" + model.ID
			if _, ok := s.Models[key]; ok {
				continue
			}
			entry := Model{
				Endpoint: name,
				ModelID:  model.ID,
				API:      provider.API,
				// The endpoint a models file names speaks chat
				// completions, and nothing else fills a model's ops for a
				// name the curated families do not know: without it a
				// roles.chat naming this model fails at build.
				Ops:          []string{string(poo.OpChat)},
				Inputs:       model.Inputs,
				ContextLimit: model.ContextWindow,
				MaxOutput:    model.MaxTokens,
			}
			if model.Reasoning && model.SupportsReasoningEffort {
				entry.Reasoning = &poo.ReasoningControl{
					Mode:  poo.ReasoningEffort,
					Param: poo.ReasoningPathEffort,
				}
			}
			if s.Models == nil {
				s.Models = map[string]Model{}
			}
			s.Models[key] = entry
		}
	}
}
