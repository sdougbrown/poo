package catalog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/catalog"
)

// TestTheDiscoveryCacheIsPrivate covers the cache file's modes: the poo
// directory and the cache hold the endpoints' data for this user only.
func TestTheDiscoveryCacheIsPrivate(t *testing.T) {
	srv := fix1VLLM(t, "gemma4", 32768)
	state := t.TempDir()

	c := catalog.New(catalog.Options{
		StateDir: state,
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"local": {BaseURL: srv.URL},
			},
		},
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("refreshing the catalog: %v", err)
	}

	dir := filepath.Join(state, "poo")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the cache directory: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("the cache directory's mode is %#o, want 0700", got)
	}
	info, err = os.Stat(filepath.Join(dir, "discovery.json"))
	if err != nil {
		t.Fatalf("the cache file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the cache file's mode is %#o, want 0600", got)
	}
}

// TestAnAnthropicEntryGetsOnlyChatCompletionsControls covers the
// catalog's Phase A data: the Anthropic entries are reached through Chat
// Completions, which can send neither a thinking budget nor an explicit
// cache breakpoint.
func TestAnAnthropicEntryGetsOnlyChatCompletionsControls(t *testing.T) {
	c := catalog.New(catalog.Options{Settings: localSettings()})
	for _, info := range c.List() {
		if info.ModelID != "claude-sonnet-4-5" {
			continue
		}
		if info.API != poo.APIChatCompletions {
			t.Errorf("the entry's API is %q, want chat_completions", info.API)
		}
		if info.Reasoning.Mode == poo.ReasoningBudget {
			t.Error("the entry names a thinking budget that Chat Completions cannot send")
		}
		if info.PromptCache == poo.CacheExplicit {
			t.Error("the entry names an explicit cache that Chat Completions cannot place")
		}
		return
	}
	t.Fatal("the Anthropic entry is not listed")
}
