package piauth

import "encoding/json"

// piFormat is the shape pi's auth.json is written with: every provider's
// credential at the file's top level, under the provider id.
type piFormat struct{}

// name is the shape's own name, for a report to say which file it read.
func (piFormat) name() string { return FormatPi }

// credentials returns the file's credentials. pi keeps them at the file's top
// level, so the object the file parses to is the credentials map.
func (piFormat) credentials(top map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	return top, nil
}

// envelope returns the object to write. pi's file holds the credentials at its
// top level, and nothing else.
func (piFormat) envelope(entries map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	return entries, nil
}
