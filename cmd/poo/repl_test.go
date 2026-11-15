package main

import (
	"io"
	"slices"
	"testing"

	clicontract "github.com/sdougbrown/cli-contract"
)

// TestTheReplHandsItsServerTheFlagsItParsed covers the repl's handoff: the
// repl runs `poo serve` as a child, so it spells every flag it parsed a
// second time. The child has to read the same values back, and a flag the
// handoff forgets is a flag the session never sees.
func TestTheReplHandsItsServerTheFlagsItParsed(t *testing.T) {
	app := pooApp(io.Discard)
	repl, err := clicontract.Parse(app, []string{
		"repl",
		"--settings", "settings.yaml",
		"--ephemeral",
		"--feeds-path", "one.yaml",
		"--feeds-path", "two.yaml",
		"--profile", "code",
		"--endpoint", "http://localhost:4000/v1",
		"--model-id", "test-model",
		"--context-limit", "8192",
		"--working-dir", "/tmp",
		"--executor", "local",
	})
	if err != nil {
		t.Fatalf("parsing the repl's flags: %v", err)
	}

	argv := append([]string{"serve"}, renderFlags(repl, serverFlagSpecs())...)
	server, err := clicontract.Parse(app, argv)
	if err != nil {
		t.Fatalf("the server refused the flags the repl spelled (%v): %v", argv, err)
	}

	for name := range repl.Supplied {
		if !server.Supplied[name] {
			t.Errorf("the handoff dropped --%s: %v", name, argv)
		}
	}
	for name, value := range repl.Flags {
		if server.Flags[name] != value {
			t.Errorf("--%s is %q for the server, want %q", name, server.Flags[name], value)
		}
	}
	for name, value := range repl.Bools {
		if server.Bools[name] != value {
			t.Errorf("--%s is %t for the server, want %t", name, server.Bools[name], value)
		}
	}
	for name, value := range repl.Ints {
		if server.Ints[name] != value {
			t.Errorf("--%s is %d for the server, want %d", name, server.Ints[name], value)
		}
	}
	for name, values := range repl.Repeated {
		if !slices.Equal(server.Repeated[name], values) {
			t.Errorf("--%s is %q for the server, want %q", name, server.Repeated[name], values)
		}
	}
}
