package main

import (
	"os"
	"testing"
)

// TestMain removes manifest overrides inherited from the shell.
func TestMain(m *testing.M) {
	os.Unsetenv("POO_FEEDS_PATH")
	os.Unsetenv("POO_CLI_REGISTRY_PATH")
	os.Exit(m.Run())
}
