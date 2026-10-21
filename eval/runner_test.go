package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// fakePoo writes a shell script that stands in for the poo binary.
//
// The script writes a session log with the given number of turns, and
// then changes a file in the workdir, so the harness has something to
// measure.
func fakePoo(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "poo")
	script := "#!/usr/bin/env bash\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake poo: %v", err)
	}
	return path
}

// fakeFixture writes a fixture directory with one file.
func fakeFixture(t *testing.T, name, content string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "fixtures", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing the fixture file: %v", err)
	}
	return filepath.Join(root, "fixtures")
}

// fakeTask writes a task directory whose check looks for a file's
// content.
func fakeTask(t *testing.T, root, id, fixture, want string) Task {
	t.Helper()
	dir := filepath.Join(root, "tasks", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the task directory: %v", err)
	}
	taskJSON := `{"repo": "fixture:` + fixture + `", "prompt": "do it", "timeout_s": 30, "tags": ["t"], "expected_paths": ["a.txt"]}`
	if err := os.WriteFile(filepath.Join(dir, "task.json"), []byte(taskJSON), 0o644); err != nil {
		t.Fatalf("writing the task: %v", err)
	}
	check := "#!/usr/bin/env bash\nset -uo pipefail\n" +
		"grep -q '" + want + "' a.txt\n"
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte(check), 0o755); err != nil {
		t.Fatalf("writing the check: %v", err)
	}
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("loading the task: %v", err)
	}
	return task
}

// logWritingPoo is a fake poo that writes a session log and changes a
// file.
const logWritingPoo = `
log=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --log) log="$2"; shift 2 ;;
    --working-dir) dir="$2"; shift 2 ;;
    *) shift ;;
  esac
done
cat > "$log" <<'JSON'
{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}
{"record":"run.started","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","model_id":"m","max_turns":50}
{"record":"message","seq":3,"ts":"2026-10-20T00:00:01Z","run_id":"r","turn":0,"origin":"user","role":"user","content":[{"kind":"text","text":"do it"}]}
{"record":"message","seq":4,"ts":"2026-10-20T00:00:02Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"read","arguments":{"path":"a.txt"}}]}
{"record":"tool.result","seq":5,"ts":"2026-10-20T00:00:03Z","run_id":"r","turn":1,"handle":"r1","tool":"read","outcome":"ok","text":"old"}
{"record":"usage","seq":6,"ts":"2026-10-20T00:00:03Z","run_id":"r","turn":1,"request":1,"input_tokens":100,"output_tokens":20,"ttft_ms":50}
{"record":"run.settled","seq":7,"ts":"2026-10-20T00:00:04Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":1,"settled_at":"2026-10-20T00:00:04Z"}
JSON
echo fixed > "$dir/a.txt"
exit 0
`

func TestTheRunnerRunsTheTasksAndMeasuresThem(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     2,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: fakePoo(t, logWritingPoo),
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if len(report.Results) != 2 {
		t.Fatalf("the harness made %d runs, want 2", len(report.Results))
	}

	for _, r := range report.Results {
		if !r.Success {
			t.Errorf("the run %+v did not pass the check", r)
		}
		if r.Metrics.Turns != 1 {
			t.Errorf("the run made %d turns, want 1", r.Metrics.Turns)
		}
		if r.Metrics.InputTokens != 100 {
			t.Errorf("the run used %d input tokens, want 100", r.Metrics.InputTokens)
		}
		if r.Metrics.SettleReason != "end_turn" {
			t.Errorf("the run settled %q, want end_turn", r.Metrics.SettleReason)
		}
		if len(r.Metrics.ChangedFiles) != 1 || r.Metrics.ChangedFiles[0] != "a.txt" {
			t.Errorf("the run changed %v, want a.txt", r.Metrics.ChangedFiles)
		}
		if r.Metrics.DiffLines == 0 {
			t.Error("the run's diff is empty, want the changed file")
		}
		if len(r.Metrics.UnexpectedFiles) != 0 {
			t.Errorf("the run changed %v outside the expected paths", r.Metrics.UnexpectedFiles)
		}
		// The log is kept.
		if _, err := os.Stat(r.LogPath); err != nil {
			t.Errorf("the run kept no log: %v", err)
		}
	}
}

func TestTheRunnerMeasuresAFailingCheck(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "never-there")

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: fakePoo(t, logWritingPoo),
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if report.Results[0].Success {
		t.Error("the run passed the check, want a failure")
	}
	if report.Results[0].CheckExit == 0 {
		t.Error("the check exited zero, want a failure")
	}
}

