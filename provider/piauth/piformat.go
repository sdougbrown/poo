package piauth

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// piFormat is the shape pi's auth.json is written with: every provider's
// credential at the file's top level, under the provider id.
type piFormat struct{}

// piEntry is one provider's entry in pi's auth.json, in the shape the oauth
// type is written with. Expires is milliseconds since the epoch, the number pi
// stores.
type piEntry struct {
	// Type is how the entry authenticates: "oauth" here.
	Type string `json:"type"`
	// Access is the access token.
	Access string `json:"access"`
	// Refresh is the refresh token.
	Refresh string `json:"refresh"`
	// Expires is when Access stops working.
	Expires int64 `json:"expires"`
	// AccountID is the ChatGPT account Access belongs to.
	AccountID string `json:"accountId"`
}

// parse reads pi's auth.json: credentials by provider id.
//
// A file that is not there is a file without credentials, the way pi reads one.
// A leading byte order mark does not count against the file: pi strips it
// before parsing, so a BOM file is a file pi reads. A file that does not parse
// is a failure, not an empty file. A write over a file this package did not
// understand would drop what it holds.
func (piFormat) parse(path string, data []byte, missing bool) (document, error) {
	doc := document{path: path, entries: map[string]json.RawMessage{}}
	if missing {
		return doc, nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(stripBOM(data), &entries); err != nil {
		return document{}, fmt.Errorf("%w: %s: %w", ErrStorage, path, err)
	}
	for provider, entry := range entries {
		doc.entries[provider] = entry
	}
	return doc, nil
}

// credential returns the credential the file holds for a provider.
func (piFormat) credential(doc document, provider string) (credential, error) {
	raw, ok := doc.entries[provider]
	if !ok {
		return credential{}, fmt.Errorf("%w: %s holds no %s entry", ErrNoCredential, doc.path, provider)
	}
	var stored piEntry
	if err := json.Unmarshal(raw, &stored); err != nil {
		return credential{}, fmt.Errorf("%w: %s: %w", ErrStorage, doc.path, err)
	}
	if stored.Type != "oauth" || stored.Access == "" || stored.Refresh == "" {
		return credential{}, fmt.Errorf("%w: %s holds a %s entry that is not an oauth credential", ErrNoCredential, doc.path, provider)
	}
	// A Codex request names the account its token belongs to, so a credential
	// that names none cannot be used. It is refused before a refresh can write
	// a credential that still names none.
	if accountID(stored.Access, stored.AccountID) == "" {
		return credential{}, fmt.Errorf("%w: %s names no account", ErrNoCredential, doc.path)
	}
	// The two shapes are the same fields, so one is the other without the
	// file's tags. A field added to either is a compile error here until it is
	// added to both.
	return credential(stored), nil
}

// encode writes pi's auth.json back with a provider's credential stored, and
// the other providers' entries keeping their values. A field the entry carries
// beyond the five this package models is kept too: pi's credential type allows
// fields of its own, and they are not this package's to drop. The fields come
// out in the order Go marshals a map, sorted; the values are what has to match.
//
// The bytes are the ones pi writes: two space indents, and no trailing
// newline.
func (piFormat) encode(doc document, provider string, entry credential) ([]byte, error) {
	fields := map[string]json.RawMessage{}
	if raw, ok := doc.entries[provider]; ok {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("%s: the %s entry is not an object: %v", doc.path, provider, err)
		}
	}
	own, err := json.Marshal(piEntry(entry))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", doc.path, err)
	}
	var written map[string]json.RawMessage
	if err := json.Unmarshal(own, &written); err != nil {
		return nil, fmt.Errorf("%s: %v", doc.path, err)
	}
	for name, value := range written {
		fields[name] = value
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", doc.path, err)
	}
	doc.entries[provider] = encoded
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc.entries); err != nil {
		return nil, fmt.Errorf("%s: %v", doc.path, err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// stripBOM drops a leading UTF-8 byte order mark, the character pi strips
// before parsing its own file. A file a Windows editor wrote opens with one,
// and every file this package reads is one pi reads.
func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte("\ufeff"))
}
