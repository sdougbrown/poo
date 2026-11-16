// Package piauth reads, refreshes and writes the OpenAI Codex OAuth credential
// poo keeps in an auth.json.
//
// The file's shape and the store that keeps the credential alive are two
// things, and they are two things here. The store owns the lock, the read, the
// refresh and the write; a format owns what one file's bytes look like. There
// are two shapes: pi's auth.json, which holds its providers at the top level,
// and poo's own, which holds a version and the credentials under it. The file
// itself says which one it is, so a caller hands over a path and nothing else.
//
// poo reads the file the settings name, never a pi installation's own
// directory, so a caller hands the path over. This package does not log in:
// pi's login writes the credential, and this package keeps it alive. Access
// uses the credential the file holds, and Refresh trades it for a new one.
//
// A stored access token expires, and pi and this package both refresh one by
// trading the refresh token at the same endpoint. Two refreshes of one
// credential race, and one of them loses the rotated token. This package
// therefore takes the lock pi takes: the directory <auth.json>.lock that
// proper-lockfile creates. It follows pi's staleness rule, and pi's habit of
// holding the lock across the token endpoint's answer. A refresh writes the
// file back with every other provider's entry as it was.
//
// Neither the refresh token nor the access token is written to a log or into
// an error. Only the caller sees a token, to put it on a request. A Codex
// request carries it as the bearer token, with the account the token names as
// the chatgpt-account-id header.
//
// An error wraps what failed underneath it, so a caller can tell a
// refused grant from a network failure with errors.Is.
package piauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ProviderCodex is the provider id pi stores the Codex credential
	// under. It is the key the file's entry is read and written by.
	ProviderCodex = "openai-codex"

	// DefaultTokenURL is the endpoint pi trades a Codex refresh token at.
	DefaultTokenURL = "https://auth.openai.com/oauth/token"

	// DefaultClientID is the OAuth client a Codex credential belongs to.
	// The token endpoint matches a refresh token to the client that was
	// granted it.
	DefaultClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// DefaultLeeway is how much life an access token must have left to
	// be used as it is. pi refreshes a token this close to expiry, so a
	// token pi would refresh is refreshed here too.
	DefaultLeeway = 5 * time.Minute

	// DefaultWait is how long a refresh waits for another writer's lock.
	// pi bounds its own wait at thirty seconds, and a pi refresh may
	// hold the lock for fifteen.
	DefaultWait = 30 * time.Second

	// DefaultTimeout bounds one refresh. A refresh holds the lock, so it is
	// bounded by pi's own fifteen second bound on a held lock. A longer one
	// could outlive the staleness rule and have the lock broken under it.
	DefaultTimeout = 15 * time.Second

	// accountClaim is where a Codex access token carries the account it
	// belongs to. It is the claim pi reads.
	accountClaim = "https://api.openai.com/auth"

	// maxTokenResponse bounds the token endpoint's answer.
	maxTokenResponse = 1 << 20
)

// The names a file's shape is reported by: pi's auth.json, and poo's own.
const (
	// FormatPi is pi's auth.json: its providers at the file's top level.
	FormatPi = "pi"
	// FormatPoo is poo's own auth file: a version, and the credentials under
	// it.
	FormatPoo = "poo"
)

