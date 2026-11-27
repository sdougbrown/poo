package config

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/auth"
	"github.com/sdougbrown/poo/provider/interactions"
)

// GooglePlanEndpoint is where the plan's models are served: the Interactions
// API's own path with the quota method beside it.
//
// The built-in record declares the same URL, and a model that points the
// provider elsewhere is refused: poo holds the sign-in's access token, and
// sending it to a host the operator did not name is the one mistake this
// constant prevents.
const GooglePlanEndpoint = "https://generativelanguage.googleapis.com/v1beta/interactions:createPerUserQuota"

// googlePlanTransport builds the Interactions transport for a plan model.
//
// The credential is the person's Google AI plan sign-in, and the token is
// resolved once per attempt: a token that expired between two attempts is traded
// for a new one rather than failing the run.
func (r *Runtime) googlePlanTransport(model poo.ModelInfo) (poo.ChatTransport, error) {
	if model.API != poo.APIGoogleInteractions {
		return nil, &Error{
			Code:  CodeUnsupported,
			Field: "models." + model.Name + ".api",
			Message: fmt.Sprintf("the %s provider speaks the %s API, and the model declares %s: set api: %s",
				auth.ProviderGoogleAIPlan, poo.APIGoogleInteractions, model.API, poo.APIGoogleInteractions),
		}
	}
	endpoint := strings.TrimRight(strings.TrimSpace(model.Endpoint), "/")
	if endpoint != GooglePlanEndpoint {
		return nil, &Error{
			Code:  CodeUnsupported,
			Field: "endpoints." + auth.ProviderGoogleAIPlan + ".endpoint",
			Message: fmt.Sprintf("the %s provider is served at %s; poo sends its credential only to %s",
				auth.ProviderGoogleAIPlan, endpoint, GooglePlanEndpoint),
		}
	}
	// The client's secret is read where the operator put it, on every run: the
	// grant records the client's id, and the half that is never stored with it
	// is this one.
	client, err := auth.GoogleClientFromEnv()
	if err != nil {
		return nil, &Error{
			Code:    CodeUnavailable,
			Field:   auth.GoogleClientIDEnv,
			Message: err.Error(),
		}
	}
	path := r.Settings.CredentialFile()
	if path == "" {
		return nil, &Error{
			Code:    CodeUnavailable,
			Field:   "auth_file",
			Message: "the Google AI plan reads a credential file, and the settings name none: set auth_file",
		}
	}
	credentials := auth.Credentials{Path: path, HTTP: r.httpClient, ReadOnly: r.Settings.AuthReadonlyEnabled()}
	provider := auth.GoogleAIPlan(client)
	return interactions.New(interactions.Options{
		Endpoint:   endpoint,
		Token:      googlePlanToken(credentials, provider),
		ModelInfo:  model,
		HTTPClient: r.httpClient,
		UserAgent:  r.Build.UserAgent(),
	}), nil
}

// googlePlanToken resolves the token one attempt carries, and classifies a
// resolution that failed.
//
// A resolution that ended with the caller's context did not fail on its own. A
// refresh that cannot be traded — an expired grant, a revoked one, a credential
// with no key — is the person signing in again, and it is terminal: another
// attempt would trade the same spent grant. A lock another writer holds is
// transient, and a retry is what the retry policy is for.
func googlePlanToken(credentials auth.Credentials, provider auth.Provider) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		token, err := credentials.Access(ctx, provider)
		if err != nil {
			if ctx.Err() != nil {
				return "", err
			}
			if errors.Is(err, auth.ErrPersist) || errors.Is(err, auth.ErrRefresh) || errors.Is(err, auth.ErrReadOnly) {
				return "", &poo.ProviderError{Kind: poo.ProviderKindAuthFailed, Message: err.Error()}
			}
			if errors.Is(err, auth.ErrLocked) {
				return "", &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
			}
			return "", err
		}
		return token.Access, nil
	}
}
