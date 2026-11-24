//go:build unix

package repl

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/chubin/vt10x"
)

// TestThePagerReadsTheFileItself covers the pager's LESSOPEN guard:
// a lesspipe hook on the path re-colorizes the transcript before less
// reads it, and a highlighter turns the escape sequences into text.
// The pager clears the hook, so its less reads the file itself.
func TestThePagerReadsTheFileItself(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\necho LESSOPEN=[$LESSOPEN] LESSCLOSE=[$LESSCLOSE]\n"
	if err := os.WriteFile(filepath.Join(bin, "less"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("LESSOPEN", "||/usr/bin/lesspipe.sh %s")
	t.Setenv("LESSCLOSE", "/usr/bin/lesspipe.sh %s %s")

	vt := vt10x.New(vt10x.WithSize(40, 12))
	var record bytes.Buffer
	l := aLayout(t, io.MultiWriter(vt, &record), 40, 12)
	r := aStreamingRenderer(t, io.MultiWriter(vt, &record), 40, l)
	r.userPrompt("go")
	record.Reset()

	if err := r.page(strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	drawn := record.String()
	if !strings.Contains(drawn, "LESSOPEN=[]") || !strings.Contains(drawn, "LESSCLOSE=[]") {
		t.Fatalf("the pager's less inherited the LESSOPEN hook, which re-colorizes the transcript:\n%s", drawn)
	}
}

// TestThePagerHandsTheTerminalBackWithoutLess covers the pager's
// terminal handoff with a pager of the test's own: the region comes
// off before the pager writes and is reserved again after, whatever
// binary pages, so the contract does not wait for the CI image's less.
func TestThePagerHandsTheTerminalBackWithoutLess(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\necho PAGER RAN\n"
	if err := os.WriteFile(filepath.Join(bin, "less"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	found, err := exec.LookPath("less")
	t.Logf("lookpath found %q err %v", found, err)

	vt := vt10x.New(vt10x.WithSize(40, 12))
	var record bytes.Buffer
	out := io.MultiWriter(vt, &record)
	l := aLayout(t, out, 40, 12)
	// The renderer's out is the recorder too: the pager's own writes
	// land in the stream the assertions read.
	r := aStreamingRenderer(t, out, 40, l)
	r.userPrompt("go")
	record.Reset()

	if err := r.page(strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	t.Logf("record len %d content %q", record.Len(), record.String())

	drawn := record.String()
	if !strings.Contains(drawn, "PAGER RAN") {
		t.Fatalf("the pager never wrote to the terminal:\n%s", trimScreen(vt.String()))
	}
	reset := strings.Index(drawn, seqResetRegion)
	ran := strings.Index(drawn, "PAGER RAN")
	if reset < 0 || ran < 0 || reset > ran {
		t.Fatalf("the region was not reset before the pager wrote:\n%q", drawn)
	}
	region := regexp.MustCompile(`\x1b\[1;[0-9]+r`).FindStringIndex(drawn[ran:])
	if region == nil {
		t.Fatalf("the region was not reserved again after the pager:\n%q", drawn)
	}
}