// The failures a caller tells apart.
var (
	// ErrNoCredential means the auth.json holds no Codex credential that
	// can be used. A file with no such entry, an entry that is an api key,
	// and an entry that names no account are all this failure. The answer
	// is pi's login, not a retry.
	ErrNoCredential = errors.New("piauth: no Codex credential")

	// ErrLocked means another writer holds the auth.json's lock, so the
	// refresh did not start and nothing was traded. The file is exactly what
	// it was, and a caller may retry. pi holds the lock while it refreshes,
	// so this is what a pi refresh in flight looks like.
	ErrLocked = errors.New("piauth: the auth.json's lock is held by another writer")

	// ErrRefresh means the token endpoint would not trade the refresh
	// token. It refused the grant, answered with something that is not one,
	// or could not be reached. The file is unchanged, which is not to say
	// its credential still works. The grant the endpoint refused is the one
	// the file holds, so a retry most likely fails the same way.
	ErrRefresh = errors.New("piauth: the Codex token refresh failed")

	// ErrStorage means the auth.json could not be read, before anything
	// was traded. Nothing was written either.
	ErrStorage = errors.New("piauth: the auth.json could not be read")

	// ErrPersist means the endpoint traded the refresh token and the new
	// credential could not be stored. The file still holds the refresh token
	// that was traded, so it may no longer work. The caller's next step is
	// pi's login, not a retry of the same credential.
	ErrPersist = errors.New("piauth: the refreshed token was not stored")
)

// Token is a live Codex access token.
type Token struct {
	// Access is the bearer token.
	Access string
	// Expires is when Access stops working.
	Expires time.Time
	// AccountID is the ChatGPT account Access belongs to. Codex takes it
	// as the chatgpt-account-id header.
	AccountID string
}

// Status is what a credential file holds, as read: the credential, and never
// the token. A caller that reports on the credential asks for this; a caller
// that sends it asks for Access.
type Status struct {
	// Path is the file the credential was read from.
	Path string
	// Format is the shape that file is written with: FormatPi or FormatPoo.
	Format string
	// Provider is the id the credential is stored under.
	Provider string
	// AccountID is the ChatGPT account the access token names.
	AccountID string
	// Expires is when the access token stops working.
	Expires time.Time
	// Live reports whether the access token has the life left that Access
	// requires before it uses one as it is.
	Live bool
}

// format is one auth file's envelope: where its credentials sit in the object
// the file parses to, and the version it names.
//
// The entry both files spell the same way is not the format's business: an
// entry is decoded and encoded once, for both.
type format interface {
	// name is the shape's own name, for a report to say which file it read.
	name() string
	// credentials returns the credentials the file's object holds, or what is
	// wrong with the object.
	credentials(top map[string]json.RawMessage) (map[string]json.RawMessage, error)
	// envelope returns the object to write the credentials into.
	envelope(entries map[string]json.RawMessage) (map[string]json.RawMessage, error)
}

// document is one auth file: the shape it is written with, and the entries it
// holds, so a write puts everything the file carried back as it was.
type document struct {
	// path is the file the document was read from.
	path string
	// format is the shape the file is written with, the one it was read as.
	format format
	// entries are the credentials, by provider id, as the bytes the file
	// carried for each: a write keeps every field this package does not model.
	entries map[string]json.RawMessage
}

// credential is one provider's credential in poo's own words. A format
// projects its file's entry into it, and writes it back out in the file's.
type credential struct {
	// Type is how the entry authenticates: "oauth" here.
	Type string
	// Access is the access token.
	Access string
	// Refresh is the refresh token.
	Refresh string
	// Expires is when Access stops working, in milliseconds since the epoch.
	Expires int64
	// AccountID is the ChatGPT account Access belongs to.
	AccountID string
}

// Credentials are the Codex credential in one auth.json.
type Credentials struct {
	// Path is the auth.json to read and write. It is the one field a
	// caller must set.
	Path string
	// TokenURL overrides the endpoint the refresh token is traded at.
	TokenURL string
	// ClientID overrides the OAuth client the credential belongs to.
	ClientID string
	// HTTP supplies the transport and the timeout the refresh goes
	// through. Its redirect policy is not used: a redirect is refused
	// whatever the client says, because a refresh body holds the refresh
	// token. The refresh's own bound is the context's, so a client
	// without a timeout is fine.
	HTTP *http.Client
	// Now overrides the clock that ages an access token.
	Now func() time.Time
	// Leeway overrides how much life an access token must have left to
	// be used as it is. Zero means DefaultLeeway.
	Leeway time.Duration
	// Wait overrides how long a refresh waits for another writer's lock.
	// Zero means DefaultWait.
	Wait time.Duration
}