func TestTheWorkdirLivesUnderTheWorkRoot(t *testing.T) {
	// The workdir is under the work root the caller names, never in
	// /tmp's shared space.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	out := t.TempDir()
	workRoot := filepath.Join(out, "work")
	// The fake poo writes down where it ran, so the harness's layout can
	// be checked.
	marker := filepath.Join(out, "workdir.txt")
	binary := fakePoo(t, `
dir=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --working-dir) dir="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf '%s' "$dir" > "`+marker+`"
`+logWritingPoo)

	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  workRoot,
		PooBinary: binary,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("running the harness: %v", err)
	}

	where, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reading the marker: %v", err)
	}
	got := string(where)
	if !strings.HasPrefix(got, workRoot) {
		t.Errorf("the workdir is %q, want it under %q", got, workRoot)
	}
}

func TestTheRunnerReportsAMissingBinary(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: filepath.Join(t.TempDir(), "does-not-exist"),
	})

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if report.Results[0].Error == "" {
		t.Error("the run reported no error, want one about the missing binary")
	}
}

func TestTheAPIBinaryIsPassedByName(t *testing.T) {
	// The harness passes the key variable's name to the binary, and
	// never the value.
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	out := t.TempDir()
	capture := filepath.Join(out, "args.txt")
	binary := fakePoo(t, `
args=""
while [ "$#" -gt 0 ]; do
  args="$args $1"
  shift
done
printf '%s' "$args" > "`+capture+`"
`+logWritingPoo)

	t.Setenv("THE_KEY", "s3cret")
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768, APIKeyEnv: "THE_KEY"}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: binary,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("running the harness: %v", err)
	}

	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("reading the arguments: %v", err)
	}
	text := string(args)
	if !strings.Contains(text, "--api-key-env THE_KEY") {
		t.Errorf("the arguments are %q, want the key variable's name", text)
	}
	if strings.Contains(text, "s3cret") {
		t.Error("the key's value was passed to the binary")
	}
}

func TestTheReplayRunsTheRecordedCalls(t *testing.T) {
	// A session that called a tool once is replayed, and the replayed
	// sequence is the same.
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	log := strings.Join([]string{
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s"}`,
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"bash","arguments":{"command":"ls -la"}}]}`,
		`{"record":"tool.result","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"handle":"r1","tool":"bash","outcome":"ok","text":"a.go"}`,
		`{"record":"message","seq":4,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":2,"origin":"model","role":"assistant","content":[{"kind":"text","text":"done"}]}`,
		`{"record":"run.settled","seq":5,"ts":"2026-10-20T00:00:00Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":2}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}

	result, err := Replay(path, ReplayOptions{Context: context.Background(), ContextLimit: 32768})
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if len(result.Recorded) != 1 {
		t.Fatalf("the recording holds %d calls, want 1", len(result.Recorded))
	}
	if len(result.Replayed) != 1 {
		t.Fatalf("the replay produced %d calls, want 1", len(result.Replayed))
	}
	if !result.Match {
		t.Errorf("the sequences differ: %v", result.Differences)
	}
	if result.Replayed[0].Tool != "bash" {
		t.Errorf("the replayed call is %q, want bash", result.Replayed[0].Tool)
	}
}

func TestTheReplayReportsADifference(t *testing.T) {
	// A replayed call that is not the recorded one is a difference, and
	// the difference names both.
	same, differences := diffCalls(
		[]CallStep{{Index: 1, Turn: 1, Tool: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)}},
		[]CallStep{{Index: 1, Turn: 2, Tool: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)}},
	)
	if same {
		t.Error("the sequences matched, want a difference")
	}
	if len(differences) == 0 {
		t.Fatal("the difference was not described")
	}
	if !strings.Contains(differences[0], "bash") || !strings.Contains(differences[0], "read") {
		t.Errorf("the difference is %q, want both calls named", differences[0])
	}

	// A replay that stops early is a difference too.
	same, differences = diffCalls(
		[]CallStep{{Index: 1, Turn: 1, Tool: "bash"}},
		nil,
	)
	if same {
		t.Error("the sequences matched, want a difference")
	}
	if len(differences) != 1 {
		t.Fatalf("the replay reported %d differences, want 1: %v", len(differences), differences)
	}
	if !strings.Contains(differences[0], "1") {
		t.Errorf("the difference is %q, want the counts named", differences[0])
	}
}

func TestTheReplayNeedsARecordedConversation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0"}`), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}
	if _, err := Replay(path, ReplayOptions{Context: context.Background()}); err == nil {
		t.Fatal("the replay was accepted, want an error")
	}
}

