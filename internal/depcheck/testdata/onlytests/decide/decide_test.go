package decide

import "testing"

// TestDecide is a test of a package that holds no non-test Go files.
func TestDecide(t *testing.T) {
	if t.Name() == "" {
		t.Fatal("no name")
	}
}
