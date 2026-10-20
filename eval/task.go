package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Task is one unit of work the harness measures.
//
// A task is a repository (or a fixture directory), a prompt, a check that
// says whether the run succeeded, and the metadata the report groups by.
type Task struct {
	// ID is the task's identifier: the name of the directory under
	// evals/tasks.
	ID string `json:"id"`
	// Repo is where the work happens: "fixture:<dir>" or "<git url>@<sha>".
	Repo string `json:"repo"`
	// Prompt is what the run is asked to do.
	Prompt string `json:"prompt"`
	// TimeoutS is the wall-clock ceiling for the run, in seconds. Zero
	// means the harness default.
	TimeoutS int `json:"timeout_s"`
	// Tags are the labels a filter can select on.
	Tags []string `json:"tags,omitempty"`
	// ExpectedPaths are the globs the run is allowed to change. A change
	// outside them is a diff-sanity finding, not a failure.
	ExpectedPaths []string `json:"expected_paths,omitempty"`
	// Description is a free-text note about the task.
	Description string `json:"description,omitempty"`
	// Dir is the task's own directory, holding task.json and check.sh.
	Dir string `json:"-"`
}

// HasTag reports whether the task carries a tag.
func (t Task) HasTag(tag string) bool {
	for _, x := range t.Tags {
		if x == tag {
			return true
		}
	}
	return false
}

// taskFile is the on-disk shape of task.json.
//
// It is a separate type so the file's fields can be strict: a typo in a
// task file is an error, not a silently dropped key.
type taskFile struct {
	Repo          string   `json:"repo"`
	Prompt        string   `json:"prompt"`
	TimeoutS      int      `json:"timeout_s"`
	Tags          []string `json:"tags"`
	ExpectedPaths []string `json:"expected_paths"`
	Description   string   `json:"description"`
}

// LoadTasks reads the task set under a directory.
//
// Each subdirectory holds a task.json and a check.sh. A directory without
// a task.json is not a task and is skipped, so a notes file next to the
// tasks does not break the load.
func LoadTasks(dir string) ([]Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("eval: reading %s: %w", dir, err)
	}

	var tasks []Task
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		taskDir := filepath.Join(dir, e.Name())
		path := filepath.Join(taskDir, "task.json")
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("eval: reading %s: %w", path, err)
		}
		task, err := LoadTask(taskDir)
		if err != nil {
			return nil, err
		}
		if seen[task.ID] {
			return nil, fmt.Errorf("eval: two tasks are named %q", task.ID)
		}
		seen[task.ID] = true
		tasks = append(tasks, task)
	}

	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

// LoadTask reads one task directory.
func LoadTask(dir string) (Task, error) {
	path := filepath.Join(dir, "task.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Task{}, fmt.Errorf("eval: reading %s: %w", path, err)
	}

	var file taskFile
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return Task{}, fmt.Errorf("eval: parsing %s: %w", path, err)
	}

	id := filepath.Base(dir)
	task := Task{
		ID:            id,
		Repo:          file.Repo,
		Prompt:        file.Prompt,
		TimeoutS:      file.TimeoutS,
		Tags:          file.Tags,
		ExpectedPaths: file.ExpectedPaths,
		Description:   file.Description,
		Dir:           dir,
	}
	if err := task.validate(); err != nil {
		return Task{}, fmt.Errorf("eval: %s: %w", path, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "check.sh")); err != nil {
		return Task{}, fmt.Errorf("eval: %s: the task has no check.sh: %w", path, err)
	}
	return task, nil
}

// validate checks a task's fields.
func (t Task) validate() error {
	switch {
	case strings.TrimSpace(t.Repo) == "":
		return fmt.Errorf("repo must not be empty")
	case strings.TrimSpace(t.Prompt) == "":
		return fmt.Errorf("prompt must not be empty")
	case t.TimeoutS < 0:
		return fmt.Errorf("timeout_s must not be negative")
	}
	return nil
}

// SelectTasks keeps the tasks a filter selects.
//
// A filter is a set of terms. A term matches a task when it equals the
// task's id, or when it equals one of the task's tags. A task with no tags
// is never matched by a term.
func SelectTasks(tasks []Task, terms []string) []Task {
	if len(terms) == 0 {
		return tasks
	}
	var out []Task
	for _, t := range tasks {
		for _, term := range terms {
			term = strings.TrimSpace(term)
			if term == "" {
				continue
			}
			if t.ID == term || t.HasTag(term) {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// FixtureDir returns the directory a "fixture:<dir>" task works in.
//
// The path is relative to the task set's root, so a task set is portable
// and a fixture is never picked up from somewhere else on the machine.
func (t Task) FixtureDir(taskRoot string) (string, error) {
	const prefix = "fixture:"
	if !strings.HasPrefix(t.Repo, prefix) {
		return "", fmt.Errorf("eval: task %q: %q is not a fixture repo", t.ID, t.Repo)
	}
	rel := strings.TrimPrefix(t.Repo, prefix)
	if rel == "" {
		return "", fmt.Errorf("eval: task %q: the fixture repo names no directory", t.ID)
	}
	return filepath.Join(taskRoot, "..", "fixtures", rel), nil
}
