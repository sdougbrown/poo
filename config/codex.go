package config

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/auth"
	"github.com/sdougbrown/poo/provider/codex"
)

// codexTransport builds the Codex transport for a model.
//
// The endpoint is pi's built-in one. pi's credential is an OAuth token for that
// endpoint, and sending it elsewhere would hand it to a host the operator did
// not name. A model that points the provider elsewhere is refused, not sent.
//
// The model has to say it speaks the Responses API. The session reads the API
// for two things a Codex model depends on — whether the history's provider
// blocks stay in a request, and whether the response's reasoning is recorded
// with the message — so a model declared under this endpoint without the API
// would run on this transport while the session stripped the reasoning the
// endpoint cannot do without.
func (r *Runtime) codexTransport(model poo.ModelInfo) (poo.ChatTransport, error) {
	if model.API != poo.APIResponses {
		return nil, &Error{
			Code:  CodeUnsupported,
			Field: "models." + model.Name + ".api",
			Message: fmt.Sprintf("the %s provider speaks the %s API, and the model declares %s: set api: %s",
				auth.ProviderCodex, poo.APIResponses, model.API, poo.APIResponses),
		}
	}
	endpoint := strings.TrimRight(strings.TrimSpace(model.Endpoint), "/")
	if endpoint != codex.DefaultEndpoint {
		return nil, &Error{
			Code:  CodeUnsupported,
			Field: "endpoints." + auth.ProviderCodex + ".endpoint",
			Message: fmt.Sprintf("the %s provider is served at %s; poo sends pi's credential only to %s",
				auth.ProviderCodex, endpoint, codex.DefaultEndpoint),
		}
	}
	path := r.Settings.CredentialFile()
	if path == "" {
		return nil, &Error{
			Code:  CodeUnavailable,
			Field: "auth_file",
			Message: "the Codex provider reads a credential file, and the settings name none: " +
				"set auth_file, or pi_auth_file for pi's auth.json",
		}
	}
	return codex.New(codex.Options{
		Endpoint:    endpoint,
		Model:       model.ModelID,
		Credentials: codexCredentials(auth.Credentials{Path: path, HTTP: r.httpClient}),
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
// The auth store owns the read, the refresh and the write under pi's lock; this only
// hands the transport the token and the account the token names.
func codexCredentials(credentials auth.Credentials) func(context.Context) (codex.Credential, error) {
	return func(ctx context.Context) (codex.Credential, error) {
		token, err := credentials.Access(ctx, auth.Codex)
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
	if errors.Is(err, auth.ErrPersist) || errors.Is(err, auth.ErrRefresh) {
		return &poo.ProviderError{Kind: poo.ProviderKindAuthFailed, Message: err.Error()}
	}
	if errors.Is(err, auth.ErrLocked) {
		return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
	}
	return err
}
