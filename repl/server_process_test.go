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
