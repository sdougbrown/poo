package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// importProfilesFiles reads the files the settings name as holding
// profile definitions and folds them below the settings' own profiles.
//
// The files layer in the order they are named. The settings' word on a
// profile name wins over a file's, and the first file that names one
// wins over the later files.
//
// A profile a file declares lands in the same map a profile the
// settings declare does. Everything that reads the profiles reads it
// the same way: the base's resolution, the tool exclusions, the prompt
// file.
func (s *Settings) importProfilesFiles(dir string) error {
	for _, named := range s.ProfilesFiles {
		path, err := resolveBeside(named, dir)
		if err != nil {
			return s.profilesFileError(err)
		}
		// The file's directory answers for the prompt files it names, so it
		// is recorded here. The settings' own resolution pass reads it, and
		// reports a failure under the same field a settings-declared
		// profile's failure carries.
		fileDir, err := filepath.Abs(filepath.Dir(path))
		if err != nil {
			return s.profilesFileError(err)
		}
		document, err := readProfilesDocument(path)
		if err != nil {
			return s.profilesFileError(err)
		}
		layer, err := profilesLayer(document)
		if err != nil {
			return s.profilesFileError(fmt.Errorf("%s: %w", path, err))
		}
		s.mergeProfiles(layer, fileDir)
	}
	return nil
}

// mergeProfiles folds a layer below the profiles already held: a name
// the settings or an earlier file declared keeps its definition. fileDir
// is the directory the definitions' relative paths answer to.
func (s *Settings) mergeProfiles(layer map[string]ProfileSettings, fileDir string) {
	if s.Profiles == nil {
		s.Profiles = map[string]ProfileSettings{}
	}
	for name, definition := range layer {
		if _, held := s.Profiles[name]; !held {
			s.Profiles[name] = definition
			if s.profilesFileDirs == nil {
				s.profilesFileDirs = map[string]string{}
			}
			s.profilesFileDirs[name] = fileDir
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
// layer. A profiles file that imports halfway is worse than one that
// says what is wrong with it.
func profilesLayer(document []byte) (map[string]ProfileSettings, error) {
	var file profilesFile
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, err
	}
	if len(file.Profiles) == 0 {
		return nil, fmt.Errorf("the file declares no profiles")
	}
	return file.Profiles, nil
}

// readProfilesDocument reads a profiles file and returns it as JSON,
// through the reader the models file shares.
func readProfilesDocument(path string) ([]byte, error) {
	return readYAMLDocument(path, "the profiles file")
}
