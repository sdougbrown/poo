package eval

import (
	"os"
	"path/filepath"
	"testing"
)

// The re-read metric counts a file range read twice.
//
// A shell read names a file and a range. The range is what makes two
// reads of the same lines a re-read, and the file is what keeps two
// different files apart.

func TestTheReReadRateTellsTheFilesApart(t *testing.T) {
	// The same line range of two different files is not a re-read.
	path := writeLog(t,
		callRecord(1, "bash", `{"command":"sed -n '10,20p' a.go"}`),
		resultRecord(1, "bash", "ok"),
		callRecord(2, "bash", `{"command":"sed -n '10,20p' b.go"}`),
		resultRecord(2, "bash", "ok"),
		settledRecord("end_turn", 2),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.Reads != 2 {
		t.Errorf("the run made %d reads, want 2", m.Reads)
	}
	if m.Rereads != 0 {
		t.Errorf("the run made %d re-reads, want 0", m.Rereads)
	}
}

func TestTheReReadRateTellsTheRangesApart(t *testing.T) {
	// Two different ranges of the same file are not a re-read.
	path := writeLog(t,
		callRecord(1, "bash", `{"command":"sed -n '10,20p' a.go"}`),
		resultRecord(1, "bash", "ok"),
		callRecord(2, "bash", `{"command":"sed -n '30,40p' a.go"}`),
		resultRecord(2, "bash", "ok"),
		settledRecord("end_turn", 2),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.Rereads != 0 {
		t.Errorf("the run made %d re-reads, want 0", m.Rereads)
	}
}

func TestTheReReadRateTellsTheToolsApart(t *testing.T) {
	// A `cat` of a file and a `sed -n` of a range of the same file
	// overlap, so the second one is a re-read.
	path := writeLog(t,
		callRecord(1, "bash", `{"command":"cat a.go"}`),
		resultRecord(1, "bash", "ok"),
		callRecord(2, "bash", `{"command":"sed -n '10,20p' a.go"}`),
		resultRecord(2, "bash", "ok"),
		settledRecord("end_turn", 2),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	m := ComputeMetrics(log, Diff{})

	if m.Rereads != 1 {
		t.Errorf("the run made %d re-reads, want 1", m.Rereads)
	}
}

func TestTheShellReadsNameTheFile(t *testing.T) {
	// The metrics count a read of a file, and the file is the one the
	// command names.
	cases := []struct {
		name    string
		command string
		want    string
	}{
		{"cat", "cat src/main.go", "src/main.go"},
		{"head", "head -n 20 src/main.go", "src/main.go"},
		{"head with a long option", "head --lines 20 src/main.go", "src/main.go"},
		{"tail", "tail -n 5 src/main.go", "src/main.go"},
		{"tail with a plus", "tail -n +5 src/main.go", "src/main.go"},
		{"sed with a range", "sed -n '10,20p' src/main.go", "src/main.go"},
		{"sed with one line", "sed -n '10p' src/main.go", "src/main.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reads := shellReads(c.command)
			if len(reads) != 1 {
				t.Fatalf("the command made %d reads, want 1: %+v", len(reads), reads)
			}
			if reads[0].path != c.want {
				t.Errorf("the read is of %q, want %q", reads[0].path, c.want)
			}
		})
	}
}

func TestTheShellReadsDoNotCountANumberAsAFile(t *testing.T) {
	// `head -n 20 file` reads the file, not the number.
	reads := shellReads("head -n 20 src/main.go")
	for _, r := range reads {
		if r.path == "20" || r.path == "-n" {
			t.Errorf("the read is of %q, want the file", r.path)
		}
	}
}

func TestTheShellReadsNormaliseThePath(t *testing.T) {
	// The same file under two names is one file, so reading it twice
	// is a re-read.
	reads := shellReads("cat ./src/main.go; cat src/main.go")
	if len(reads) != 2 {
		t.Fatalf("the command made %d reads, want 2: %+v", len(reads), reads)
	}
	if reads[0].path != reads[1].path {
		t.Errorf("the reads are of %q and %q, want the same file", reads[0].path, reads[1].path)
	}
}

// A log whose last line was torn by a kill keeps the records before it.
//
// A run that was killed mid-write leaves a partial record at the end.
// The records before it are good, and a metric computed from them is
// better than no metric at all.

func TestATornLastLineKeepsTheRecordsBeforeIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s"}` + "\n" +
		`{"record":"usage","seq":2,"ts":"2026-10-20T00:00:01Z","run_id":"r","turn":1,"request":1,"input_tokens":100,"output_tokens":20,"ttft_ms":50}` + "\n" +
		`{"record":"run.settled","seq":3,"ts":"2026-10-20T00:00:02Z","run_id":"r","re`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}

	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	if len(log.Records) != 2 {
		t.Fatalf("the log holds %d records, want the two complete ones", len(log.Records))
	}
	m := ComputeMetrics(log, Diff{})
	if m.InputTokens != 100 {
		t.Errorf("the input tokens are %d, want 100", m.InputTokens)
	}
	if m.Turns != 1 {
		t.Errorf("the turns are %d, want 1", m.Turns)
	}
}

func TestAMalformedLineInTheMiddleIsAnError(t *testing.T) {
	// A record in the middle that does not decode means the log is not
	// the log it claims to be, and a metric from it would be a guess.
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s"}` + "\n" +
		`{"record":"usage","seq":2,"ts":"2026-10-20T00:00:01Z","run_id":"r"` + "\n" +
		`{"record":"usage","seq":3,"ts":"2026-10-20T00:00:02Z","run_id":"r","turn":1,"request":1,"input_tokens":100,"output_tokens":20,"ttft_ms":50}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}

	if _, err := ParseLog(path); err == nil {
		t.Error("the malformed log was accepted, want an error")
	}
}
