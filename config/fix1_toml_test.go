package config_test

import (
	"testing"

	"github.com/sdougbrown/poo/config"
)

// TestAnUnknownTOMLKeyIsRejected covers the strict decode in the TOML
// path: the formats normalize to the same JSON, and the same unknown
// key is refused.
func TestAnUnknownTOMLKeyIsRejected(t *testing.T) {
	_, err := config.Parse([]byte("version = 1\ncolour = \"blue\"\n"), config.FormatTOML)
	if err == nil {
		t.Fatal("the TOML settings decoded, want a failure")
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeUnknownField {
		t.Fatalf("the failure is %v, want an unknown field", err)
	}
	field, ok := config.FieldOf(err)
	if !ok || field != "colour" {
		t.Errorf("the failure names %q, want colour", field)
	}
}
