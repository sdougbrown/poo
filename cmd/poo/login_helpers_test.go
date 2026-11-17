package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo/provider/auth"
)

// freePort asks the kernel for a port nothing is listening on.
func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// withStdin points os.Stdin at a pipe holding content, for the rest of the test.
func withStdin(t *testing.T, content string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe: %v", err)
	}
	if _, err := writer.WriteString(content); err != nil {
		t.Fatalf("filling the pipe: %v", err)
	}
	// The writer closes now, so a reader that outlasts the content sees end of input instead of
	// waiting for a line that never comes.
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the pipe: %v", err)
	}
	old := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = old
		_ = reader.Close()
	})
}

// codexAttemptForTest is a sign-in whose redirect is on a free port, so the wait can listen.
func codexAttemptForTest() (auth.Attempt, error) {
	port, err := freePort()
	if err != nil {
		return auth.Attempt{}, err
	}
	flow := auth.CodexLogin
	flow.RedirectURI = fmt.Sprintf("http://127.0.0.1:%d/auth/callback", port)
	return flow.Authorize()
}

// streamTo points one of the process's streams at a file, and returns what the file holds so
// far.
//
// A command that waits for a callback prints its URL before it gets there, so a test has to read
// the stream while the command is still running. A file is what two goroutines can hold without
// sharing a buffer.
func streamTo(t *testing.T, target **os.File) func() string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stream")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating the stream's file: %v", err)
	}
	old := *target
	*target = file
	t.Cleanup(func() {
		*target = old
		_ = file.Close()
	})
	return func() string {
		written, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading the stream's file: %v", err)
		}
		return string(written)
	}
}

// signInURL waits for a command's stderr to print the authorization URL, and returns it.
//
// The URL is printed before the wait listens, so a test that answers the callback asks the port
// until the listener takes it rather than trusting the print.
func signInURL(t *testing.T, printed func() string) *url.URL {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(printed(), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
				continue
			}
			if parsed, err := url.Parse(line); err == nil && parsed.Query().Get("state") != "" {
				return parsed
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the command never printed a sign-in URL; its stderr is\n%s", printed())
	return nil
}

// askCallback requests a path and query on the loopback a redirect URI names, retrying while the
// listener is still coming up, and returns the answer. An empty path is the redirect's own.
func askCallback(t *testing.T, redirectURI, path, query string) *http.Response {
	t.Helper()
	redirect, err := url.Parse(redirectURI)
	if err != nil {
		t.Fatalf("the redirect URI %q does not parse: %v", redirectURI, err)
	}
	if path == "" {
		path = redirect.Path
	}
	target := fmt.Sprintf("%s://%s%s%s", redirect.Scheme, redirect.Host, path, query)
	var last error
	for i := 0; i < 200; i++ {
		response, err := http.Get(target)
		if err == nil {
			return response
		}
		last = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("the callback listener never answered %s: %v", target, last)
	return nil
}