// Access returns the credential's access token, refreshing one that has
// expired or is close enough to expiry that pi would refresh it too.
//
// The read, the refresh and the write are one critical section under pi's
// lock. Two callers that need a token at once, and pi itself, trade the
// refresh token once. A refresh that fails leaves the file as it was.
//
// The failures tell a caller what a retry would cost. ErrLocked and
// ErrStorage come before anything is traded, so the file is untouched and a
// retry is free. ErrRefresh leaves the file untouched, but the endpoint may
// have traded the token before its response was lost. ErrPersist means the
// endpoint traded the token, so the file holds a refresh token that may
// be spent. A retry cannot be expected to work, and pi's login is the way
// back. A caller that gets ErrPersist gets no token either, because the traded
// token would go into a session the next process cannot reload.
func (c Credentials) Access(ctx context.Context) (Token, error) {
	return c.token(ctx, false)
}

// Refresh trades the credential's refresh token for a new one, whether the
// stored access token is live or not. A caller that needs the credential
// replaced, rather than used, calls this.
//
// The trade is Access's trade: the same lock, the same request, and the same
// write that keeps every other entry and field. The failures are the same.
// Concurrent calls each refresh in turn. The second uses the refresh token
// stored by the first.
func (c Credentials) Refresh(ctx context.Context) (Token, error) {
	return c.token(ctx, true)
}

// token reads the credential, trades it when force says so or when it is not
// live, and writes the traded one back. The whole of it is one critical
// section under pi's lock.
func (c Credentials) token(ctx context.Context, force bool) (Token, error) {
	if c.Path == "" {
		return Token{}, errors.New("piauth: no auth.json path")
	}
	hold, err := acquire(ctx, c.Path, c.wait())
	if err != nil {
		return Token{}, err
	}
	defer hold.release()
	stored, doc, err := c.read()
	if err != nil {
		return Token{}, err
	}
	if force || !stored.live(c.now(), c.leeway()) {
		refreshed, err := c.trade(ctx, stored)
		if err != nil {
			return Token{}, err
		}
		if err := hold.verify(); err != nil {
			return Token{}, fmt.Errorf("%w: another writer took the lock before the write: %w", ErrPersist, err)
		}
		if err := c.write(doc, refreshed); err != nil {
			return Token{}, fmt.Errorf("%w: %w", ErrPersist, err)
		}
		stored = refreshed
	}
	return Token{
		Access:    stored.Access,
		Expires:   time.UnixMilli(stored.Expires),
		AccountID: accountID(stored.Access, stored.AccountID),
	}, nil
}

// Status reports the credential the file holds, without trading it.
//
// A status is a report, so it writes nothing and refreshes nothing: a refresh
// would rotate the credential under whoever is using it. It takes no lock
// either, because a report should not wait on a refresh that is in flight.
func (c Credentials) Status() (Status, error) {
	if c.Path == "" {
		return Status{}, errors.New("piauth: no auth.json path")
	}
	stored, doc, err := c.read()
	if err != nil {
		return Status{}, err
	}
	return Status{
		Path:      doc.path,
		Format:    doc.format.name(),
		Provider:  ProviderCodex,
		AccountID: accountID(stored.Access, stored.AccountID),
		Expires:   time.UnixMilli(stored.Expires),
		Live:      stored.live(c.now(), c.leeway()),
	}, nil
}

// read returns the credential the file holds, and the document it came from.
//
// The document is what a write puts back, so a refresh keeps every other entry
// and field the file carried.
func (c Credentials) read() (credential, document, error) {
	doc, err := c.load()
	if err != nil {
		return credential{}, document{}, err
	}
	stored, err := decodeEntry(doc.path, ProviderCodex, doc.entries)
	if err != nil {
		return credential{}, document{}, err
	}
	return stored, doc, nil
}

