package config_test

import (
	"context"
	"os"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/perm"
)

// fix1AskClient records the asks a policy makes.
type fix1AskClient struct {
	asks []perm.Ask
}

func (c *fix1AskClient) Connected() bool         { return true }
func (c *fix1AskClient) PermissionAnswers() bool { return true }
func (c *fix1AskClient) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	c.asks = append(c.asks, ask)
	return perm.DecisionAllow, nil
}

// TestThePolicyGetsTheResolvedWorkingDirectory covers the build's
// working directory: a consumer that leaves BuildOptions.WorkingDir
// empty still gets the directory the tools work in.
func TestThePolicyGetsTheResolvedWorkingDirectory(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+"permission:\n  unmatched: ask\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	client := &fix1AskClient{}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
		Askers:         []string{"wire"},
		Wire:           client,
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if _, err := runtime.Policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec: poo.ToolSpec{Name: "bash", Class: poo.ClassSideEffecting},
	}); err != nil {
		t.Fatalf("asking the policy: %v", err)
	}
	if len(client.asks) != 1 {
		t.Fatalf("the policy asked %d time(s), want 1", len(client.asks))
	}
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	if client.asks[0].WorkingDir != want {
		t.Errorf("the ask's working directory is %q, want the resolved %q", client.asks[0].WorkingDir, want)
	}
}
