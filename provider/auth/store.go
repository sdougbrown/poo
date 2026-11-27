// Package auth reads, refreshes and writes the credentials poo keeps in its own
// auth file.
//
// The file's shape and the store that keeps the credential alive are two things, and
// they are two things here. The store owns the lock, the read, the refresh and the
// write; a format owns what one file's bytes look like. There are three shapes:
// poo's, which holds a version and the credentials under it, pi's auth.json, which
// holds its providers at the top level, and the Codex CLI's own auth.json, which is
// read and never written back. The file itself says which one it is, so a
// caller hands over a path and nothing else.
//
// pi's shape is the compat one: poo reads it and writes it back as it found it, so pi
// keeps working with the same file, and the day poo's own import path is the only
// reader of pi's file this codec is what stays behind.
//
// The Codex CLI's own auth.json is the third shape, and it is never written
// back at all: the CLI owns the file and the refresh, and poo only reads it,
// in the mode a read-only store (ReadOnly) names.
//
// poo reads the file the settings name, never a pi installation's own
// directory, so a caller hands the path over. This package owns both the
// sign-in machinery and the store that keeps the credential alive. Access
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
package auth

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

	// ProviderOpencode is the provider id the OpenCode key is stored under. One
	// credential serves both the pay-as-you-go and the Go endpoints, because a
	// key belongs to one workspace and the workspace says which plan it runs.
	ProviderOpencode = "opencode"

	// ProviderTypesafe is the provider id the TypeSafe key is stored
	// under. It is the entry's name in poo's own credential file. The
	// endpoint reads the credential by declaring the same environment
	// variable, so the names meet there and nowhere else.
	ProviderTypesafe = "typesafe"

	// DefaultTokenURL is the endpoint pi trades a Codex refresh token at.
	DefaultTokenURL = "https://auth.openai.com/oauth/token"

	// DefaultClientID is the client pi's Codex grants belong to: the Codex CLI's public
	// client, which pi reuses.
	//
	// It is not poo's. A credential poo signs in records the client it was granted to, and
	// that record is what a refresh names; this is the fallback for a credential that records
	// none, which today means one pi granted.
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
	// FormatCodex is the Codex CLI's own auth.json: an api key beside the
	// tokens object the CLI's sign-in wrote.
	FormatCodex = "codex"
)

