package config

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/codex"
	"github.com/sdougbrown/poo/provider/piauth"
)

// codexModel reports whether the model is served by pi's built-in Codex
// provider.
//
// The provider is the settings' endpoint name, and nothing else infers it. A
// responses API on another endpoint is not Codex, nor is the Codex URL under
// another name. The name is pi's provider id, which is also the key pi stores
// the credential under.
func (r *Runtime) codexModel(model poo.ModelInfo) bool {
	entry, ok := r.Settings.Models[model.Name]
	return ok && entry.Endpoint == piauth.ProviderCodex
}

// codexTransport builds the Codex transport for a model.
//
// The endpoint is pi's built-in one. pi's credential is an OAuth token for that
// endpoint, and sending it elsewhere would hand it to a host the operator did
// not name. A model that points the provider elsewhere is refused, not sent.
func (r *Runtime) codexTransport(model poo.ModelInfo) (poo.ChatTransport, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(model.Endpoint), "/")
	if endpoint != codex.DefaultEndpoint {
		return nil, &Error{
			Code:  CodeUnsupported,
			Field: "endpoints." + piauth.ProviderCodex + ".endpoint",
			Message: fmt.Sprintf("the %s provider is served at %s; poo sends pi's credential only to %s",
				piauth.ProviderCodex, endpoint, codex.DefaultEndpoint),
		}
	}
	path := r.Settings.piAuthFile()
	if path == "" {
		return nil, &Error{
			Code:  CodeUnavailable,
			Field: "pi_auth_file",
			Message: "the Codex provider reads pi's auth.json, and the settings name none: " +
				"set pi_auth_file, or put auth.json beside the models file",
		}
	}
	return codex.New(codex.Options{
		Endpoint:    endpoint,
		Model:       model.ModelID,
		Credentials: codexCredentials(piauth.Credentials{Path: path, HTTP: r.httpClient}),
		ModelInfo:   model,
		ToolsMode:   string(model.Tools.Mode),
		Reasoning:   r.codexReasoning(model),
		HTTPClient:  r.httpClient,
	}), nil
}

// codexReasoning returns the reasoning level a Codex request falls back to.
//
// The settings' reasoning.chat is the operator's, and the session sends its own
// level on every request. A model that has no level which turns reasoning off
// would otherwise be asked for one by a transport with no setting. Its lowest
// level is the fallback.
func (r *Runtime) codexReasoning(model poo.ModelInfo) string {
	if r.Settings.Reasoning.Chat != nil {
		return *r.Settings.Reasoning.Chat
	}
	if model.Reasoning.Mode != poo.ReasoningEffort || len(model.ReasoningLevels) == 0 {
		return ""
	}
	if slices.Contains(model.ReasoningLevels, "off") {
		return ""
	}
	return model.ReasoningLevels[0]
}

// codexCredentials returns the resolver the Codex transport calls once per
// attempt, so a token that expires between attempts is refreshed rather than
// failing the run.
//
// piauth owns the read, the refresh and the write under pi's lock; this only
// hands the transport the token and the account the token names.
func codexCredentials(credentials piauth.Credentials) func(context.Context) (codex.Credential, error) {
	return func(ctx context.Context) (codex.Credential, error) {
		token, err := credentials.Access(ctx)
		if err != nil {
			return codex.Credential{}, codexCredentialError(ctx, err)
		}
		return codex.Credential{Token: token.Access, AccountID: token.AccountID}, nil
	}
}

// codexCredentialError classifies the failure a resolution reported.
//
// A traded token that could not be stored and a refresh whose answer is
// unknown are terminal. Another attempt would trade a refresh token that may
// already be spent. ErrPersist therefore wins over any lock in its chain — the
// lock is what stopped the write, not what makes the failure free to retry. A
// lock that stopped the refresh before anything was traded is the transient
// one, and a cancelled wait is the caller's cancellation.
func codexCredentialError(ctx context.Context, err error) error {
	// A resolution that ended with the caller's context did not fail on its
	// own: the transport reports the cancellation.
	if ctx.Err() != nil {
		return err
	}
	if errors.Is(err, piauth.ErrPersist) || errors.Is(err, piauth.ErrRefresh) {
		return &poo.ProviderError{Kind: poo.ProviderKindAuthFailed, Message: err.Error()}
	}
	if errors.Is(err, piauth.ErrLocked) {
		return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
	}
	return err
}
