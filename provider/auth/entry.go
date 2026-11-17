package auth

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// fileEntry is one provider's credential as both auth files spell it.
//
// The field names are pi's, and poo's own file keeps them: the entry is the
// same credential whichever file holds it, so one importer reads either. Only
// the envelope around it differs, and each format owns its own.
type fileEntry struct {
	// Type is how the entry authenticates: "oauth" here.
	Type string `json:"type"`
	// Access is the access token.
	Access string `json:"access"`
	// Refresh is the refresh token.
	Refresh string `json:"refresh"`
	// Expires is when Access stops working, in milliseconds since the epoch.
	Expires int64 `json:"expires"`
	// AccountID is the ChatGPT account Access belongs to.
	AccountID string `json:"accountId"`
}

// stripBOM drops a leading UTF-8 byte order mark, the character pi strips
// before parsing its own file. A file a Windows editor wrote opens with one,
// and both of these files are one an operator may have touched.
func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte("\ufeff"))
}

// decodeEntry returns the credential a provider's entry holds.
//
// The check is the same for both files, because both spell an entry the same
// way. A field the entry carries beyond the five modelled here is not lost: a
// write puts the entry back from the bytes it read.
func decodeEntry(path, provider string, entries map[string]json.RawMessage) (credential, error) {
	raw, ok := entries[provider]
	if !ok {
		return credential{}, fmt.Errorf("%w: %s holds no %s entry", ErrNoCredential, path, provider)
	}
	var stored fileEntry
	if err := json.Unmarshal(raw, &stored); err != nil {
		return credential{}, fmt.Errorf("%w: %s: %w", ErrStorage, path, err)
	}
	if stored.Type != "oauth" || stored.Access == "" || stored.Refresh == "" {
		return credential{}, fmt.Errorf("%w: %s holds a %s entry that is not an oauth credential", ErrNoCredential, path, provider)
	}
	// A Codex request names the account its token belongs to, so a credential
	// that names none cannot be used. It is refused before a refresh can write
	// a credential that still names none.
	if accountID(stored.Access, stored.AccountID) == "" {
		return credential{}, fmt.Errorf("%w: %s names no account", ErrNoCredential, path)
	}
	// The two shapes are the same fields, so one is the other without the
	// file's tags. A field added to either is a compile error here until it is
	// added to both.
	return credential(stored), nil
}

// encodeEntry stores a credential in the entry a provider's credentials map
// holds it under, and leaves every other entry as it was.
//
// A field the entry carries beyond the five fileEntry models is kept: pi's
// credential type allows fields of its own, and they are not this package's to
// drop.
func encodeEntry(path, provider string, entries map[string]json.RawMessage, entry credential) error {
	fields := map[string]json.RawMessage{}
	if raw, ok := entries[provider]; ok {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("%s: the %s entry is not an object: %v", path, provider, err)
		}
	}
	own, err := json.Marshal(fileEntry(entry))
	if err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	var written map[string]json.RawMessage
	if err := json.Unmarshal(own, &written); err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	for name, value := range written {
		fields[name] = value
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	entries[provider] = encoded
	return nil
}

// readObject parses an auth file's top-level object.
//
// A file is JSON or YAML: pi writes JSON, and poo writes YAML, which is what an
// operator edits. JSON is a subset of YAML, so the JSON reader answers for both
// files and a YAML file falls through to the YAML one.
func readObject(data []byte) (map[string]json.RawMessage, error) {
	document := stripBOM(data)
	if len(document) == 0 {
		return nil, fmt.Errorf("the file is empty")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(document, &top); err == nil {
		return top, nil
	}
	var value any
	if err := yaml.Unmarshal(document, &value); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &top); err != nil {
		return nil, err
	}
	return top, nil
}

// yamlObject returns an object's fields as the values a YAML writer takes.
func yamlObject(object map[string]json.RawMessage) (map[string]any, error) {
	fields := make(map[string]any, len(object))
	for field, raw := range object {
		value, err := yamlValue(raw)
		if err != nil {
			return nil, err
		}
		fields[field] = value
	}
	return fields, nil
}

// yamlValue reads one field for the YAML writer.
//
// A JSON number decodes into a float by default, and a float is written as 1.76e+12:
// a millisecond epoch nobody can read or edit, in the one file an operator is meant
// to be able to edit. A number that is whole stays whole.
func yamlValue(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return whole(value), nil
}

// whole walks a decoded value and turns a whole number back into an integer.
func whole(value any) any {
	switch v := value.(type) {
	case json.Number:
		if integer, err := v.Int64(); err == nil {
			return integer
		}
		if fraction, err := v.Float64(); err == nil {
			return fraction
		}
		return v.String()
	case map[string]any:
		out := make(map[string]any, len(v))
		for field, item := range v {
			out[field] = whole(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, whole(item))
		}
		return out
	}
	return value
}
