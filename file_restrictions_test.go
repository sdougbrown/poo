package poo_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/edit"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
)

func TestSessionAppliesFileToolRestrictions(t *testing.T) {
	outside := t.TempDir()
	readPath := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(readPath, []byte("outside content"), 0600); err != nil {
		t.Fatal(err)
	}
	editPath := filepath.Join(outside, "edit.txt")
	if err := os.WriteFile(editPath, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "needle.txt"), []byte("x needle y\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tool      string
		newTool   func() poo.Tool
		restrict  func(*poo.Options, bool)
		args      map[string]string
		unchecked string
	}{
		{
			tool: "read", newTool: func() poo.Tool { return read.New() },
			restrict:  func(o *poo.Options, v bool) { o.RestrictRead = v },
			args:      map[string]string{"path": readPath},
			unchecked: "outside content",
		},
		{
			tool: "grep", newTool: func() poo.Tool { return grep.New() },
			restrict:  func(o *poo.Options, v bool) { o.RestrictGrep = v },
			args:      map[string]string{"path": outside, "pattern": "needle"},
			unchecked: "needle",
		},
		{
			tool: "edit", newTool: func() poo.Tool { return edit.New() },
			restrict: func(o *poo.Options, v bool) { o.RestrictEdit = v },
			args:     map[string]string{"path": editPath, "old": "hello", "new": "world"},
		},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			args, err := json.Marshal(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			for _, restricted := range []bool{false, true} {
				name := map[bool]string{false: "unrestricted", true: "restricted"}[restricted]
				t.Run(name, func(t *testing.T) {
					sink := &recordingSink{}
					tr := scripted([]fakeResponse{{calls: []poo.ToolCall{{CallID: "c", Name: tc.tool, Arguments: args}}}, {text: "done"}})
					opts := testOptions(t, tr, &echoTool{}, sink)
					opts.Tools = map[string]poo.Tool{tc.tool: tc.newTool()}
					opts.ToolOrder = []string{tc.tool}
					tc.restrict(&opts, restricted)
					s := newTestSession(t, opts)
					runSession(t, s, tc.tool)
					result := toolResultRecord(t, sink)
					if restricted {
						if !strings.Contains(result.Text, "outside the working directory") {
							t.Fatalf("restricted %s result %q does not refuse the path", tc.tool, result.Text)
						}
						return
					}
					if strings.Contains(result.Text, "outside the working directory") {
						t.Fatalf("unrestricted %s result %q refuses the path", tc.tool, result.Text)
					}
					if tc.unchecked != "" && !strings.Contains(result.Text, tc.unchecked) {
						t.Fatalf("unrestricted %s result %q does not contain %q", tc.tool, result.Text, tc.unchecked)
					}
				})
			}
		})
	}
	if string(mustRead(t, editPath)) != "world" {
		t.Fatalf("unrestricted edit did not write the file: %q", mustRead(t, editPath))
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
