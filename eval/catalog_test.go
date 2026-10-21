package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheModelCatalogIsRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yaml")
	content := `
- name: gemma4
  base_url: http://localhost:4000/v1
  model_id: gemma4
  context_limit: 32768
  notes: the low-budget run

- name: minimax
  base_url: https://ollama.com/v1
  model_id: minimax-m3.1-flash
  api_key_env: OLLAMA_API_KEY
  context_limit: 262144
  tools_mode: text
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing the catalog: %v", err)
	}

	models, err := LoadModels(path)
	if err != nil {
		t.Fatalf("loading the catalog: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("the catalog holds %d models, want 2", len(models))
	}
	if models[0].Name != "gemma4" || models[0].ContextLimit != 32768 {
		t.Errorf("the first model is %+v, want gemma4 with a 32768 limit", models[0])
	}
	if models[0].EffectiveToolsMode() != "native" {
		t.Errorf("the first model's tools mode is %q, want native", models[0].EffectiveToolsMode())
	}
	if models[1].APIKeyEnv != "OLLAMA_API_KEY" {
		t.Errorf("the second model's key variable is %q, want OLLAMA_API_KEY", models[1].APIKeyEnv)
	}
	if models[1].EffectiveToolsMode() != "text" {
		t.Errorf("the second model's tools mode is %q, want text", models[1].EffectiveToolsMode())
	}
}

func TestABadModelCatalogIsRejected(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"no name", "- base_url: http://x\n  model_id: m\n  context_limit: 10\n", "name"},
		{"no context limit", "- name: m\n  base_url: http://x\n  model_id: m\n", "context_limit"},
		{"a zero context limit", "- name: m\n  base_url: http://x\n  model_id: m\n  context_limit: 0\n", "context_limit"},
		{"a bad tools mode", "- name: m\n  base_url: http://x\n  model_id: m\n  context_limit: 10\n  tools_mode: weird\n", "tools_mode"},
		{"an unknown field", "- name: m\n  base_url: http://x\n  model_id: m\n  context_limit: 10\n  wat: 1\n", "wat"},
		{"no models", "[]\n", "no models"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "models.yaml")
			if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
				t.Fatalf("writing the catalog: %v", err)
			}
			_, err := LoadModels(path)
			if err == nil {
				t.Fatal("the catalog was accepted, want an error")
			}
			if !contains(err.Error(), c.want) {
				t.Errorf("the error is %q, want it to name %q", err.Error(), c.want)
			}
		})
	}
}

func TestTwoModelsWithTheSameNameAreRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yaml")
	content := `
- name: m
  base_url: http://x
  model_id: m
  context_limit: 10
- name: m
  base_url: http://y
  model_id: m
  context_limit: 10
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing the catalog: %v", err)
	}
	if _, err := LoadModels(path); err == nil {
		t.Fatal("the catalog was accepted, want an error")
	}
}

func TestTheModelsAreSelectedByName(t *testing.T) {
	models := []Model{
		{Name: "gemma4"},
		{Name: "qwen"},
		{Name: "minimax"},
	}
	got := SelectModels(models, []string{"qwen", "minimax"})
	if len(got) != 2 || got[0].Name != "qwen" || got[1].Name != "minimax" {
		t.Errorf("the selection is %+v, want qwen and minimax", got)
	}
	if got := SelectModels(models, nil); len(got) != 3 {
		t.Errorf("an empty filter kept %d models, want 3", len(got))
	}
}

func TestTheTasksAreRead(t *testing.T) {
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "fix-a")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("making the task directory: %v", err)
	}
	content := `{
  "repo": "fixture:fix-a",
  "prompt": "fix it",
  "timeout_s": 120,
  "tags": ["bugfix", "python"],
  "expected_paths": ["a.py"],
  "description": "a bug"
}`
	if err := os.WriteFile(filepath.Join(taskDir, "task.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing the task: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "check.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing the check: %v", err)
	}
	// A directory without a task.json is not a task.
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatalf("making the notes directory: %v", err)
	}

	tasks, err := LoadTasks(dir)
	if err != nil {
		t.Fatalf("loading the tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("the task set holds %d tasks, want 1", len(tasks))
	}
	task := tasks[0]
	if task.ID != "fix-a" {
		t.Errorf("the task's id is %q, want fix-a", task.ID)
	}
	if task.Repo != "fixture:fix-a" {
		t.Errorf("the task's repo is %q, want fixture:fix-a", task.Repo)
	}
	if task.TimeoutS != 120 {
		t.Errorf("the task's timeout is %d, want 120", task.TimeoutS)
	}
	if len(task.Tags) != 2 || !task.HasTag("bugfix") {
		t.Errorf("the task's tags are %v, want bugfix and python", task.Tags)
	}
	if len(task.ExpectedPaths) != 1 || task.ExpectedPaths[0] != "a.py" {
		t.Errorf("the task's expected paths are %v, want a.py", task.ExpectedPaths)
	}
}

