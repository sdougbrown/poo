package piauth

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// The fields poo's own auth file is written with.
const (
	// versionField is the field the file carries its version under.
	versionField = "version"
	// credentialsField is the field the file carries its credentials under.
	credentialsField = "credentials"
)

// pooVersion is the only version of poo's own auth file this package reads and
// writes. A file that names another one is refused, not read halfway: a later
// version is not this version with extra fields.
const pooVersion = 1

// pooFormat is the shape poo's own auth file is written with: a version, and
// the credentials under it, by provider id.
type pooFormat struct{}

// name is the shape's own name, for a report to say which file it read.
func (pooFormat) name() string { return FormatPoo }

// credentials returns the credentials the file holds under its version.
//
// A file that names another version is a file this package would write wrong,
// so it is refused before anything is read out of it.
func (pooFormat) credentials(top map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	raw, ok := top[versionField]
	if !ok {
		return nil, fmt.Errorf("the file names no version, and version %d is the one this reads", pooVersion)
	}
	var version int
	if err := json.Unmarshal(raw, &version); err != nil {
		return nil, fmt.Errorf("%s is not a version number", versionField)
	}
	if version != pooVersion {
		return nil, fmt.Errorf("the file names version %d, and version %d is the one this reads", version, pooVersion)
	}
	entries := map[string]json.RawMessage{}
	raw, ok = top[credentialsField]
	if !ok {
		return entries, nil
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("%s is not a map of credentials", credentialsField)
	}
	return entries, nil
}

// envelope returns the object to write: this version, and the credentials under
// it.
func (pooFormat) envelope(entries map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{
		versionField:     json.RawMessage(strconv.Itoa(pooVersion)),
		credentialsField: encoded,
	}, nil
}
