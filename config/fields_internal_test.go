package config

import "testing"

// TestTheFieldsKeepAnEmptyChainApart covers the fields the generated
// check reads: a chain the settings name with no askers is present and
// empty, and a chain they do not name is absent.
func TestTheFieldsKeepAnEmptyChainApart(t *testing.T) {
	empty, err := Parse([]byte("version: 1\npermission:\n  askers: []\n"), FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	fields := fieldsOf(empty)
	if fields.PermissionAskers == nil {
		t.Error("the fields hold no asker chain, want an empty one the settings named")
	}
	if len(fields.PermissionAskers) != 0 {
		t.Errorf("the fields hold %v, want no askers", fields.PermissionAskers)
	}

	absent, err := Parse([]byte("version: 1\n"), FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	if got := fieldsOf(absent).PermissionAskers; got != nil {
		t.Errorf("the fields hold %v, want no chain: the settings named none", got)
	}
}
