package main

import (
	"context"
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

// isolatedConfigHome points the default credential paths at directories of the test's, so a
// command cannot read or write the operator's own.
func isolatedConfigHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
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

// callbackWait is how long a test's own callback wait holds its listener. It outlives the
// window askCallback asks within: a wait that gives up while asks are still standing takes its
// port down with it, and the asks that follow read as a connection refused rather than as
// whatever the wait had to say.
const callbackWait = 30 * time.Second

// holdCallbackListener opens the listener a callback wait will hold, and hands it to the wait.
//
// The wait binds this listener, not a port that was free a moment ago: the kernel hands out the
// port and the test keeps it bound until the wait has it, so nothing can take it in between.
func holdCallbackListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("opening the callback's listener: %v", err)
	}
	listened := listenOn
	listenOn = func(string, string) (net.Listener, error) {
		return listener, nil
	}
	t.Cleanup(func() {
		listenOn = listened
		_ = listener.Close()
	})
	return listener
}

// codexFlowForTest is the Codex sign-in on a loopback the test holds open, so the wait binds the
// listener its redirect names.
func codexFlowForTest(t *testing.T) auth.Flow {
	t.Helper()
	listener := holdCallbackListener(t)
	flow := auth.CodexLogin
	flow.RedirectURI = fmt.Sprintf("http://%s/auth/callback", listener.Addr())
	return flow
}

// codexAttemptForTest is a sign-in whose redirect is on a listener the test holds open.
func codexAttemptForTest(t *testing.T) auth.Attempt {
	t.Helper()
	attempt, err := codexFlowForTest(t).Authorize()
	if err != nil {
		t.Fatalf("the attempt: %v", err)
	}
	return attempt
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
// The URL is printed before the wait serves the callback, so a test that answers it asks until
// the answer comes rather than trusting the print.
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
// A caller may pass one stop channel: when a dial fails after the wait has ended — its answer
// was handled without the client seeing the page, or the wait gave up — the ask returns nil,
// and the caller asserts on the wait's own outcome instead of the page it never saw.
func askCallback(t *testing.T, redirectURI, path, query string, stop ...<-chan struct{}) *http.Response {
	t.Helper()
	redirect, err := url.Parse(redirectURI)
	if err != nil {
		t.Fatalf("the redirect URI %q does not parse: %v", redirectURI, err)
	}
	if path == "" {
		path = redirect.Path
	}
	target := fmt.Sprintf("%s://%s%s%s", redirect.Scheme, redirect.Host, path, query)
	var stopped <-chan struct{}
	if len(stop) > 0 {
		stopped = stop[0]
	}
	var last error
	// The window bounds the whole retry, not the count. A listener that
	// accepts but never answers costs one 2s request at a time.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		if err != nil {
			t.Fatalf("building the callback request: %v", err)
		}
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		response, err := http.DefaultClient.Do(request.WithContext(ctx))
		cancel()
		if err == nil {
			return response
		}
		select {
		case <-stopped:
			return nil
		default:
		}
		last = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the callback listener never answered %s: %v", target, last)
	return nil
}
