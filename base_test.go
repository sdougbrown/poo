package poo_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestTheCoreRendersABaseSection covers the plan's section table: the
// core owns the `base` section, and it is the operating contract the
// model works under. A session that does not come from cmd/poo has no
// other place to get one.
func TestTheCoreRendersABaseSection(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "one"}})
	sink := &recordingSink{}
	opts := poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
		WorkingDir:   t.TempDir(),
	}
	s := newTestSession(t, opts)
	runSession(t, s, "one")

	var text string
	for _, rec := range sink.allRecords() {
		if prompt, ok := rec.(*poo.SystemPromptRecord); ok {
			text = prompt.Text
		}
	}
	if text == "" {
		t.Fatal("the session recorded no prompt")
	}
	t.Logf("the core's prompt is:\n%s", text)
	if strings.HasPrefix(text, "Environment:") {
		t.Errorf("the core's prompt holds no base section: it starts with the environment")
	}
}
