package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// The executor is the layer under the sandbox: it wraps the commands the
// model runs, and the harness has to pass it through to the run.

// realPooBinary builds the command once per test process.
var (
	realPooOnce sync.Once
	realPooPath string
	realPooErr  error
)

// buildPoo returns the real command's binary.
//
// The test drives what a user gets: the binary the repository builds, not
// a script that stands in for it.
func buildPoo(t *testing.T) string {
	t.Helper()
	realPooOnce.Do(func() {
		root := repositoryRootOfTests(t)
		dir, err := os.MkdirTemp("", "poo-eval-binary-")
		if err != nil {
			realPooErr = err
			return
		}
		cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "poo"), "./cmd/poo")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			realPooErr = fmt.Errorf("%v: %s", err, out)
			return
		}
		realPooPath = filepath.Join(dir, "poo")
	})
	if realPooErr != nil {
		t.Fatalf("building poo: %v", realPooErr)
	}
	return realPooPath
}

// bashToolEndpoint scripts one bash call and then an answer.
func bashToolEndpoint(t *testing.T, command string) *httptest.Server {
	t.Helper()
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshalling the command: %v", err)
	}
	toolCall := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":` + strconv.Quote(string(args)) + `}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	answer := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		index := requests
		requests++
		mu.Unlock()
		body := toolCall
		if index > 0 {
			body = answer
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestTheBwrapExecutorShowsTheTaskOnlyItsWorkdir covers the eval
// isolation: a run with --executor bwrap gives the model's bash a sandbox
// bound to the task's working dir. A file beside the working dir is not there.
func TestTheBwrapExecutorShowsTheTaskOnlyItsWorkdir(t *testing.T) {
	requireBwrap(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("skip: no go toolchain to build poo: %v", err)
	}

	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	// The check passes only when the fix is in place and the file beside
	// the working dir did not leak into it.
	check := "#!/usr/bin/env bash\nset -uo pipefail\n" +
		"grep -q fixed a.txt || exit 1\n" +
		"if grep -q the-beside seen.txt 2>/dev/null; then exit 1; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(task.Dir, "check.sh"), []byte(check), 0o755); err != nil {
		t.Fatalf("writing the check: %v", err)
	}

	endpoint := bashToolEndpoint(t,
		"echo fixed > a.txt; cat ../beside.txt > seen.txt 2>/dev/null || true")

	runsDir := filepath.Join(t.TempDir(), "work")
	run := cell{task: task, model: Model{Name: "m"}, seed: 1}
	// The file sits beside the working dir on the host, inside the seed's
	// directory. It must not be visible to the sandboxed command.
	seedRoot := jobDir(runsDir, run)
	if err := os.MkdirAll(seedRoot, 0o755); err != nil {
		t.Fatalf("making the seed directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seedRoot, "beside.txt"), []byte("the-beside"), 0o644); err != nil {
		t.Fatalf("writing the file beside the workdir: %v", err)
	}

	runner := NewRunner(RunOptions{
		Models: []Model{{
			Name:         "m",
			BaseURL:      endpoint.URL + "/v1",
			ModelID:      "m",
			ContextLimit: 32768,
		}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       t.TempDir(),
		RunsDir:   runsDir,
		PooBinary: buildPoo(t),
		Executor:  ExecutorBwrap,
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(report.Results) != 1 {
		t.Fatalf("the report holds %d results, want 1", len(report.Results))
	}
	res := report.Results[0]
	if !res.Success {
		t.Fatalf("the sandboxed run did not succeed: %+v", res)
	}
	if report.Executor != ExecutorBwrap {
		t.Errorf("the report's executor is %q, want %q", report.Executor, ExecutorBwrap)
	}
}
