package read_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo/tool/read"
)

// TestACarriageReturnIsStripped covers a file with CRLF line endings:
// the carriage return is not part of the line the model reads.
func TestACarriageReturnIsStripped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "crlf.txt", "a\r\nb\r\n")

	res := call(t, read.New(), map[string]any{"path": "crlf.txt"}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	lines := strings.SplitN(res.Text, "\n", 2)
	body := lines[1]
	if strings.Contains(body, "\r") {
		t.Errorf("the body holds a carriage return: %q", body)
	}
	if body != "a\nb\n" {
		t.Errorf("the body is %q, want the two lines", body)
	}
}

// TestACarriageReturnOnTheLastLineIsStripped covers a file whose last
// line has no newline after it.
func TestACarriageReturnOnTheLastLineIsStripped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "crlf.txt", "a\r\nb\r")

	res := call(t, read.New(), map[string]any{"path": "crlf.txt"}, dir)
	if res.IsError {
		t.Fatalf("the read failed: %s", res.Text)
	}
	body := strings.SplitN(res.Text, "\n", 2)[1]
	if strings.Contains(body, "\r") {
		t.Errorf("the body holds a carriage return: %q", body)
	}
	if body != "a\nb\n" {
		t.Errorf("the body is %q, want the two lines", body)
	}
}
