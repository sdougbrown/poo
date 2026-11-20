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