func TestABadTaskIsRejected(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"no repo", `{"prompt": "x"}`, "repo"},
		{"no prompt", `{"repo": "fixture:a"}`, "prompt"},
		{"an unknown field", `{"repo": "fixture:a", "prompt": "x", "wat": 1}`, "wat"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			taskDir := filepath.Join(dir, "t")
			if err := os.MkdirAll(taskDir, 0o755); err != nil {
				t.Fatalf("making the task directory: %v", err)
			}
			if err := os.WriteFile(filepath.Join(taskDir, "task.json"), []byte(c.content), 0o644); err != nil {
				t.Fatalf("writing the task: %v", err)
			}
			if err := os.WriteFile(filepath.Join(taskDir, "check.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatalf("writing the check: %v", err)
			}
			_, err := LoadTasks(dir)
			if err == nil {
				t.Fatal("the task was accepted, want an error")
			}
			if !contains(err.Error(), c.want) {
				t.Errorf("the error is %q, want it to name %q", err.Error(), c.want)
			}
		})
	}
}

func TestATaskWithoutACheckIsRejected(t *testing.T) {
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "t")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("making the task directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "task.json"), []byte(`{"repo":"fixture:a","prompt":"x"}`), 0o644); err != nil {
		t.Fatalf("writing the task: %v", err)
	}
	if _, err := LoadTasks(dir); err == nil {
		t.Fatal("the task was accepted, want an error about the missing check")
	}
}

func TestTheTasksAreSelectedByIDOrTag(t *testing.T) {
	tasks := []Task{
		{ID: "fix-a", Tags: []string{"bugfix"}},
		{ID: "fix-b", Tags: []string{"bugfix", "python"}},
		{ID: "explain-c", Tags: []string{"readonly"}},
	}
	got := SelectTasks(tasks, []string{"bugfix"})
	if len(got) != 2 {
		t.Errorf("the tag filter kept %d tasks, want 2", len(got))
	}
	got = SelectTasks(tasks, []string{"fix-a", "readonly"})
	if len(got) != 2 {
		t.Errorf("the id and tag filter kept %d tasks, want 2", len(got))
	}
	if got := SelectTasks(tasks, nil); len(got) != 3 {
		t.Errorf("an empty filter kept %d tasks, want 3", len(got))
	}
}

func TestTheFixturePathIsRelativeToTheTaskRoot(t *testing.T) {
	task := Task{ID: "t", Repo: "fixture:fix-a"}
	got, err := task.FixtureDir("/repo/evals/tasks")
	if err != nil {
		t.Fatalf("resolving the fixture: %v", err)
	}
	want := filepath.Join("/repo/evals/fixtures", "fix-a")
	if got != want {
		t.Errorf("the fixture path is %q, want %q", got, want)
	}
}

func TestTheShippedModelCatalogIsValid(t *testing.T) {
	models, err := LoadModels(filepath.Join(repoRoot(t), "evals", "models.yaml"))
	if err != nil {
		t.Fatalf("loading evals/models.yaml: %v", err)
	}
	byName := map[string]Model{}
	for _, m := range models {
		byName[m.Name] = m
	}
	if got := byName["gemma4-think"].ReasoningEffort; got == "" {
		t.Errorf("gemma4-think sends no reasoning_effort")
	}
}

func TestTheRunnerPassesTheReasoningEffort(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	argv := filepath.Join(t.TempDir(), "argv")
	poo := fakePoo(t, `printf '%s\n' "$@" > '`+argv+`'
`+logWritingPoo)
	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models: []Model{
			{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768, ReasoningEffort: "low"},
		},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: poo,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	data, err := os.ReadFile(argv)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	found := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--reasoning-effort" && args[i+1] == "low" {
			found = true
		}
	}
	if !found {
		t.Errorf("poo was run with %q, want --reasoning-effort low", args)
	}
}
