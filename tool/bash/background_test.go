package bash_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
)

// TestTheToolMayRunInTheBackground covers the plan's job rule: the
// bash tool declares that the model may ask for a background call.
//
// The tool's own schema must not define the reserved argument: the core
// would not know whether the model meant the tool's argument or its
// own.
func TestTheToolMayRunInTheBackground(t *testing.T) {
	tool := bash.New()
	spec := tool.Spec()
	if spec.Background != poo.BackgroundAllowed {
		t.Errorf("the background mode is %q, want %q", spec.Background, poo.BackgroundAllowed)
	}
	if strings.Contains(string(spec.Schema), `"background"`) {
		t.Errorf("the schema defines the reserved argument: %s", spec.Schema)
	}
}
