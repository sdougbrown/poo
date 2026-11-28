package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/sdougbrown/poo/hook"
)

// importHooksFiles reads the files the settings name as holding hook
// specs and folds them below the settings' own hooks.
//
// The files layer in the order they are named. The settings' word on a
// hook name wins over a file's, and the first file that names one wins
// over the later files.
//
// A hook a file declares lands in the same map a hook the settings
// declare does. Everything that reads the hooks reads it the same way:
// the check that compiles them, and the profile that selects them.
func (s *Settings) importHooksFiles(dir string) error {
	for _, named := range s.HooksFiles {
		path, err := resolveBeside(named, dir)
		if err != nil {
			return s.hooksFileError(err)
		}
		document, err := readHooksDocument(path)
		if err != nil {
			return s.hooksFileError(err)
		}
		layer, err := hooksLayer(document)
		if err != nil {
			return s.hooksFileError(fmt.Errorf("%s: %w", path, err))
		}
		s.mergeHooks(layer)
	}
	return nil
}

// mergeHooks folds a layer below the hooks already held: a name the
// settings or an earlier file declared keeps its spec.
func (s *Settings) mergeHooks(layer map[string]hook.Spec) {
	if s.Hooks == nil {
		s.Hooks = map[string]hook.Spec{}
	}
	for name, spec := range layer {
		if _, held := s.Hooks[name]; !held {
			s.Hooks[name] = spec
		}
	}
}

// hooksFileError names the setting at fault: every failure here is one
// a named file caused, so the field is the setting that named it.
func (s *Settings) hooksFileError(err error) error {
	return &Error{Code: CodeInvalid, Field: "hooks_files", Err: err}
}

// hooksFile is a file of hook specs. The vocabulary is the settings'
// own, so nothing is expressible only here. The document is decoded
// strictly: a key this file does not have is a failure that names it,
// rather than a hook whose policy silently loses part of its shape.
type hooksFile struct {
	// Hooks are the hook specs the file declares, by name.
	Hooks map[string]hook.Spec `json:"hooks"`
}

// hooksLayer reads a hooks file and returns the specs it declares.
//
// A file that declares nothing is refused rather than read as an empty
// layer. A hooks file that imports halfway is worse than one that says
// what is wrong with it.
func hooksLayer(document []byte) (map[string]hook.Spec, error) {
	var file hooksFile
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, err
	}
	if len(file.Hooks) == 0 {
		return nil, fmt.Errorf("the file declares no hooks")
	}
	return file.Hooks, nil
}

// readHooksDocument reads a hooks file and returns it as JSON, through
// the reader the profiles file shares.
func readHooksDocument(path string) ([]byte, error) {
	return readYAMLDocument(path, "the hooks file")
}
