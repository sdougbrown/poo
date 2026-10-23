package config_test

import (
	"testing"

	"github.com/sdougbrown/poo/config"
)

// TestASetZeroIsChecked covers the generated minimum rules: a numeric
// field the settings set to zero is a value, and the rule refuses it.
func TestASetZeroIsChecked(t *testing.T) {
	for _, body := range []string{
		"version: 1\npermission:\n  chain_timeout_s: 0\n",
		"version: 1\ndecider:\n  timeout_ms: 0\n",
		"version: 1\npermission:\n  doom_loop:\n    threshold: 0\n",
		"version: 1\npermission:\n  doom_loop:\n    near_threshold: 0\n",
		"version: 1\npermission:\n  decide:\n    timeout_ms: 0\n",
	} {
		if _, err := config.Parse([]byte(body), config.FormatYAML); err == nil {
			t.Errorf("the settings %q decoded, want the minimum rule to refuse it", body)
		}
	}
}
