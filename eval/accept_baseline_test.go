package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnAcceptedBaselineHoldsTheReportFilesOnly(t *testing.T) {
	runDir := t.TempDir()
	for _, name := range reportFiles {
		if err := os.WriteFile(filepath.Join(runDir, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	// The run's own scratch: the raw runs and the comparison stay out
	// of the baseline.
	if err := os.MkdirAll(filepath.Join(runDir, "runs", "task", "model", "seed1"), 0o755); err != nil {
		t.Fatalf("making the runs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "runs", "task", "model", "seed1", "session.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("writing the transcript: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, "comparison"), 0o755); err != nil {
		t.Fatalf("making the comparison directory: %v", err)
	}

	baseline := filepath.Join(t.TempDir(), "baseline")
	if err := os.MkdirAll(baseline, 0o755); err != nil {
		t.Fatalf("making the baseline: %v", err)
	}
	stale := filepath.Join(baseline, "report.md")
	if err := os.WriteFile(stale, []byte("the old baseline\n"), 0o644); err != nil {
		t.Fatalf("writing the old baseline: %v", err)
	}

	if err := AcceptBaseline(baseline, runDir); err != nil {
		t.Fatalf("accepting the baseline: %v", err)
	}

	for _, name := range reportFiles {
		data, err := os.ReadFile(filepath.Join(baseline, name))
		if err != nil {
			t.Fatalf("reading the baseline's %s: %v", name, err)
		}
		if string(data) != "# "+name+"\n" {
			t.Errorf("the baseline's %s is %q, want the run's report", name, data)
		}
	}
	entries, err := os.ReadDir(baseline)
	if err != nil {
		t.Fatalf("reading the baseline: %v", err)
	}
	if len(entries) != len(reportFiles) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the baseline holds %v, want only the report files", names)
	}
}

func TestAnAcceptRefusesADirectoryWithoutAReport(t *testing.T) {
	runDir := t.TempDir()
	baseline := filepath.Join(t.TempDir(), "baseline")
	if err := os.MkdirAll(baseline, 0o755); err != nil {
		t.Fatalf("making the baseline: %v", err)
	}
	before := filepath.Join(baseline, "report.json")
	if err := os.WriteFile(before, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("writing the old baseline: %v", err)
	}

	if err := AcceptBaseline(baseline, runDir); err == nil {
		t.Fatal("accepting a directory without a report: expected an error")
	}
	data, err := os.ReadFile(before)
	if err != nil {
		t.Fatalf("reading the old baseline: %v", err)
	}
	if string(data) != "{}\n" {
		t.Errorf("the refused accept changed the baseline: %s is %q", before, data)
	}
}
