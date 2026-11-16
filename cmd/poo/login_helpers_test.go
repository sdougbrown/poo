package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"

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

// getCallback asks the wait's listener for the callback the flow names.
func getCallback(attempt auth.Attempt) error {
	redirect, err := url.Parse(attempt.Flow.RedirectURI)
	if err != nil {
		return err
	}
	target := fmt.Sprintf("%s://%s%s?code=the-code&state=%s", redirect.Scheme, redirect.Host, redirect.Path, url.QueryEscape(attempt.State))
	response, err := http.Get(target)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return nil
}
