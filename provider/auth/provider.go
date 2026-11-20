package auth

import "fmt"

// Kind is how a credential authenticates.
type Kind string

const (
	// KindOAuth is a token pair that expires and is refreshed by trading the refresh
	// token.
	KindOAuth Kind = "oauth"
	// KindAPIKey is a key that is sent as it is and does not expire.
	KindAPIKey Kind = "api_key"
)

// Provider is one provider as the store needs it: the id its credential is stored under,
// where its tokens are traded, and what its credentials must carry.
//
// The store knows no provider. Everything provider-specific a credential needs arrives
// here, so a second provider — an api key with no token endpoint and no account — is a
// Provider with those fields empty, not a change to the store.
type Provider struct {
	// ID is the entry's key in the auth file.
	ID string
	// Kind is the credential kind this provider accepts. Empty accepts a credential of
	// either kind, which is what a provider the store only copies needs to say.
	Kind Kind
	// TokenURL is where a refresh token is traded, and where a sign-in's code would be.
	// Empty means the credential has no tokens to trade: an api key.
	TokenURL string
	// Env names the environment variable a key credential can be read from. Empty means the
	// provider reads none.
	Env string
	// ClientID is the OAuth client the credential belongs to. Empty means DefaultClientID.
	ClientID string
	// Account names the account a credential belongs to, given its access token and the id
	// the entry stored. It returns "" when the credential names none, which is a credential
	// this provider cannot use.
	//
	// Nil means the provider's credentials carry no account, and none is required.
	Account func(access, stored string) string
}

// Codex is the OpenAI Codex provider: pi's client, the endpoint its refresh token is traded at,
// and the ChatGPT account its access tokens name. How a person signs in to it is CodexLogin.
var Codex = Provider{
	ID:       ProviderCodex,
	Kind:     KindOAuth,
	TokenURL: DefaultTokenURL,
	ClientID: DefaultClientID,
	Account:  chatGPTAccount,
}

// OllamaCloud is the Ollama Cloud provider: a key, read from the environment or from the
// credential `poo login` stores. It has no token endpoint and no account.
var OllamaCloud = Provider{ID: "ollama-cloud", Kind: KindAPIKey, Env: "OLLAMA_API_KEY"}

// Opencode is the OpenCode provider: a key the Console issues, read from the
// environment or from the credential `poo login` stores. The one key serves both
// the pay-as-you-go and the Go endpoints, which read the same environment
// variable: a key is bound to one workspace, and the workspace's plan says
// which endpoint bills it.
var Opencode = Provider{ID: ProviderOpencode, Kind: KindAPIKey, Env: "OPENCODE_API_KEY"}

// Typesafe is the TypeSafe provider: a key the console issues, read from the
// environment or from the credential `poo login` stores. It reaches the one
// decisions endpoint the built-in record declares, which serves no chat
// turns, so a key buys a decider and not a chat model.
var Typesafe = Provider{ID: ProviderTypesafe, Kind: KindAPIKey, Env: "TYPESAFE_API_KEY"}

// KeyProviders are the providers whose credential is a key, in the order a
// search reads them.
//
// Everything that walks the key providers — the endpoint's key, the auth
// status report, the built-in offer — reads this one list, so a provider
// added later is a declaration beside these and not a second list to keep
// in step.
func KeyProviders() []Provider {
	return []Provider{OllamaCloud, Opencode, Typesafe}
}

// validate reports what a credential is missing, or what it carries that its kind or its
// provider does not take.
//
// It is the one answer to "is this a usable credential for this provider", asked by the write
// that stores one and the read that returns one: two copies of the rule would agree until the
// day a kind or a required field was added to one of them.
//
// The reasons are plain, and the caller that reports one adds the sentinel and its own
// context: a path, for a file's entry.
func validate(c Credential, p Provider) error {
	kind := c.Kind
	if kind == "" {
		kind = KindOAuth
	}
	if p.Kind != "" && kind != p.Kind {
		return fmt.Errorf("the %s provider reads a %s credential, and this is a %s one", p.ID, p.Kind, kind)
	}
	switch kind {
	case KindAPIKey:
		if c.Key == "" {
			return fmt.Errorf("an api key credential needs a key")
		}
		if c.Access != "" || c.Refresh != "" || !c.Expires.IsZero() || c.AccountID != "" {
			return fmt.Errorf("an api key credential carries tokens")
		}
		if c.ClientID != "" || c.TokenURL != "" {
			return fmt.Errorf("an api key credential carries a client, and a key is not granted to one")
		}
	case KindOAuth:
		if c.Access == "" || c.Refresh == "" {
			return fmt.Errorf("an oauth credential needs an access token and a refresh token")
		}
		if c.Key != "" {
			return fmt.Errorf("an oauth credential carries a key")
		}
		if p.needsAccount() && p.account(c.Access, c.AccountID) == "" {
			return fmt.Errorf("the credential names no account")
		}
	default:
		return fmt.Errorf("%q is not a credential kind this reads", kind)
	}
	return nil
}

// account names the account a credential belongs to, by the provider's own rule.
//
// A provider with no rule, or one whose rule finds no account, names none.
func (p Provider) account(access, stored string) string {
	if p.Account == nil {
		return ""
	}
	return p.Account(access, stored)
}

// needsAccount reports whether the provider's credentials must name an account.
func (p Provider) needsAccount() bool {
	return p.Account != nil
}

// chatGPTAccount is the account a Codex access token names: the claim in the token, and the
// accountId pi stored when the token carries none.
func chatGPTAccount(access, stored string) string {
	if claim := claimAccountID(access); claim != "" {
		return claim
	}
	return stored
}
