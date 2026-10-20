package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAWrittenReportHoldsNoAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	report := &Report{
		Dir: dir,
		Results: []RunResult{{
			LogPath: filepath.Join(dir, "runs", "run-1", "t", "m", "seed1", "session.jsonl"),
		}},
	}
	if err := WriteReport(dir, report); err != nil {
		t.Fatalf("writing the report: %v", err)
	}
	for _, name := range []string{"report.json", "report.md", "summary.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if strings.Contains(string(data), dir) {
			t.Errorf("%s holds the absolute path %s", name, dir)
		}
	}
	if !filepath.IsAbs(report.Results[0].LogPath) {
		t.Error("writing the report changed the caller's log path")
	}
}

func TestARelativeReportDirStillWritesRelativePaths(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	dir := filepath.Join("results", "r")
	report := &Report{
		Dir: filepath.Join(base, dir),
		Results: []RunResult{{
			LogPath: filepath.Join(base, dir, "runs", "run-1", "t", "m", "seed1", "session.jsonl"),
		}},
	}
	if err := WriteReport(dir, report); err != nil {
		t.Fatalf("writing the report: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	if strings.Contains(string(data), base) {
		t.Errorf("report.md holds the absolute path %s", base)
	}
}
