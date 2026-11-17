//go:build linux

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/sdougbrown/poo/provider/auth"
)

// The pty ioctls a terminal pair needs. Linux's syscall package names neither, so they are here.
const (
	ioctlPtyUnlock = 0x40045431 // TIOCSPTLCK: 0 unlocks the slave
	ioctlPtyNumber = 0x80045430 // TIOCGPTN: the slave's number
)

// withATerminal opens a real pty and points os.Stdin at its slave for the rest of the test,
// returning the master: what the test writes there is what the operator types.
//
// A command decides what its input can do by asking the descriptor, so a test of the paths that
// only a terminal takes needs a terminal, not a seam over the question.
func withATerminal(t *testing.T) *os.File {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("opening a pty: %v", err)
	}
	unlock := int32(0)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), ioctlPtyUnlock, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		t.Fatalf("unlocking the pty: %v", errno)
	}
	var number uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), ioctlPtyNumber, uintptr(unsafe.Pointer(&number))); errno != 0 {
		master.Close()
		t.Fatalf("naming the pty: %v", errno)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatalf("opening the pty's slave: %v", err)
	}
	old := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() {
		os.Stdin = old
		_ = slave.Close()
		_ = master.Close()
	})
	return master
}

// waitForOutput waits for a command's stream to hold a line, so a test types only once the
// command is where it says it is.
func waitForOutput(t *testing.T, printed func() string, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(printed(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the command never printed %q; its output is\n%s", want, printed())
}

// terminalAnswer returns what the terminal sent back to the master, waiting a moment for it.
//
// What the master receives is what a person would see on the screen: the echo of their typing.
// The command's own output does not reach it, because the test points the command's streams at
// files, so anything here is the terminal answering a read.
func terminalAnswer(t *testing.T, master *os.File) string {
	t.Helper()
	var collected strings.Builder
	buffer := make([]byte, 256)
	fd := int(master.Fd())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var readable syscall.FdSet
		if fd/64 >= len(readable.Bits) {
			t.Fatalf("the terminal's descriptor %d is past the set select reads", fd)
		}
		readable.Bits[fd/64] |= 1 << (uint(fd) % 64)
		timeout := syscall.NsecToTimeval(int64(250 * time.Millisecond))
		ready, err := syscall.Select(fd+1, &readable, nil, nil, &timeout)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			t.Fatalf("waiting for the terminal to answer: %v", err)
		}
		// Nothing came back inside the window, so the terminal answered nothing at all.
		if ready == 0 {
			return collected.String()
		}
		got, err := master.Read(buffer)
		if err != nil {
			return collected.String()
		}
		collected.Write(buffer[:got])
	}
	return collected.String()
}

