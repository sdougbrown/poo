package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnAcceptedBaselineHoldsTheReportFilesOnly(t *testing.T) {
	runDir := t.TempDir()
	if err := WriteReport(runDir, &Report{}); err != nil {
		t.Fatalf("writing the run's report: %v", err)
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
		want, err := os.ReadFile(filepath.Join(runDir, name))
		if err != nil {
			t.Fatalf("reading the run's %s: %v", name, err)
		}
		if string(data) != string(want) {
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

func TestAnAcceptRefusesTheBaselineItself(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "baseline")
	if err := os.MkdirAll(filepath.Join(baseline, "inner"), 0o755); err != nil {
		t.Fatalf("making the baseline: %v", err)
	}
	for _, dir := range []string{baseline, filepath.Join(baseline, "inner")} {
		for _, name := range reportFiles {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("kept\n"), 0o644); err != nil {
				t.Fatalf("writing %s: %v", name, err)
			}
		}
	}

	for _, runDir := range []string{baseline, filepath.Join(baseline, "inner")} {
		if err := AcceptBaseline(baseline, runDir); err == nil {
			t.Fatalf("AcceptBaseline(%s) accepted a directory inside the baseline", runDir)
		}
	}
	for _, name := range reportFiles {
		data, err := os.ReadFile(filepath.Join(baseline, name))
		if err != nil || string(data) != "kept\n" {
			t.Fatalf("the baseline's %s = %q, %v; want it left as it was", name, data, err)
		}
	}
}

func TestAnAcceptRefusesAnUnreadableReport(t *testing.T) {
	runDir := t.TempDir()
	for _, name := range reportFiles {
		if err := os.WriteFile(filepath.Join(runDir, name), []byte("not json\n"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	baseline := filepath.Join(t.TempDir(), "baseline")
	if err := os.MkdirAll(baseline, 0o755); err != nil {
		t.Fatalf("making the baseline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseline, reportJSONFile), []byte("kept\n"), 0o644); err != nil {
		t.Fatalf("writing the baseline: %v", err)
	}
	if err := AcceptBaseline(baseline, runDir); err == nil {
		t.Fatal("AcceptBaseline accepted a report it cannot read")
	}
	if data, err := os.ReadFile(filepath.Join(baseline, reportJSONFile)); err != nil || string(data) != "kept\n" {
		t.Fatalf("the baseline's report = %q, %v; want it left as it was", data, err)
	}
}
