package piauth

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// marshalFile returns an auth file's bytes: two space indents, and no trailing
// newline, the shape pi writes its own file in. The fields come out in the
// order Go marshals a map, sorted; the values are what has to match.
func marshalFile(path string, object map[string]json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(object); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
