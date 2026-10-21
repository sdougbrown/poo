package read

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestAReadRangeStopsOnADoneContext covers the scan-loop's context
// check: a range whose context is already done fails without reading,
// so a cancelled run stops paying for the scan.
func TestAReadRangeStopsOnADoneContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the file: %v", err)
	}
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, _, _, _, err = readRange(ctx, f, 1, 100, 1000)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the scan returned %v, want context.Canceled", err)
	}
}