// The failures a caller tells apart.
var (
	// ErrNoCredential means the auth file holds no credential the provider can use. A file
	// with no such entry, an entry of the wrong kind, and an entry its provider cannot carry
	// — a Codex token that names no account — are all this failure. The answer is a sign-in,
	// not a retry.
	ErrNoCredential = errors.New("auth: no usable credential")

	// ErrLocked means another writer holds the auth.json's lock, so the
	// refresh did not start and nothing was traded. The file is exactly what
	// it was, and a caller may retry. pi holds the lock while it refreshes,
	// so this is what a pi refresh in flight looks like.
	ErrLocked = errors.New("auth: the auth.json's lock is held by another writer")

	// ErrRefresh means a provider's token endpoint would not trade the refresh
	// token. It refused the grant, answered with something that is not one,
	// or could not be reached. The file is unchanged, which is not to say
	// its credential still works. The grant the endpoint refused is the one
	// the file holds, so a retry most likely fails the same way.
	ErrRefresh = errors.New("auth: the token refresh failed")

	// ErrSignIn means a sign-in failed: the authorization was refused, the
	// callback did not answer, or the token endpoint would not trade the code.
	// It is not a refresh failure: the credential being signed in for does not
	// exist yet.
	ErrSignIn = errors.New("auth: the sign-in failed")

	// ErrStorage means the auth.json could not be read, or could not be made ready to
	// write, before anything was traded. Nothing was written either.
	ErrStorage = errors.New("auth: the auth.json could not be read")

	// ErrReadOnly means the credential file is one poo reads and never writes,
	// and the access poo was asked for is one a refresh would have to make.
	// Nothing was traded and nothing was written: the file is what it was.
	// Refreshing the credential is the tool that owns the file's job.
	ErrReadOnly = errors.New("auth: the credential file is read-only")

	// ErrForeignFile means the file is another tool's: pi's, or the Codex CLI's, in the
	// shape it was read as. poo writes its own, and a command that rewrote another tool's
	// file would be changing a file that tool reads.
	ErrForeignFile = errors.New("auth: the auth file belongs to another tool")

	// ErrExists means the file already holds a credential for the provider, and
	// the caller did not ask for it to be replaced. A command that replaced a
	// working credential without being asked is a logout nobody wanted.
	ErrExists = errors.New("auth: the auth file already holds a credential")

	// ErrPersist means the endpoint traded the refresh token and the new
	// credential could not be stored. The file still holds the refresh token
	// that was traded, so it may no longer work. The caller's next step is
	// pi's login, not a retry of the same credential.
	ErrPersist = errors.New("auth: the refreshed token was not stored")
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

// Credential is a credential to store: what a sign-in or an import hands over, in poo's own
// words.
//
// One kind uses one set of fields: an oauth credential has tokens, and an api key has a key.
// A field the kind does not use is refused rather than stored, so a credential that names
// two kinds at once cannot be written.
type Credential struct {
	// Kind is how the credential authenticates. Empty is an oauth credential: a caller
	// that has a token pair in hand has the common case, and the file's entry spells the
	// kind it was written with.
	Kind Kind
	// Access is the access token, for an oauth credential.
	Access string
	// Refresh is the refresh token, for an oauth credential.
	Refresh string
	// Expires is when Access stops working, for an oauth credential.
	Expires time.Time
	// AccountID is the account the access token names, for an oauth credential of a
	// provider that names accounts.
	AccountID string
	// Key is the key itself, for an api key.
	Key string
	// ClientID is the OAuth client the credential was granted to, when the caller knows it.
	// Empty falls back to the provider's client, which covers entries that record none —
	// older files, and pi's own credential format, which carries no client.
	ClientID string
	// TokenURL is where that client trades its tokens, when it is not the provider's.
	TokenURL string
}

// check reports what a credential is missing, as the write that stores one reports it.
func (c Credential) check(p Provider) error {
	if err := validate(c, p); err != nil {
		return fmt.Errorf("%w: %v", ErrNoCredential, err)
	}
	return nil
}

// Holds reports whether the file holds an entry for the provider, whatever kind it is.
//
// It is the question the replace rule asks: an entry this package cannot decode is still an
// entry, so replacing it is still a decision, and Status is the wrong question to ask of it.
func (c Credentials) Holds(provider string) (bool, error) {
	if c.Path == "" {
		return false, errors.New("auth: no auth.json path")
	}
	doc, err := c.load()
	if err != nil {
		return false, err
	}
	_, ok := doc.entries[provider]
	return ok, nil
}

// Read returns the credential the file holds, without trading it.
//
// It is the read a caller that copies a credential makes: no refresh, no write and no
// lock. A caller that needs a live token asks Access, and one that reports on the
// credential asks Status.
func (c Credentials) Read(p Provider) (Credential, error) {
	if c.Path == "" {
		return Credential{}, errors.New("auth: no auth.json path")
	}
	stored, _, err := c.read(p)
	if err != nil {
		return Credential{}, err
	}
	return stored.export(), nil
}

// Set stores a credential under the provider.
//
// The write is a refresh's write — the same lock, the same file, every other entry
// kept — so a login and a refresh cannot interleave. replace says the caller has
// decided to replace a credential the file already holds; without it, a file that
// holds one is ErrExists and nothing is written.
//
// A credential that cannot be used is refused here rather than stored: an entry
// with no token, or one whose access token names no account, is one the next read
// would refuse anyway. Only poo's own file takes a write: pi's is pi's, and a
// command that rewrote it would be changing a file another tool owns.
func (c Credentials) Set(ctx context.Context, p Provider, entry Credential, replace bool) (Status, error) {
	if c.Path == "" {
		return Status{}, errors.New("auth: no auth.json path")
	}
	if err := entry.check(p); err != nil {
		return Status{}, err
	}
	return c.writeLocked(ctx, true, func(doc document) (Status, bool, error) {
		// Presence, not decodability: an entry of a kind this package cannot read is still an
		// entry, and replacing it without being asked is the thing ErrExists exists to prevent.
		if _, held := doc.entries[p.ID]; held && !replace {
			return Status{}, false, fmt.Errorf("%w: %s holds a %s credential", ErrExists, doc.path, p.ID)
		}
		stored := entry.internal(p)
		if err := encodeEntry(doc.path, p.ID, doc.entries, stored); err != nil {
			return Status{}, false, err
		}
		return c.status(doc, p, stored), true, nil
	})
}

// Delete removes the provider's entry, and leaves every other entry as it was.
//
// A provider the file does not hold is not a failure: the caller asked for a state, and the
// file is in it.
func (c Credentials) Delete(ctx context.Context, provider string) error {
	if c.Path == "" {
		return errors.New("auth: no auth.json path")
	}
	_, err := c.writeLocked(ctx, true, func(doc document) (Status, bool, error) {
		if _, held := doc.entries[provider]; !held {
			return Status{}, false, nil
		}
		delete(doc.entries, provider)
		return Status{}, true, nil
	})
	return err
}

// writeLocked runs one write under the lock: the directory, the lock, the file, the refusal of a
// file another tool owns, the change, the lock's last word, and the write.
//
// Every write this package makes goes through it, because a check added to one path and not
// another is a write into pi's file, or one under a lock another writer took. mutate gets the
// document under the lock and returns what the caller reports and whether the document changed;
// an error writes nothing, and so does changed false — an access that found a live token must
// not rewrite the file.
//
// own says the write is poo's: a sign-in, an import, a logout. Those refuse a file another tool
// owns. A refresh is not one of them — it writes back the shape it read, which is how pi keeps
// working with a credential poo refreshed — so it passes false.
func (c Credentials) writeLocked(ctx context.Context, own bool, mutate func(document) (Status, bool, error)) (Status, error) {
	if own && c.ReadOnly {
		return Status{}, fmt.Errorf("%w: poo does not write its credential file", ErrReadOnly)
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		// Nothing was traded: the file could not be made ready to write.
		return Status{}, fmt.Errorf("%w: %s: %w", ErrStorage, filepath.Dir(c.Path), err)
	}
	hold, err := acquire(ctx, c.Path, c.wait())
	if err != nil {
		return Status{}, err
	}
	defer hold.release()
	doc, err := c.load()
	if err != nil {
		return Status{}, err
	}
	if own {
		if _, ok := doc.format.(pooFormat); !ok {
			return Status{}, fmt.Errorf("%w: %s is the %s file, and poo writes its own", ErrForeignFile, doc.path, doc.format.name())
		}
	}
	status, changed, err := mutate(doc)
	if err != nil {
		return Status{}, err
	}
	if !changed {
		return status, nil
	}
	if err := hold.verify(); err != nil {
		return Status{}, fmt.Errorf("%w: another writer took the lock before the write: %w", ErrPersist, err)
	}
	if err := c.persist(doc); err != nil {
		return Status{}, fmt.Errorf("%w: %w", ErrPersist, err)
	}
	return status, nil
}

// status is the report of one credential in the document it was read from.
func (c Credentials) status(doc document, p Provider, stored credential) Status {
	status := Status{
		Path:      doc.path,
		Format:    doc.format.name(),
		Provider:  p.ID,
		Kind:      stored.Kind(),
		AccountID: p.account(stored.Access, stored.AccountID),
	}
	// A credential with no tokens has no expiry: an api key does not stop working on its
	// own, and a report that said it expired in 1969 would be worse than one that said
	// nothing.
	if status.Kind == KindOAuth {
		status.ClientID = stored.ClientID
		// An access token whose expiry is not readable has none to report: a
		// time rendered for it would be a time nothing enforces, and the
		// report's own rule for a key is the rule here.
		if stored.Expires != 0 {
			status.Expires = time.UnixMilli(stored.Expires)
		}
		status.Live = stored.live(c.now(), c.readiness())
	}
	return status
}

// Status is what a credential file holds, as read: the credential, and never
// the token. A caller that reports on the credential asks for this; a caller
// that sends it asks for Access.
type Status struct {
	// Path is the file the credential was read from.
	Path string
	// Format is the shape that file is written with: FormatPi, FormatPoo or
	// FormatCodex.
	Format string
	// Provider is the id the credential is stored under.
	Provider string
	// Kind is how the credential authenticates: an oauth token pair, or an api key.
	Kind Kind
	// ClientID is the OAuth client the credential was granted to, when the file records one.
	ClientID string
	// AccountID is the account the access token names, for a provider that names accounts.
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
	// marshal returns the file's bytes for an object the envelope built.
	marshal(path string, object map[string]json.RawMessage) ([]byte, error)
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
	// Type is how the entry authenticates: KindOAuth or KindAPIKey, as the file spells it.
	Type string
	// Access is the access token, for an oauth entry.
	Access string
	// Refresh is the refresh token, for an oauth entry.
	Refresh string
	// Expires is when Access stops working, in milliseconds since the epoch.
	Expires int64
	// AccountID is the account the access token names, for a provider that names one.
	AccountID string
	// Key is the key itself, for an api_key entry.
	Key string
	// ClientID is the OAuth client the credential was granted to.
	ClientID string
	// TokenURL is where that client trades its tokens.
	TokenURL string
}

// Credentials are the credentials in one auth file, by provider id, and the protocol that
// keeps them alive.
type Credentials struct {
	// Path is the auth.json to read and write. It is the one field a
	// caller must set.
	Path string
	// TokenURL overrides the endpoint the refresh token is traded at.
	TokenURL string
	// ClientID overrides the OAuth client the credential belongs to.
	ClientID string
	// HTTP supplies the transport and the timeout the token paths go
	// through. Its redirect policy is not used: refusingClient's is, shared
	// by every token path, because a token request's body holds a code or a
	// refresh token. The request's own bound is the context's, so a client
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
	// ReadOnly makes the file a store poo reads and never writes: no refresh
	// is traded, and no sign-in, import or logout goes through. An access
	// token past its expiry is an error, not a trade — keeping the credential
	// alive is the tool that owns the file's job. It is the mode for a file
	// another tool owns, the Codex CLI's auth.json shared with poo.
	ReadOnly bool
}

// Access returns the credential's access token, refreshing one that has expired or is close
// enough to expiry that pi would refresh it too.
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
func (c Credentials) Access(ctx context.Context, p Provider) (Token, error) {
	return c.token(ctx, p, false)
}

// Refresh trades the credential's refresh token for a new one, whether the
// stored access token is live or not. A caller that needs the credential
// replaced, rather than used, calls this.
//
// The trade is Access's trade: the same lock, the same request, and the same
// write that keeps every other entry and field. The failures are the same.
// Concurrent calls each refresh in turn. The second uses the refresh token
// stored by the first.
func (c Credentials) Refresh(ctx context.Context, p Provider) (Token, error) {
	if c.ReadOnly {
		return Token{}, fmt.Errorf("%w: poo does not trade its credential", ErrReadOnly)
	}
	return c.token(ctx, p, true)
}

// readiness is how much life a credential must have left to be used or
// reported as live.
//
// A file poo refreshes uses pi's leeway: a token pi would refresh is one poo
// refreshes too. A read-only file has no refresh to front-run, so a token is
// live until it has expired.
func (c Credentials) readiness() time.Duration {
	if c.ReadOnly {
		return 0
	}
	return c.leeway()
}

// oauthToken turns a stored credential into the token a caller sends, and
// refuses one that is not oauth. Both token paths — the trade and the
// read-only read — answer through it, so the two cannot drift: a field the
// Token grows, or a rule the refusal changes, lands once.
func (c Credentials) oauthToken(p Provider, stored credential) (Token, error) {
	if stored.Kind() != KindOAuth {
		return Token{}, fmt.Errorf("%w: %s holds a %s credential, and a token is not what it has",
			ErrNoCredential, p.ID, stored.Kind())
	}
	return Token{
		Access:    stored.Access,
		Expires:   time.UnixMilli(stored.Expires),
		AccountID: p.account(stored.Access, stored.AccountID),
	}, nil
}

// readOnlyToken returns the credential the file holds, as it is.
//
// The read holds the lock even though nothing is written: the owner of the
// file writes in place, and a read in that window would see a truncated file.
// The lock is the owner's own, so the wait is the owner finishing its write,
// not a trade poo could make. The credential is used until its access token
// has expired, with no leeway — there is no refresh to front-run, and a token
// the file holds is one to send. An expired one is the failure a caller
// cannot get past on its own, reported as ErrReadOnly with the moment the
// token stopped working.
func (c Credentials) readOnlyToken(ctx context.Context, p Provider) (Token, error) {
	// A file that is not there is not mid-write, and a lock guards nothing:
	// the read answers no credential, the way every other path does, instead
	// of a lock error about a directory that does not exist.
	if _, err := os.Stat(c.Path); !errors.Is(err, fs.ErrNotExist) {
		hold, err := acquire(ctx, c.Path, c.wait())
		if err != nil {
			return Token{}, err
		}
		defer hold.release()
	}
	stored, _, err := c.read(p)
	if err != nil {
		return Token{}, err
	}
	if !stored.live(c.now(), 0) {
		if stored.Expires == 0 {
			return Token{}, fmt.Errorf("%w: the %s credential's access token names no expiry", ErrReadOnly, p.ID)
		}
		return Token{}, fmt.Errorf("%w: the %s credential's access token expired at %s; refresh it with the tool that owns the file",
			ErrReadOnly, p.ID, time.UnixMilli(stored.Expires).UTC().Format(time.RFC3339))
	}
	return c.oauthToken(p, stored)
}

// token reads the credential, trades it when force says so or when it is not
// live, and writes the traded one back. The whole of it is one critical
// section under pi's lock.
func (c Credentials) token(ctx context.Context, p Provider, force bool) (Token, error) {
	if c.Path == "" {
		return Token{}, errors.New("auth: no auth.json path")
	}
	if c.ReadOnly {
		return c.readOnlyToken(ctx, p)
	}
	var token Token
	_, err := c.writeLocked(ctx, false, func(doc document) (Status, bool, error) {
		stored, err := decodeEntry(doc.path, p, doc.entries)
		if err != nil {
			return Status{}, false, err
		}
		// The refusal of a credential that is not oauth comes before the trade:
		// the endpoint is not asked for a token an api key cannot be.
		if _, err := c.oauthToken(p, stored); err != nil {
			return Status{}, false, err
		}
		changed := false
		if force || !stored.live(c.now(), c.leeway()) {
			// A file the format never writes back is refused before the trade, not
			// after it: a trade the write cannot record would spend the refresh
			// token and leave the file holding the spent one.
			if _, isCodex := doc.format.(codexFormat); isCodex {
				return Status{}, false, fmt.Errorf("%w: %s is the Codex CLI's auth.json, and poo does not refresh it: set auth_readonly to share the file, and let the CLI keep the credential alive", ErrForeignFile, doc.path)
			}
			refreshed, err := c.trade(ctx, p, stored)
			if err != nil {
				return Status{}, false, err
			}
			if err := encodeEntry(doc.path, p.ID, doc.entries, refreshed); err != nil {
				return Status{}, false, fmt.Errorf("%w: %w", ErrPersist, err)
			}
			stored = refreshed
			changed = true
		}
		token, err = c.oauthToken(p, stored)
		if err != nil {
			return Status{}, false, err
		}
		return c.status(doc, p, stored), changed, nil
	})
	if err != nil {
		return Token{}, err
	}
	return token, nil
}

// Status reports the credential the file holds, without trading it.
//
// A status is a report, so it writes nothing and refreshes nothing: a refresh
// would rotate the credential under whoever is using it. It takes no lock
// either, because a report should not wait on a refresh that is in flight.
func (c Credentials) Status(p Provider) (Status, error) {
	if c.Path == "" {
		return Status{}, errors.New("auth: no auth.json path")
	}
	stored, doc, err := c.read(p)
	if err != nil {
		return Status{}, err
	}
	return c.status(doc, p, stored), nil
}

// Entries reads an auth file of either shape and encoding and returns the
// credentials it holds, by provider id, as the bytes each entry carries.
//
// It is the reader for a caller that reads the file and is not the store: the
// models file's providers take their api keys from the same file, and one reader
// means one answer for which shape and which encoding the file is written with. A
// file that is not there holds no credentials.
func Entries(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s: %w", ErrStorage, path, err)
	}
	doc, err := documentOf(path, data)
	if err != nil {
		return nil, err
	}
	return doc.entries, nil
}

