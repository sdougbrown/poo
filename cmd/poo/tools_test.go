package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// TestTheCallToolAnswersEveryBuiltInToolWithTheCorrection pins the built-in
// names the call tool refuses to run: a model that handed call_cli a tool the
// session offers reads that it is a built-in tool, called directly, rather
// than a count of catalog entries. The names are the command's own tools and
// the core's, so a tool added to either is added to the refusal with it.
func TestTheCallToolAnswersEveryBuiltInToolWithTheCorrection(t *testing.T) {
	catalog, err := cliregistry.Load()
	if err != nil {
		t.Fatalf("loading an empty catalog: %v", err)
	}
	tool := cliregistry.NewCall(catalog)

	names := []string{profile.JobsToolName, profile.CancelJobToolName, poo.SubscribeFeedToolName, poo.FinishToolName}
	for name := range linkedTools() {
		names = append(names, name)
	}
	for _, name := range names {
		// The catalog's own two tools are not built-in: one of them runs the
		// call, and the other searches what it runs.
		if name == cliregistry.CallToolName || name == cliregistry.FindToolName {
			continue
		}
		raw, err := json.Marshal(map[string]any{"command": name})
		if err != nil {
			t.Fatalf("encoding the call: %v", err)
		}
		_, err = tool.Prepare(raw, poo.PrepareEnv{})
		if err == nil {
			t.Errorf("%s prepared a call of the built-in tool %q", cliregistry.CallToolName, name)
			continue
		}
		if !strings.Contains(err.Error(), "is a built-in tool") {
			t.Errorf("the refusal of %q does not name it a built-in tool: %v", name, err)
		}
	}
}
