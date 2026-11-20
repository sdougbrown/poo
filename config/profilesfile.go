package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// importProfilesFiles reads the files the settings name as holding
// profile definitions and folds them below the settings' own profiles.
//
// The files layer in the order they are named: the settings' word on a
// profile name wins over a file's, and the first file that names one
// wins over the later files. A profile a file declares lands in the
// same map a profile the settings declared does, so everything that
// reads the profiles — the base's resolution, the tool exclusions, the
// prompt file — reads it the same way.
func (s *Settings) importProfilesFiles(dir string) error {
	for _, named := range s.ProfilesFiles {
		path, err := resolveBeside(named, dir)
		if err != nil {
			return s.profilesFileError(err)
		}
		document, err := readProfilesDocument(path)
		if err != nil {
			return s.profilesFileError(err)
		}
		layer, err := profilesLayer(path, document)
		if err != nil {
			return s.profilesFileError(err)
		}
		s.mergeProfiles(layer)
	}
	return nil
}

// mergeProfiles folds a layer below the profiles already held: a name
// the settings or an earlier file declared keeps its definition.
func (s *Settings) mergeProfiles(layer map[string]ProfileSettings) {
	if s.Profiles == nil {
		s.Profiles = map[string]ProfileSettings{}
	}
	for name, definition := range layer {
		if _, held := s.Profiles[name]; !held {
			s.Profiles[name] = definition
		}
	}
}

// profilesFileError names the setting at fault: every failure here is
// one a named file caused, so the field is the setting that named it.
func (s *Settings) profilesFileError(err error) error {
	return &Error{Code: CodeInvalid, Field: "profiles_files", Err: err}
}

// profilesFile is a file of profile definitions. The vocabulary is the
// settings' own, so nothing is expressible only here. The document is
// decoded strictly: a key this file does not have is a failure that
// names it, rather than a profile with no effect.
type profilesFile struct {
	// Profiles are the profile definitions the file declares, by name.
	Profiles map[string]ProfileSettings `json:"profiles"`
}

// profilesLayer reads a profiles file and returns the definitions it
// declares.
//
// A file that declares nothing is refused rather than read as an empty
// layer: a profiles file that imports halfway is worse than one that
// says what is wrong with it.
func profilesLayer(path string, document []byte) (map[string]ProfileSettings, error) {
	var file profilesFile
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(file.Profiles) == 0 {
		return nil, fmt.Errorf("%s: the file declares no profiles", path)
	}
	for name, definition := range file.Profiles {
		if definition.PromptFile == "" {
			continue
		}
		resolved, err := resolveBeside(definition.PromptFile, filepath.Dir(path))
		if err != nil {
			return nil, fmt.Errorf("%s: the profile %s's prompt_file cannot be resolved: %w", path, name, err)
		}
		definition.PromptFile = resolved
		file.Profiles[name] = definition
	}
	return file.Profiles, nil
}

// readProfilesDocument reads a profiles file and returns it as JSON.
//
// One codec over two encodings, the pipeline the models file reads
// through: the file is YAML, which is what an operator edits, and JSON
// is YAML that an operator may also write.
func readProfilesDocument(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	document, err := normalize(stripDocumentBOM(data), FormatYAML, "the profiles file")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return document, nil
}
