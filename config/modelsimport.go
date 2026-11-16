package config

import (
	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config/internal/piimport"
	"github.com/sdougbrown/poo/provider/catalog"
)

// piLayer turns pi's models file into the models file's layer: the endpoints it
// declares, and each model under the name provider/id.
//
// pi's file and poo's say the same things in different words, so this is the one
// translation from pi's vocabulary into the layer's. A provider whose key the
// file writes literally keeps that key out of the layer: a models file never
// carries a literal key, and the settings are where one lives.
func piLayer(file piimport.File) catalog.FileSettings {
	layer := catalog.FileSettings{
		Endpoints: make(map[string]catalog.EndpointSettings, len(file.Providers)),
		Models:    map[string]catalog.ModelSettings{},
	}
	for name, provider := range file.Providers {
		layer.Endpoints[name] = catalog.EndpointSettings{
			Endpoint:  provider.BaseURL,
			APIKeyEnv: provider.APIKeyEnv,
			SkipProbe: provider.SkipProbe,
		}
		for _, model := range provider.Models {
			entry := catalog.ModelSettings{
				Endpoint:     name,
				ModelID:      model.ID,
				API:          poo.API(provider.API),
				ToolsMode:    poo.ToolsMode(provider.ToolsMode),
				Inputs:       modalities(model.Inputs),
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
			layer.Models[name+"/"+model.ID] = entry
		}
	}
	return layer
}

// addLayer folds a layer into the models file's, a later one overruling an
// earlier one by name.
func (s *Settings) addLayer(layer catalog.FileSettings) {
	if s.modelsFileLayer.Endpoints == nil {
		s.modelsFileLayer.Endpoints = map[string]catalog.EndpointSettings{}
	}
	if s.modelsFileLayer.Models == nil {
		s.modelsFileLayer.Models = map[string]catalog.ModelSettings{}
	}
	for name, endpoint := range layer.Endpoints {
		s.modelsFileLayer.Endpoints[name] = endpoint
	}
	for name, model := range layer.Models {
		s.modelsFileLayer.Models[name] = model
	}
}

// importPiKeys keeps in the settings the keys pi's file writes literally, which
// is where a literal key lives.
//
// A settings endpoint of the same name is the operator's own declaration, and it
// wins: the key the file carries is then not read.
func (s *Settings) importPiKeys(file piimport.File) {
	for name, provider := range file.Providers {
		if provider.APIKey == "" {
			continue
		}
		if _, ok := s.Endpoints[name]; ok {
			continue
		}
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
}