func TestPooIsGivenATimeoutShorterThanTheHarnessDeadline(t *testing.T) {
	r := NewRunner(RunOptions{})
	for _, tc := range []struct {
		timeoutS int
		want     time.Duration
	}{
		{600, 590 * time.Second},
		{20, 18 * time.Second},
		{1, 900 * time.Millisecond},
	} {
		if got := r.pooTimeout(Task{TimeoutS: tc.timeoutS}); got != tc.want {
			t.Errorf("a %ds task gives poo %s, want %s", tc.timeoutS, got, tc.want)
		}
	}
}

func TestAFixtureRootOfItsOwnIsUsedWhenTheTaskSetIsElsewhere(t *testing.T) {
	// The task set and the fixtures live in unrelated directories, as
	// they do with a custom --tasks-dir and --fixtures-dir.
	fixtures := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(t.TempDir(), "mytasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:      []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:       []Task{task},
		TaskRoot:    taskRoot,
		FixtureRoot: fixtures,
		Seeds:       1,
		Out:         out,
		WorkRoot:    filepath.Join(out, "work"),
		PooBinary:   fakePoo(t, logWritingPoo),
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if len(report.Results) != 1 || !report.Results[0].Success {
		t.Errorf("the run did not pass with the fixtures root named: %+v", report.Results)
	}
}

func TestTheTailKeepsWholeCharacters(t *testing.T) {
	for n := 1; n < 8; n++ {
		if got := tail("a…b…c", n); !utf8.ValidString(got) || len(got) > n {
			t.Errorf("tail to %d gave %q", n, got)
		}
	}
	if got := tail("short", 10); got != "short" {
		t.Errorf("tail of a short string gave %q", got)
	}
}

func TestARunPooEndedByItsOwnTimeoutReportsTheTimeout(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	task.TimeoutS = 20

	// poo settles as cancelled because its deadline passed, and exits 130.
	binary := fakePoo(t, `
while [ "$#" -gt 0 ]; do
  case "$1" in
    --log) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
cat > "$log" <<'JSON'
{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s"}
{"record":"run.settled","seq":2,"ts":"2026-10-20T00:00:01Z","run_id":"r","reason":"cancelled","detail":{"source":"cancel:deadline","text":"context deadline exceeded"},"turns":1}
JSON
exit 130
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
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if got, want := report.Results[0].Error, "the run timed out after 18s"; got != want {
		t.Errorf("the error is %q, want %q", got, want)
	}
}

func TestARunTheHarnessKilledQuotesTheDeadlineItEnforced(t *testing.T) {
	fixtureRoot := fakeFixture(t, "fix-a", "old\n")
	taskRoot := filepath.Join(filepath.Dir(fixtureRoot), "tasks")
	task := fakeTask(t, taskRoot, "fix-a", "fix-a", "fixed")
	task.TimeoutS = 1

	out := t.TempDir()
	runner := NewRunner(RunOptions{
		Models:    []Model{{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}},
		Tasks:     []Task{task},
		TaskRoot:  taskRoot,
		Seeds:     1,
		Out:       out,
		WorkRoot:  filepath.Join(out, "work"),
		PooBinary: fakePoo(t, "sleep 30\n"),
	})
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("running the harness: %v", err)
	}
	if got := report.Results[0].Error; !strings.HasPrefix(got, "the run timed out after 1s") {
		t.Errorf("the error is %q, want it to name the 1s deadline the harness enforced", got)
	}
}

func TestOnlyTheDeadlineSourceIsATimeout(t *testing.T) {
	settled := func(source, text string) *Log {
		rec := LogRecord{Record: "run.settled", Reason: "cancelled"}
		rec.Details.Source = source
		rec.Details.Text = text
		return &Log{Records: []LogRecord{rec}}
	}
	if !pooTimedOut(settled("cancel:deadline", "")) {
		t.Error("a deadline source is not a timeout")
	}
	if pooTimedOut(settled("cancel:context", "context deadline exceeded")) {
		t.Error("a plain cancel whose text mentions a deadline is a timeout")
	}
	if pooTimedOut(nil) {
		t.Error("a missing log is a timeout")
	}
}
