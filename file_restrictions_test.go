package poo_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/read"
)

func TestSessionAppliesReadRestriction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(path, []byte("outside content"), 0600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": path})
	for _, restricted := range []bool{false, true} {
		t.Run(map[bool]string{false: "unrestricted", true: "restricted"}[restricted], func(t *testing.T) {
			sink := &recordingSink{}
			tr := scripted([]fakeResponse{{calls: []poo.ToolCall{{CallID: "c", Name: "read", Arguments: args}}}, {text: "done"}})
			opts := testOptions(t, tr, &echoTool{}, sink)
			opts.Tools = map[string]poo.Tool{"read": read.New()}
			opts.ToolOrder = []string{"read"}
			opts.RestrictRead = restricted
			s := newTestSession(t, opts)
			runSession(t, s, "read")
			result := toolResultRecord(t, sink)
			want := "outside content"
			if restricted {
				want = "outside the working directory"
			}
			if !strings.Contains(result.Text, want) {
				t.Fatalf("result %q does not contain %q", result.Text, want)
			}
		})
	}
}
