package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/provider/auth"
	"github.com/sdougbrown/poo/provider/catalog"
)

// TestTheEndpointKeyRungs covers the rungs the endpoint key's resolution
// reads, in the order the operator's intent names: the environment
// variable first, the credential poo's file holds second, the literal the
// settings carry last. The presence answer and the key itself read the
// same rungs, so a rung that serves a key serves a model, and a rung that
// serves none refuses it.
func TestTheEndpointKeyRungs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("POO_SETTINGS", "")

	authPath, err := config.DefaultAuthFile()
	if err != nil {
		t.Skipf("the default credential file is not resolvable here: %v", err)
	}
	credentials := auth.Credentials{Path: authPath}
	if _, err := credentials.Set(context.Background(), auth.Opencode, auth.Credential{Kind: auth.KindAPIKey, Key: "stored-key"}, true); err != nil {
		t.Fatalf("writing the credential fixture: %v", err)
	}
	settings := &config.Settings{
		Version: 1,
		Endpoints: map[string]config.Endpoint{
			"opencode": {APIKey: "literal-key"},
		},
	}
	credential := catalog.Credential{Source: catalog.CredentialEnv, Env: "OPENCODE_API_KEY"}

	// The variable is the answer the process was told to use, and it
	// outranks the stored credential and the literal.
	t.Setenv("OPENCODE_API_KEY", "env-key")
	if key := settings.EndpointKey("opencode", credential); key != "env-key" {
		t.Errorf("the key with the variable set is %q, want the variable's", key)
	}
	t.Setenv("OPENCODE_API_KEY", "")

	// The stored credential outranks the literal.
	if key := settings.EndpointKey("opencode", credential); key != "stored-key" {
		t.Errorf("the key with the credential stored is %q, want the file's", key)
	}
	if !settings.EndpointKeyPresent("opencode", credential) {
		t.Errorf("the stored credential does not present the key")
	}

	// The literal is the last rung: a file that holds nothing leaves it.
	if err := os.Remove(authPath); err != nil {
		t.Fatalf("removing the credential fixture: %v", err)
	}
	if key := settings.EndpointKey("opencode", credential); key != "literal-key" {
		t.Errorf("the key with no credential stored is %q, want the literal's", key)
	}

	// A credential entry whose key is empty serves nothing: the presence
	// answer and the key's own agree that the rung holds nothing. The entry
	// is written as the file spells it, since a write checks the key. The
	// settings name no literal, so no rung answers for the endpoint.
	entry := "version: 1\ncredentials:\n  opencode:\n    key: \"\"\n    type: api_key\n"
	if err := os.WriteFile(authPath, []byte(entry), 0o600); err != nil {
		t.Fatalf("writing the empty-key fixture: %v", err)
	}
	bare := &config.Settings{Version: 1}
	if key := bare.EndpointKey("opencode", credential); key != "" {
		t.Errorf("the key with an empty stored key is %q, want none", key)
	}
	if bare.EndpointKeyPresent("opencode", credential) {
		t.Errorf("an empty stored key presents a key")
	}

	// An endpoint that reads no key is served as it stands.
	none := settings.EndpointKeyPresent("typesafe", catalog.Credential{Source: catalog.CredentialNone})
	if !none {
		t.Errorf("an endpoint that reads no key is not present")
	}
}

