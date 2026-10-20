package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// concurrencyCounter counts the runs that are in flight at once, per
// endpoint, and records the high-water mark.
//
// The fake poo appends a line to a file when it starts and another when
// it ends, so the overlap can be counted from the file.
type concurrencyCounter struct {
	paths map[string]string
	// tmp is where the event files go.
	tmp string
}

func newConcurrencyCounter() *concurrencyCounter {
	return &concurrencyCounter{paths: map[string]string{}}
}

// script returns a fake poo that records its start and its end.
func (c *concurrencyCounter) script(endpoint string) string {
	c.paths[endpoint] = filepath.Join(c.tmp, "events-"+strings.ReplaceAll(endpoint, "/", "_"))
	return `
log=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --log) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
echo start >> "` + c.paths[endpoint] + `"
sleep 0.2
echo end >> "` + c.paths[endpoint] + `"
cat > "$log" <<'JSON'
{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s"}
{"record":"run.settled","seq":2,"ts":"2026-10-20T00:00:01Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":1}
JSON
exit 0
`
}

// measure reads the event file and returns the most runs that overlapped.
func (c *concurrencyCounter) measure(endpoint string) int {
	data, err := os.ReadFile(c.paths[endpoint])
	if err != nil {
		return 0
	}
	live := 0
	peak := 0
	for _, line := range strings.Split(string(data), "\n") {
		switch line {
		case "start":
			live++
			if live > peak {
				peak = live
			}
		case "end":
			live--
		}
	}
	return peak
}

func TestTheEndpointLimitHolds(t *testing.T) {
	// Two models on one endpoint, three seeds each: six runs, and the
	// endpoint never carries more than two at a time.
	root := t.TempDir()
	counter := newConcurrencyCounter()
	counter.tmp = root

	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	endpoint := "http://localhost:4000/v1"
	models := []Model{
		{Name: "a", BaseURL: endpoint, ModelID: "m", ContextLimit: 32768},
		{Name: "b", BaseURL: endpoint, ModelID: "m", ContextLimit: 32768},
	}

	// The fake poo is one script per model, but both models share the
	// endpoint, so they share the event file.
	binary := fakePoo(t, counter.script(endpoint))

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    models,
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     3,
		Parallel:  6,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: binary,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("running the harness: %v", err)
	}

	peak := counter.measure(endpoint)
	if peak > MaxParallelPerEndpoint {
		t.Errorf("the endpoint carried %d runs at once, want at most %d", peak, MaxParallelPerEndpoint)
	}
	if peak < 2 {
		t.Errorf("the endpoint carried %d runs at once, want the limit to be used", peak)
	}
}

func TestTheGlobalLimitHolds(t *testing.T) {
	// The global limit is the smaller of the two, and it holds even
	// when there are many endpoints.
	root := t.TempDir()
	counter := newConcurrencyCounter()
	counter.tmp = root

	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	endpoint := "http://localhost:4000/v1"
	models := []Model{
		{Name: "a", BaseURL: endpoint, ModelID: "m", ContextLimit: 32768},
		{Name: "b", BaseURL: endpoint, ModelID: "m", ContextLimit: 32768},
	}
	binary := fakePoo(t, counter.script(endpoint))

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    models,
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     3,
		Parallel:  1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: binary,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("running the harness: %v", err)
	}

	peak := counter.measure(endpoint)
	if peak > 1 {
		t.Errorf("the harness ran %d runs at once, want at most 1", peak)
	}
}

func TestTheRunIsBoundedByTheTaskTimeout(t *testing.T) {
	// A poo that never ends is stopped by the task's timeout, and the
	// run is reported as an error rather than hanging the harness.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	task.TimeoutS = 1

	binary := fakePoo(t, `
log=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --log) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
sleep 30
exit 0
`)

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: binary,
	})

	started := time.Now()
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Errorf("the run took %s, want the timeout to have stopped it", elapsed)
	}
	if report.Results[0].Error == "" {
		t.Error("the run reported no error, want one about the timeout")
	}
}
