package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// TestANetworkFailureIsAProviderError covers a request that never
// reached the endpoint: the run settles a provider error, not a
// cancellation.
func TestANetworkFailureIsAProviderError(t *testing.T) {
	// The endpoint is closed before the run reaches it, so the
	// connection is refused.
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := endpoint.URL
	endpoint.Close()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr := runCLI(t,
		"run",
		"--endpoint", url+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	)
	if code != 20 {
		t.Fatalf("the exit code is %d, want 20; stderr: %s", code, stderr)
	}

	recs := readLog(t, logPath)
	settled := recs[len(recs)-1]
	if settled["record"] != "run.settled" {
		t.Fatalf("the last record is %v, want run.settled", settled["record"])
	}
	if settled["reason"] != "provider_error" {
		t.Errorf("the settle reason is %v, want provider_error", settled["reason"])
	}
	detail, _ := settled["detail"].(map[string]any)
	provider, _ := detail["provider"].(map[string]any)
	if provider["kind"] != "network" {
		t.Errorf("the provider kind is %v, want network", provider["kind"])
	}
}