// read returns the credential the file holds, and the document it came from.
//
// The document is what a write puts back, so a refresh keeps every other entry
// and field the file carried.
func (c Credentials) read(p Provider) (credential, document, error) {
	doc, err := c.load()
	if err != nil {
		return credential{}, document{}, err
	}
	stored, err := decodeEntry(doc.path, p, doc.entries)
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
	return documentOf(c.Path, data)
}

// documentOf parses a file that is there: the shape it says it is, and the
// credentials that shape holds.
func documentOf(path string, data []byte) (document, error) {
	doc := document{path: path}
	top, err := readObject(data)
	if err != nil {
		return document{}, fmt.Errorf("%w: %s: %w", ErrStorage, path, err)
	}
	doc.format = shapeOf(top)
	entries, err := doc.format.credentials(top)
	if err != nil {
		return document{}, fmt.Errorf("%w: %s: %w", ErrStorage, path, err)
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
	if claimsCodexShape(top) {
		return codexFormat{}
	}
	return piFormat{}
}

// persist writes the document back: the entries as they are now, in the shape the file was
// read with.
//
// A file created here gets mode 0600 and a directory created here 0700. A file that already
// exists keeps its own mode and follows its own symlinks, as it does through pi's own write.
// The write is in place, not a temporary file renamed over it.
//
// Writing in place is the trade pi makes too: a process that dies mid-write leaves a
// truncated file. The next read reports that as ErrStorage, and pi's login repairs it. A
// temporary file renamed over the path would make the bytes atomic. It would also replace a
// symlinked auth.json with a regular file, dropping the mode, ACLs and extended attributes
// the operator gave it.
func (c Credentials) persist(doc document) error {
	object, err := doc.format.envelope(doc.entries)
	if err != nil {
		return fmt.Errorf("%s: %v", doc.path, err)
	}
	body, err := doc.format.marshal(doc.path, object)
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

// jwtPayload reads the claims of a JWT, as the payload spelled them. A token
// that is not a JWT, or whose payload does not decode, names none.
func jwtPayload(token string) (map[string]json.RawMessage, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, false
	}
	return claims, true
}

// claimAccountID reads the account out of a Codex access token.
//
// The account is a claim in the payload. A token that names no claim names
// no account here.
func claimAccountID(access string) string {
	claims, ok := jwtPayload(access)
	if !ok {
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
