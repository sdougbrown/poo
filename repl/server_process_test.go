//go:build unix

package repl

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/chubin/vt10x"
)

func TestServerHasItsOwnProcessGroup(t *testing.T) {
	if os.Getenv("POO_TEST_SERVER_GROUP") == "1" {
		fmt.Println(syscall.Getpgrp())
		return
	}
	t.Setenv("POO_TEST_SERVER_GROUP", "1")
	m := &machine{opts: Options{ServerArgs: []string{"-test.run=^TestServerHasItsOwnProcessGroup$"}}}
	stdin, stdout, wait, err := m.server()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	group, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	if err := wait(); err != nil {
		t.Fatal(err)
	}
	if group == syscall.Getpgrp() {
		t.Fatal("the server shares the terminal's process group and receives its interrupts")
	}
}

// TestAServerStderrHoldsAPartialLine covers the stderr pipe's chunks:
// a write is not a row, so a diagnostic split across chunks reads as
// one line when its newline lands.
func TestAServerStderrHoldsAPartialLine(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 24))
	l := aLayout(t, vt, 40, 24)
	w := &layoutStderr{l: l}

	n, err := w.Write([]byte("a partial "))
	if n != len("a partial ") || err != nil {
		t.Fatalf("the partial chunk wrote %d bytes, err %v", n, err)
	}
	if drawn := trimScreen(vt.String()); strings.Contains(drawn, "a partial") {
		t.Fatalf("the partial chunk painted a row:\n%s", drawn)
	}
	if _, err := w.Write([]byte("line\n")); err != nil {
		t.Fatal(err)
	}
	drawn := trimScreen(vt.String())
	if n := strings.Count(drawn, "a partial line"); n != 1 {
		t.Errorf("the split diagnostic reads %d time(s), want 1:\n%s", n, drawn)
	}
}

func TestResizeKeepsServerDiagnosticsInTranscriptOrder(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.notice("before the diagnostic")
	r.streamDelta("m1", "text", "pending reply")
	stderr := &layoutStderr{l: scr.layout}
	_, _ = stderr.Write([]byte("server diagnostic"))
	stderr.flush()
	r.finish()
	r.notice("after the reply")

	scr.vt.Resize(36, 16)
	r.resize(36, 16, 1)
	scr.paint()
	for name, text := range map[string]string{"screen": scr.text(), "transcript": plain(r.transcriptText())} {
		last := -1
		for _, want := range []string{"before the diagnostic", "server diagnostic", "pending reply", "after the reply"} {
			i := strings.Index(text, want)
			if i <= last || strings.Count(text, want) != 1 {
				t.Fatalf("%s lost, duplicated, or moved %q:\n%s", name, want, text)
			}
			last = i
		}
	}
}

// TestServerStderrRidesTheLayout covers the diagnostics the default
// server writes while a footer layout owns the terminal: they land
// above the footer through the layout's locked writer, and the footer
// keeps its draft below them.
func TestServerStderrRidesTheLayout(t *testing.T) {
	if os.Getenv("POO_TEST_SERVER_STDERR") == "1" {
		fmt.Println("ready")
		fmt.Fprintln(os.Stderr, "a server diagnostic")
		fmt.Fprint(os.Stderr, "a final word")
		return
	}
	t.Setenv("POO_TEST_SERVER_STDERR", "1")
	scr, r := aScreen(t, 40)
	scr.typeText("a draft")
	m := &machine{
		r:    r,
		opts: Options{ServerArgs: []string{"-test.run=^TestServerStderrRidesTheLayout$"}},
	}
	stdin, stdout, wait, err := m.server()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	// The wait returns after the stderr copy is done, so the screen
	// below reads a settled frame.
	if err := wait(); err != nil {
		t.Fatal(err)
	}
	drawn := scr.text()
	for _, want := range []string{"a server diagnostic", "a final word"} {
		if n := strings.Count(drawn, want); n != 1 {
			t.Errorf("%q shows %d time(s) on the screen, want 1:\n%s", want, n, drawn)
		}
	}
	said := rowOf(t, scr, "a server diagnostic")
	footer := rowOf(t, scr, promptMark+"a draft")
	if said < 0 || footer < 0 || said > footer {
		t.Fatalf("the diagnostic did not land above the footer:\n%s", drawn)
	}
}