// TestAPastedURLCompletesTheSignIn covers the way back for a machine the browser cannot reach:
// the operator pastes the URL the browser landed on into the terminal the wait is reading, and
// the code its query carries is traded against the state beside it.
func TestAPastedURLCompletesTheSignIn(t *testing.T) {
	isolatedConfigHome(t)
	dir := t.TempDir()
	settings := loginSettings(t, dir)

	access := codexAccessToken(t, "the-account")
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"the-refresh-token","expires_in":3600}`, access)
	}))
	defer endpoint.Close()

	old := auth.CodexLogin
	defer func() { auth.CodexLogin = old }()
	flow := codexFlowForTest(t)
	flow.TokenURL = endpoint.URL
	auth.CodexLogin = flow

	master := withATerminal(t)
	stderr := streamTo(t, &os.Stderr)
	stdout := streamTo(t, &os.Stdout)
	exited := make(chan int, 1)
	go func() {
		exited <- run([]string{"login", "codex", "--settings", settings, "--no-browser"})
	}()

	// The code comes back to the browser, and the operator hands the URL it landed on to the
	// terminal.
	authorization := signInURL(t, stderr)
	waitForOutput(t, stderr, "paste the URL")
	pasted := fmt.Sprintf("%s?code=the-code&state=%s", flow.RedirectURI, url.QueryEscape(authorization.Query().Get("state")))
	if _, err := fmt.Fprintf(master, "%s\n", pasted); err != nil {
		t.Fatalf("pasting the URL: %v", err)
	}

	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("poo login exited %d; its stderr is\n%s", code, stderr())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("poo login did not finish; its stderr is\n%s", stderr())
	}
	if printed := stdout(); !strings.Contains(printed, "Signed in to openai-codex") {
		t.Errorf("the report is %q, want it to say which provider signed in", printed)
	}
	entry, err := (auth.Credentials{Path: filepath.Join(dir, "auth.json")}).Read(auth.Codex)
	if err != nil {
		t.Fatalf("reading the credential back: %v", err)
	}
	if entry.Access != access || entry.Refresh != "the-refresh-token" {
		t.Errorf("the credential is %+v, want the token the paste was traded for", entry)
	}
}

// TestAPasteWithNothingInItFailsTheSignIn covers the guard on the paste: an empty line is not the
// sign-in coming back, so the command says so rather than waiting out its bound.
func TestAPasteWithNothingInItFailsTheSignIn(t *testing.T) {
	isolatedConfigHome(t)
	dir := t.TempDir()
	settings := loginSettings(t, dir)

	old := auth.CodexLogin
	defer func() { auth.CodexLogin = old }()
	auth.CodexLogin = codexFlowForTest(t)

	master := withATerminal(t)
	stderr := streamTo(t, &os.Stderr)
	streamTo(t, &os.Stdout)
	exited := make(chan int, 1)
	go func() {
		exited <- run([]string{"login", "codex", "--settings", settings, "--no-browser"})
	}()

	signInURL(t, stderr)
	waitForOutput(t, stderr, "paste the URL")
	if _, err := fmt.Fprint(master, "\n"); err != nil {
		t.Fatalf("pasting an empty line: %v", err)
	}

	select {
	case code := <-exited:
		if code != operationalExitCode {
			t.Errorf("the exit code is %d, want %d", code, operationalExitCode)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("poo login did not finish; its stderr is\n%s", stderr())
	}
	if printed := stderr(); !strings.Contains(printed, "nothing was pasted") {
		t.Errorf("the failure is %q, want it to say nothing was pasted", printed)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); err == nil {
		t.Error("the failed sign-in stored a credential")
	}
}

// TestTheKeyPromptReadsTheKey covers the terminal's own prompt: a person types the key without it
// being echoed, and the key that is stored is the one they typed.
func TestTheKeyPromptReadsTheKey(t *testing.T) {
	isolatedConfigHome(t)
	dir := t.TempDir()
	settings := loginSettings(t, dir)

	const typed = "the-typed-key"
	master := withATerminal(t)
	stderr := streamTo(t, &os.Stderr)
	stdout := streamTo(t, &os.Stdout)
	exited := make(chan int, 1)
	go func() {
		exited <- run([]string{"login", "ollama-cloud", "--force", "--settings", settings})
	}()

	waitForOutput(t, stderr, "Ollama Cloud key:")
	if _, err := fmt.Fprintf(master, "%s\n", typed); err != nil {
		t.Fatalf("typing the key: %v", err)
	}

	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("poo login exited %d; its stderr is\n%s", code, stderr())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("poo login did not finish; its stderr is\n%s", stderr())
	}
	if printed := stdout(); !strings.Contains(printed, "Signed in to ollama-cloud") {
		t.Errorf("the report is %q, want it to say which provider signed in", printed)
	}
	entry, err := (auth.Credentials{Path: filepath.Join(dir, "auth.json")}).Read(auth.OllamaCloud)
	if err != nil {
		t.Fatalf("reading the credential back: %v", err)
	}
	if entry.Kind != auth.KindAPIKey || entry.Key != typed {
		t.Errorf("the credential is %+v, want the key that was typed", entry)
	}
	// The read is the hidden one, so the terminal put nothing on the screen: an echoed read
	// would have left the key on the master's side of the pair.
	if echoed := terminalAnswer(t, master); strings.Contains(echoed, typed) {
		t.Errorf("the terminal echoed the key: %q", echoed)
	}
}

// TestTheReplacePromptReplacesTheKeyWhenTheOperatorSaysYes covers the other way to replace a
// credential: the operator answers the prompt, and the run goes on to take the key they type.
func TestTheReplacePromptReplacesTheKeyWhenTheOperatorSaysYes(t *testing.T) {
	isolatedConfigHome(t)
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	target := filepath.Join(dir, "auth.json")
	seeded := "version: 1\ncredentials:\n  ollama-cloud:\n    type: api_key\n    key: already-there\n"
	if err := os.WriteFile(target, []byte(seeded), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}

	const typed = "the-new-key"
	master := withATerminal(t)
	stderr := streamTo(t, &os.Stderr)
	stdout := streamTo(t, &os.Stdout)
	exited := make(chan int, 1)
	go func() {
		exited <- run([]string{"login", "ollama-cloud", "--settings", settings})
	}()

	waitForOutput(t, stderr, "Replace it? [y/N]")
	if _, err := fmt.Fprint(master, "y\n"); err != nil {
		t.Fatalf("answering the prompt: %v", err)
	}
	// Each answer is one line read on its own, so the key is typed only once the key's prompt
	// is up: a line written before that read starts is the confirmation's to read.
	waitForOutput(t, stderr, "Ollama Cloud key:")
	if _, err := fmt.Fprintf(master, "%s\n", typed); err != nil {
		t.Fatalf("typing the key: %v", err)
	}

	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("poo login exited %d; its stderr is\n%s", code, stderr())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("poo login did not finish; its stderr is\n%s", stderr())
	}
	if printed := stdout(); !strings.Contains(printed, "Signed in to ollama-cloud") {
		t.Errorf("the report is %q, want it to say which provider signed in", printed)
	}
	entry, err := (auth.Credentials{Path: target}).Read(auth.OllamaCloud)
	if err != nil {
		t.Fatalf("reading the credential back: %v", err)
	}
	if entry.Key != typed {
		t.Errorf("the credential is %+v, want the key the operator answered for", entry)
	}
}

// TestTheReplacePromptLeavesTheCredentialWhenTheOperatorSaysNo covers the answer that is not
// yes: the run stops there, and the credential is the bytes it already had.
func TestTheReplacePromptLeavesTheCredentialWhenTheOperatorSaysNo(t *testing.T) {
	isolatedConfigHome(t)
	dir := t.TempDir()
	settings := loginSettings(t, dir)
	target := filepath.Join(dir, "auth.json")
	seeded := "version: 1\ncredentials:\n  ollama-cloud:\n    type: api_key\n    key: already-there\n"
	if err := os.WriteFile(target, []byte(seeded), 0o600); err != nil {
		t.Fatalf("writing the credential file: %v", err)
	}

	master := withATerminal(t)
	stderr := streamTo(t, &os.Stderr)
	streamTo(t, &os.Stdout)
	exited := make(chan int, 1)
	go func() {
		exited <- run([]string{"login", "ollama-cloud", "--settings", settings})
	}()

	waitForOutput(t, stderr, "Replace it? [y/N]")
	if _, err := fmt.Fprint(master, "n\n"); err != nil {
		t.Fatalf("answering the prompt: %v", err)
	}

	select {
	case code := <-exited:
		if code != operationalExitCode {
			t.Errorf("the exit code is %d, want %d", code, operationalExitCode)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("poo login did not finish; its stderr is\n%s", stderr())
	}
	if printed := stderr(); !strings.Contains(printed, "the credential was left as it was") {
		t.Errorf("the failure is %q, want it to say the credential was left as it was", printed)
	}
	stored, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading the credential file: %v", err)
	}
	if string(stored) != seeded {
		t.Errorf("the file is\n%s\nwant it untouched:\n%s", stored, seeded)
	}
}

// TestTheKeyPromptWithNothingTypedIsAFailure covers the terminal's prompt answered with nothing:
// the key is not there, and the read that would ask again on the same terminal has no prompt to
// ask on, so the run refuses instead of falling through to it.
func TestTheKeyPromptWithNothingTypedIsAFailure(t *testing.T) {
	isolatedConfigHome(t)
	dir := t.TempDir()
	settings := loginSettings(t, dir)

	master := withATerminal(t)
	stderr := streamTo(t, &os.Stderr)
	streamTo(t, &os.Stdout)
	exited := make(chan int, 1)
	go func() {
		exited <- run([]string{"login", "ollama-cloud", "--settings", settings})
	}()

	waitForOutput(t, stderr, "Ollama Cloud key:")
	if _, err := fmt.Fprint(master, "\n"); err != nil {
		t.Fatalf("pressing enter at the prompt: %v", err)
	}

	select {
	case code := <-exited:
		if code != operationalExitCode {
			t.Errorf("the exit code is %d, want %d", code, operationalExitCode)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("poo login waited on an empty prompt; its stderr is\n%s", stderr())
	}
	if printed := stderr(); !strings.Contains(printed, "no key was given") {
		t.Errorf("the failure is %q, want it to say no key was given", printed)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); err == nil {
		t.Error("the failed sign-in stored a credential")
	}
}
