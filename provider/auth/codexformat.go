package auth

import (
	"encoding/json"
	"fmt"
	"time"
)

// The fields the Codex CLI's own auth.json is written with.
const (
	// codexTokensField is the field the CLI carries its oauth grant under.
	codexTokensField = "tokens"
	// codexKeyField is the field the CLI carries an api key under, in the
	// api-key mode that holds no tokens at all.
	codexKeyField = "OPENAI_API_KEY"
)

// codexFormat is the shape the Codex CLI's own auth.json is written with: an
// api key beside the tokens object the CLI's sign-in wrote.
//
// The file is the CLI's. The CLI signs in and the CLI refreshes, and poo
// reads the credential it holds. The caller's word keeps that true: a
// read-only store (Credentials.ReadOnly) is the mode a shared file is read
// in. A read-only store never writes. The format refuses a write here too,
// so a second writer cannot grow.
type codexFormat struct{}

// name is the shape's own name, for a report to say which file it read.
func (codexFormat) name() string { return FormatCodex }

// credentials returns the file's credential, projected into the entry shape
// the other two files spell.
//
// The projection is by the names the CLI spells. The tokens object holds the
// access token, the refresh token and the account. The expiry is read from
// the access token's own exp claim, because the file records no expiry of
// its own. A file in api-key mode — an api key and no tokens — holds no
// credential the Codex provider reads, and answers no entry here.
func (codexFormat) credentials(top map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	raw, ok := top[codexTokensField]
	// A JSON null is the api-key mode's way of writing no tokens: the key is
	// there and the grant is not. It answers no entry, like an absent field
	// would, or the provider would be offered on a credential no request can
	// use.
	if !ok || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	}
	if err := json.Unmarshal(raw, &tokens); err != nil {
		return nil, fmt.Errorf("%s is not the token object the Codex CLI writes: %v", codexTokensField, err)
	}
	stored := credential{
		Type:      string(KindOAuth),
		Access:    tokens.AccessToken,
		Refresh:   tokens.RefreshToken,
		Expires:   jwtExpiry(tokens.AccessToken),
		AccountID: tokens.AccountID,
	}
	encoded, err := json.Marshal(stored.fileEntry())
	if err != nil {
		return nil, fmt.Errorf("%s: %v", codexTokensField, err)
	}
	return map[string]json.RawMessage{ProviderCodex: encoded}, nil
}

// envelope is the last line of a rule the store applies earlier — a codex
// document is refused before a trade is composed, and a read-only store
// before any write — and refuses here all the same.
func (codexFormat) envelope(map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	return nil, fmt.Errorf("the Codex CLI's auth.json is never written by poo")
}

// marshal refuses, by the same word the envelope refuses.
func (codexFormat) marshal(path string, _ map[string]json.RawMessage) ([]byte, error) {
	return nil, fmt.Errorf("%s: the Codex CLI's auth.json is never written by poo", path)
}

// jwtExpiry reads a JWT's exp claim, in milliseconds since the epoch.
//
// A token that names no exp has no readable expiry: it is stored with none,
// and a read of it reports an expired credential rather than a live one,
// because a token whose life is unknown is not one to send.
func jwtExpiry(token string) int64 {
	claims, ok := jwtPayload(token)
	if !ok {
		return 0
	}
	var exp float64
	if err := json.Unmarshal(claims["exp"], &exp); err != nil || exp <= 0 {
		return 0
	}
	return time.Unix(int64(exp), 0).UnixMilli()
}
