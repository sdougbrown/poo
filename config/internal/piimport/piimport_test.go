package piimport_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo/config/internal/piimport"
)

// TestAPisFilesThatOpenWithABOMAreRead covers the files a Windows editor
// writes. pi strips the byte order mark before parsing, and this reader strips
// it from the models file and from the auth file: one file must not be readable
// by pi and refused by poo, on either path.
func TestAPisFilesThatOpenWithABOMAreRead(t *testing.T) {
	dir := t.TempDir()
	models := filepath.Join(dir, "models.json")
	if err := os.WriteFile(models, []byte("\ufeff"+`{
		"providers": {
			"home": {
				"baseUrl": "http://grace:4000/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	auth := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(auth, []byte("\ufeff"+`{"home": {"type": "api_key", "key": "test-key-123"}}`), 0o600); err != nil {
		t.Fatalf("writing the auth file: %v", err)
	}
	file, err := piimport.Read(models)
	if err != nil {
		t.Fatalf("reading the models file: %v", err)
	}
	provider := file.Providers["home"]
	if provider.APIKey != "test-key-123" {
		t.Errorf("the provider imported with the key %q, want the one the file holds", provider.APIKey)
	}
}