// load reads the file and parses it with the shape it is written with.
//
// A file that is not there is a file without credentials, the way pi reads one,
// and it is read as poo's own shape: that is the file poo creates.
func (c Credentials) load() (document, error) {
	doc := document{path: c.Path, entries: map[string]json.RawMessage{}}
	data, err := os.ReadFile(c.Path)
	if errors.Is(err, fs.ErrNotExist) {
		doc.format = pooFormat{}
		return doc, nil
	}
	if err != nil {
		return document{}, fmt.Errorf("%w: reading %s: %w", ErrStorage, c.Path, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(stripBOM(data), &top); err != nil {
		return document{}, fmt.Errorf("%w: %s: %w", ErrStorage, c.Path, err)
	}
	doc.format = shapeOf(top)
	entries, err := doc.format.credentials(top)
	if err != nil {
		return document{}, fmt.Errorf("%w: %s: %w", ErrStorage, c.Path, err)
	}
	doc.entries = entries
	return doc, nil
}

// shapeOf returns the shape a file's object is written with.
//
// The file itself says which one it is: poo's own names a version, and pi's
// holds its providers at the top level. A file that says neither is pi's,
// which is the shape with nothing to name.
func shapeOf(top map[string]json.RawMessage) format {
	if _, ok := top[versionField]; ok {
		return pooFormat{}
	}
	return piFormat{}
}

// write stores a credential under the provider, and leaves the rest of the
// file as the document found it.
//
// A file created here gets mode 0600 and a directory created here 0700. A file
// that already exists keeps its own mode and follows its own symlinks, as it
// does through pi's own write. The write is in place, not a temporary file
// renamed over it.
//
// Writing in place is the trade pi makes too: a process that dies mid-write
// leaves a truncated file. The next read reports that as ErrStorage, and pi's
// login repairs it. A temporary file renamed over the path would make the
// bytes atomic. It would also replace a symlinked auth.json with a regular
// file, dropping the mode, ACLs and extended attributes the operator gave it.
func (c Credentials) write(doc document, entry credential) error {
	if err := encodeEntry(doc.path, ProviderCodex, doc.entries, entry); err != nil {
		return err
	}
	object, err := doc.format.envelope(doc.entries)
	if err != nil {
		return fmt.Errorf("%s: %v", doc.path, err)
	}
	body, err := marshalFile(doc.path, object)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return fmt.Errorf("%s: %v", c.Path, err)
	}
	if err := os.WriteFile(c.Path, body, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", c.Path, err)
	}
	return nil
}

// live reports whether an access token has enough life left to be used
// as it is.
func (c credential) live(now time.Time, leeway time.Duration) bool {
	return c.Access != "" && time.UnixMilli(c.Expires).After(now.Add(leeway))
}

// accountID names the account a token belongs to: the claim in the
// access token, and the accountId pi stored when the token carries none.
func accountID(access, stored string) string {
	if claim := claimAccountID(access); claim != "" {
		return claim
	}
	return stored
}

// claimAccountID reads the account out of a Codex access token.
//
// The token is a JWT and the account is a claim in its payload. A token
// that is not a JWT, or does not carry the claim, names no account here.
func claimAccountID(access string) string {
	parts := strings.Split(access, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	auth, ok := claims[accountClaim]
	if !ok {
		return ""
	}
	var account struct {
		AccountID string `json:"chatgpt_account_id"`
	}
	if err := json.Unmarshal(auth, &account); err != nil {
		return ""
	}
	return account.AccountID
}

// now is the clock the credential is aged by.
func (c Credentials) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// leeway is how much life an access token must have left to be used as
// it is.
func (c Credentials) leeway() time.Duration {
	if c.Leeway > 0 {
		return c.Leeway
	}
	return DefaultLeeway
}

// wait is how long a refresh waits for another writer's lock.
func (c Credentials) wait() time.Duration {
	if c.Wait > 0 {
		return c.Wait
	}
	return DefaultWait
}