// TestTheBuiltinOllamaCloudEndpointIsOfferedOnItsKey covers the built-in hosted
// provider the record declares: the endpoint is offered once the key it reads is
// in reach, under the provider's own name, and the model the record names is one a
// role can pick before anything is probed — a list is the provider's answer, and a
// catalog resolves a chat role without one. A credential for another provider
// leaves it unoffered, and a key in the environment wakes it the same way.
func TestTheBuiltinOllamaCloudEndpointIsOfferedOnItsKey(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "")
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	settingsPath := filepath.Join(dir, "settings.yaml")
	body := "version: 1\nauth_file: " + authPath + "\nsession:\n  mode: ephemeral\n"
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	if err := os.WriteFile(authPath, []byte("version: 1\ncredentials: {}\n"), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}
	settings, err := config.Load(settingsPath)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if _, ok := catalog.New(settings.CatalogOptions()).Endpoints()["ollama-cloud"]; ok {
		t.Errorf("the endpoint is offered with no key in reach")
	}

	// The key the operator signs in with is the one the endpoint reads, and the
	// credential file is the rung that answers.
	credentials := auth.Credentials{Path: authPath}
	if _, err := credentials.Set(context.Background(), auth.OllamaCloud,
		auth.Credential{Kind: auth.KindAPIKey, Key: "the-stored-key"}, true); err != nil {
		t.Fatalf("writing the credential fixture: %v", err)
	}
	settings, err = config.Load(settingsPath)
	if err != nil {
		t.Fatalf("loading the settings again: %v", err)
	}
	endpoint, ok := catalog.New(settings.CatalogOptions()).Endpoints()["ollama-cloud"]
	if !ok {
		t.Fatalf("the endpoint is not offered with its key stored")
	}
	if endpoint.Endpoint != "https://ollama.com/v1" {
		t.Errorf("the endpoint is %q, want the record's Ollama Cloud one", endpoint.Endpoint)
	}
	if endpoint.Credential.Source != catalog.CredentialEnv || endpoint.Credential.Env != "OLLAMA_API_KEY" {
		t.Errorf("the credential reads %+v, want the record's variable", endpoint.Credential)
	}
	model, err := catalog.New(settings.CatalogOptions()).Resolve("ollama-cloud/minimax-m3.1-flash")
	if err != nil {
		t.Fatalf("the model the record declares did not resolve: %v", err)
	}
	if model.Provider != "ollama-cloud" {
		t.Errorf("the model is served by %q, want the endpoint's name", model.Provider)
	}

	// The variable is the rung above the file, so a key set in the environment wakes
	// the endpoint with no credential stored.
	if err := os.Remove(authPath); err != nil {
		t.Fatalf("removing the credential fixture: %v", err)
	}
	if err := os.WriteFile(authPath, []byte("version: 1\ncredentials: {}\n"), 0o600); err != nil {
		t.Fatalf("writing the empty credential file: %v", err)
	}
	t.Setenv("OLLAMA_API_KEY", "the-env-key")
	settings, err = config.Load(settingsPath)
	if err != nil {
		t.Fatalf("loading the settings with the variable set: %v", err)
	}
	if _, err := catalog.New(settings.CatalogOptions()).Resolve("ollama-cloud/minimax-m3.1-flash"); err != nil {
		t.Errorf("the endpoint's model did not resolve on the variable: %v", err)
	}
}

// TestTheProviderKeyJoinIsOneRule covers the join a sign-in and a key's resolution both
// ask: which endpoints read the key a provider stores. It is one answer, so a sign-in
// cannot report that a provider serves nothing while the key resolution finds the key.
func TestTheProviderKeyJoinIsOneRule(t *testing.T) {
	reads := catalog.Credential{Source: catalog.CredentialEnv, Env: "OLLAMA_API_KEY"}
	if !config.ReadsProviderKey(reads, auth.OllamaCloud) {
		t.Errorf("the endpoint reading %s does not read the key the Ollama Cloud sign-in stores", reads.Env)
	}
	for _, other := range []struct {
		name       string
		credential catalog.Credential
		provider   auth.Provider
	}{
		{"another provider's variable", reads, auth.Opencode},
		{"a static key", catalog.Credential{Source: catalog.CredentialStatic}, auth.OllamaCloud},
		{"an endpoint that reads no key", catalog.Credential{Source: catalog.CredentialNone}, auth.OllamaCloud},
	} {
		if config.ReadsProviderKey(other.credential, other.provider) {
			t.Errorf("the join reports %s as the %s provider's", other.name, other.provider.ID)
		}
	}
}
