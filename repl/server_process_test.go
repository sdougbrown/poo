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

// TestServerStderrRidesTheLayout covers the diagnostics the default
// server writes while a footer layout owns the terminal: they land
// above the footer through the layout's locked writer, and the footer
// keeps its draft below them.
func TestServerStderrRidesTheLayout(t *testing.T) {
	if os.Getenv("POO_TEST_SERVER_STDERR") == "1" {
		fmt.Println("ready")
		fmt.Fprintln(os.Stderr, "a server diagnostic")
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
	if n := strings.Count(drawn, "a server diagnostic"); n != 1 {
		t.Fatalf("the diagnostic shows %d time(s) on the screen, want 1:\n%s", n, drawn)
	}
	said := rowOf(t, scr, "a server diagnostic")
	footer := rowOf(t, scr, promptMark+"a draft")
	if said < 0 || footer < 0 || said > footer {
		t.Fatalf("the diagnostic did not land above the footer:\n%s", drawn)
	}
}
