package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The API key must not reach anything but the poo process that needs
// it.
//
// The catalog names one key variable per model, and the harness knows
// every model in the catalog. A run against a model with no key must not
// carry another model's key into the poo process, the check, or the
// report.

func TestTheKeyDoesNotReachTheCheckEnvironment(t *testing.T) {
	// A check that prints its environment must not print the key.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")

	dir := filepath.Join(taskRoot, "fix-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the task directory: %v", err)
	}
	taskJSON := `{"repo": "fixture:fix-a", "prompt": "do it", "timeout_s": 30, "tags": ["t"], "expected_paths": ["a.txt"]}`
	if err := os.WriteFile(filepath.Join(dir, "task.json"), []byte(taskJSON), 0o644); err != nil {
		t.Fatalf("writing the task: %v", err)
	}
	check := "#!/usr/bin/env bash\nset -uo pipefail\necho \"KEY=${THE_KEY:-none}\"\nenv | grep -c '^THE_KEY=' || true\n"
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte(check), 0o755); err != nil {
		t.Fatalf("writing the check: %v", err)
	}
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("loading the task: %v", err)
	}

	t.Setenv("THE_KEY", "s3cret-value")
	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models: []Model{
			{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768, APIKeyEnv: "THE_KEY"},
		},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   filepath.Join(out, "work"),
		PooBinary: fakePoo(t, logWritingPoo),
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if strings.Contains(report.Results[0].CheckOutput, "s3cret-value") {
		t.Errorf("the check output holds the key: %q", report.Results[0].CheckOutput)
	}

	// The report itself must not hold the key either.
	report = &Report{Dir: out, Results: report.Results}
	if err := WriteReport(out, report); err != nil {
		t.Fatalf("writing the report: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "report.json"))
	if err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	if strings.Contains(string(data), "s3cret-value") {
		t.Error("the report holds the key")
	}
}

func TestTheKeyDoesNotReachThePooProcessForAnotherModel(t *testing.T) {
	// The catalog holds a model with a key and one without. A run
	// against the model without a key must not carry the other model's
	// key into the poo process.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	t.Setenv("THE_KEY", "s3cret-value")
	out := t.TempDir()
	capture := filepath.Join(out, "env.txt")
	binary := fakePoo(t, `
env > "`+capture+`"
`+logWritingPoo)

	runner := NewRunner(RunOptions{
		Models: []Model{
			{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768},
		},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   filepath.Join(out, "work"),
		PooBinary: binary,
		// The catalog names a key variable for another model. The run
		// must not carry it.
		Catalog: []Model{
			{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768},
			{Name: "other", Endpoint: "http://y", ModelID: "o", ContextLimit: 32768, APIKeyEnv: "THE_KEY"},
		},
	})

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	env, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("reading the environment: %v", err)
	}
	if strings.Contains(string(env), "THE_KEY=") {
		t.Errorf("the poo process holds another model's key:\n%s", env)
	}
}

func TestTheSelectedModelsKeyReachesThePooProcess(t *testing.T) {
	// The model that names the key gets it, or the run cannot talk to
	// the endpoint.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	t.Setenv("THE_KEY", "s3cret-value")
	out := t.TempDir()
	capture := filepath.Join(out, "env.txt")
	binary := fakePoo(t, `
env > "`+capture+`"
`+logWritingPoo)

	runner := NewRunner(RunOptions{
		Models: []Model{
			{Name: "m", Endpoint: "http://x", ModelID: "m", ContextLimit: 32768, APIKeyEnv: "THE_KEY"},
		},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		RunsDir:   filepath.Join(out, "work"),
		PooBinary: binary,
	})

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	env, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("reading the environment: %v", err)
	}
	if !strings.Contains(string(env), "THE_KEY=s3cret-value") {
		t.Errorf("the poo process does not hold its own key:\n%s", env)
	}
}
