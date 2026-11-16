package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// fileEntry is one provider's credential as both auth files spell it.
//
// The field names are pi's, and poo's own file keeps them: the entry is the
// same credential whichever file holds it, so one importer reads either. Only
// the envelope around it differs, and each format owns its own.
type fileEntry struct {
	// Type is how the entry authenticates: KindOAuth or KindAPIKey.
	Type string `json:"type"`
	// Access is the access token, for an oauth entry.
	Access string `json:"access,omitempty"`
	// Refresh is the refresh token, for an oauth entry.
	Refresh string `json:"refresh,omitempty"`
	// Expires is when Access stops working, in milliseconds since the epoch.
	Expires int64 `json:"expires,omitempty"`
	// AccountID is the account the access token names, for a provider that names one.
	AccountID string `json:"accountId,omitempty"`
	// Key is the key itself, for an api_key entry.
	Key string `json:"key,omitempty"`
	// ClientID is the OAuth client the credential was granted to. A refresh names it, because
	// the endpoint matches a refresh token to the client that was granted it.
	ClientID string `json:"clientId,omitempty"`
	// TokenURL is where that client trades its tokens. The client that granted a credential
	// decides where the credential is refreshed.
	TokenURL string `json:"tokenUrl,omitempty"`
}

// stripBOM drops a leading UTF-8 byte order mark, the character pi strips
// before parsing its own file. A file a Windows editor wrote opens with one,
// and both of these files are one an operator may have touched.
func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte("\ufeff"))
}

// decodeEntry returns the credential a provider's entry holds.
//
// The check is the same for both files, because both spell an entry the same way. What a
// provider needs beyond the entry's shape — an account its tokens must name — is the
// provider's, and arrives with it. A field the entry carries beyond the ones modelled here
// is not lost: a write puts the entry back from the bytes it read.
func decodeEntry(path string, p Provider, entries map[string]json.RawMessage) (credential, error) {
	raw, ok := entries[p.ID]
	if !ok {
		return credential{}, fmt.Errorf("%w: %s holds no %s entry", ErrNoCredential, path, p.ID)
	}
	var stored fileEntry
	if err := json.Unmarshal(raw, &stored); err != nil {
		return credential{}, fmt.Errorf("%w: %s: %w", ErrStorage, path, err)
	}
	entry := credential(stored)
	// The write path asks the same question of the same rule: a credential Set stores is one
	// Read returns, and one validator is what keeps that true.
	if err := validate(entry.export(), p); err != nil {
		return credential{}, fmt.Errorf("%w: %s holds an entry for %s: %v", ErrNoCredential, path, p.ID, err)
	}
	return entry, nil
}

// modelledFields are the entry's fields this package writes and reads. A field outside them
// is one pi's credential type may hold and poo does not model: it is kept as it was.
var modelledFields = []string{"access", "refresh", "expires", "accountId", "key", "clientId", "tokenUrl"}

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
	own, err := json.Marshal(entry.fileEntry())
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
	// A field this kind does not carry is not part of this credential any more: replacing an
	// api key with an oauth credential must not leave the key behind, or the next read finds a
	// credential that carries both and refuses it. A field beyond the model stays as it was,
	// because it is not this package's to drop.
	for _, name := range modelledFields {
		if _, ok := written[name]; !ok {
			delete(fields, name)
		}
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

// fileEntry is the entry as both files spell it.
func (c credential) fileEntry() fileEntry {
	return fileEntry(c)
}

// Kind is how the entry authenticates, as the file spells it.
//
// An entry that names no type is an oauth one: that is what a credential with tokens is, and
// it is the same answer validate gives, so the read and the write agree.
func (c credential) Kind() Kind {
	if c.Type == "" {
		return KindOAuth
	}
	return Kind(c.Type)
}

// export turns the stored entry into the credential a caller hands over or copies.
func (c credential) export() Credential {
	out := Credential{Kind: c.Kind(), Key: c.Key}
	if out.Kind == KindOAuth {
		out.Access = c.Access
		out.Refresh = c.Refresh
		out.Expires = time.UnixMilli(c.Expires)
		out.AccountID = c.AccountID
		out.ClientID = c.ClientID
		out.TokenURL = c.TokenURL
	}
	return out
}

// internal turns a caller's credential into the entry the file holds.
func (c Credential) internal(p Provider) credential {
	if c.Kind == KindAPIKey {
		return credential{Type: string(KindAPIKey), Key: c.Key}
	}
	return credential{
		Type:      string(KindOAuth),
		Access:    c.Access,
		Refresh:   c.Refresh,
		Expires:   c.Expires.UnixMilli(),
		AccountID: p.account(c.Access, c.AccountID),
		ClientID:  c.ClientID,
		TokenURL:  c.TokenURL,
	}
}
