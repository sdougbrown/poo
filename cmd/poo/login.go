package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo/provider/auth"
)

// ollamaCloud is the second way in: a key, with no browser and no redirect.
var ollamaCloud = auth.Flow{
	Name:     "Ollama Cloud",
	Provider: auth.Provider{ID: "ollama-cloud", Kind: auth.KindAPIKey},
	Method:   auth.MethodKey,
}

// logins are the sign-ins this command offers, in the order the picker shows them.
func logins() []auth.Flow {
	return []auth.Flow{auth.CodexLogin, ollamaCloud}
}

// loginOptions are the flags of `poo login`.
type loginOptions struct {
	sharedFlags
	// force replaces a credential the file already holds, without asking.
	force bool
	// noBrowser prints the sign-in URL instead of opening a browser.
	noBrowser bool
	// wait bounds how long a browser sign-in waits for its callback, in seconds.
	wait int
	// key is the key a key flow signs in with. Empty reads it from the prompt or stdin.
	key string
	// provider is the sign-in named on the command line, if one was.
	provider string
}

// bind reads the login command's flags out of a parsed invocation.
func (o *loginOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.force = inv.Bools["force"]
	o.noBrowser = inv.Bools["no-browser"]
	o.wait = inv.Ints["wait"]
	o.key = inv.Flags["key"]
	if len(inv.Args) > 0 {
		o.provider = inv.Args[0]
	}
}

// handleLogin executes `poo login`.
//
// The command signs in to a provider and stores the credential in poo's own file. Which provider,
// and how, is a choice: a bare `poo login` offers the list, and a name picks one. An existing
// credential is replaced only when asked — by --force, or by answering the prompt.
func handleLogin(ctx context.Context, inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o loginOptions
	o.bind(inv)
	flow, ok := o.choose(stderr)
	if !ok {
		return exit{usageExitCode}
	}
	settingsPath, settings, err := resolveSettings(o.settingsFile, "")
	if err != nil {
		fmt.Fprintf(stderr, "poo login: %v\n", err)
		return exit{usageExitCode}
	}
	target, err := pooCredentialFile(settingsPath, settings)
	if err != nil {
		fmt.Fprintf(stderr, "poo login: %v\n", err)
		return exit{usageExitCode}
	}
	credentials := auth.Credentials{Path: target}
	mayReplace, failed := replace(credentials, flow.Provider, o.force, "poo login", stderr)
	if failed {
		return exit{operationalExitCode}
	}

	entry, failed := o.signIn(ctx, flow, stdout, stderr)
	if failed {
		return exit{operationalExitCode}
	}
	status, err := credentials.Set(ctx, flow.Provider, entry, mayReplace)
	if err != nil {
		fmt.Fprintf(stderr, "poo login: %v\n", err)
		return exit{operationalExitCode}
	}
	fmt.Fprintf(stdout, "Signed in to %s", status.Provider)
	if status.AccountID != "" {
		fmt.Fprintf(stdout, " as %s", status.AccountID)
	}
	fmt.Fprintf(stdout, ".\nThe credential is in %s.\n", status.Path)
	// The sign-in is only worth making if poo reads it. A settings file that resolves the
	// credential to another tool's file still points at that file after the write.
	if reads := settings.CredentialFile(); reads != "" && !sameFile(reads, status.Path) {
		fmt.Fprintf(stdout, "\nThe settings still resolve the credential to %s. Point poo at the copy:\n  auth_file: %s\n", reads, status.Path)
	}
	return exit{}
}

// choose returns the sign-in the request names, or the one an operator picks from the list.
func (o *loginOptions) choose(stderr io.Writer) (auth.Flow, bool) {
	if o.provider != "" {
		for _, flow := range logins() {
			if slices.Contains(providerNames(flow), o.provider) || flow.Name == o.provider {
				return flow, true
			}
		}
		fmt.Fprintf(stderr, "poo login: %q is not a sign-in poo knows: %s\n", o.provider, strings.Join(loginNames(), ", "))
		return auth.Flow{}, false
	}
	if !interactive() {
		fmt.Fprintf(stderr, "poo login: name a provider: %s\n", strings.Join(loginNames(), ", "))
		return auth.Flow{}, false
	}
	fmt.Fprintln(stderr, "Sign in with:")
	for i, flow := range logins() {
		fmt.Fprintf(stderr, "  %d. %s\n", i+1, flow.Name)
	}
	fmt.Fprintf(stderr, "Which one? [1-%d] ", len(logins()))
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		fmt.Fprintln(stderr, "poo login: nothing was chosen")
		return auth.Flow{}, false
	}
	var choice int
	if _, err := fmt.Sscanf(strings.TrimSpace(scanner.Text()), "%d", &choice); err != nil ||
		choice < 1 || choice > len(logins()) {
		fmt.Fprintln(stderr, "poo login: that is not one of the sign-ins")
		return auth.Flow{}, false
	}
	return logins()[choice-1], true
}

// signIn runs the flow the operator chose.
func (o *loginOptions) signIn(ctx context.Context, flow auth.Flow, stdout, stderr io.Writer) (auth.Credential, bool) {
	if flow.Method == auth.MethodKey {
		return o.keyCredential(flow, stderr)
	}
	attempt, err := flow.Authorize()
	if err != nil {
		fmt.Fprintf(stderr, "poo login: %v\n", err)
		return auth.Credential{}, true
	}
	fmt.Fprintf(stderr, "Open this URL to sign in:\n\n  %s\n\n", attempt.URL)
	if !o.noBrowser {
		if err := openBrowser(attempt.URL); err != nil {
			fmt.Fprintf(stderr, "poo login: %v\n", err)
		}
	}
	fmt.Fprintln(stderr, "Waiting for the sign-in to come back. Paste the code or the URL here if")
	fmt.Fprintln(stderr, "the browser cannot reach this machine.")
	callback, err := waitForCallback(ctx, attempt, o.waitDuration(), nil, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "poo login: %v\n", err)
		return auth.Credential{}, true
	}
	credential, err := attempt.Exchange(ctx, nil, callback)
	if err != nil {
		fmt.Fprintf(stderr, "poo login: %v\n", err)
		return auth.Credential{}, true
	}
	return credential, false
}

// keyCredential reads the key a key flow signs in with.
//
// The key comes from --key, then the terminal's prompt, then stdin: a key typed into a shell's
// history is the one place it should not be.
func (o *loginOptions) keyCredential(flow auth.Flow, stderr io.Writer) (auth.Credential, bool) {
	key := o.key
	if key == "" && interactive() {
		fmt.Fprintf(stderr, "%s key: ", flow.Name)
		if read, err := readPassword(); err == nil {
			key = read
			fmt.Fprintln(stderr)
		}
	}
	if key == "" {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			key = strings.TrimSpace(scanner.Text())
		}
	}
	if key == "" {
		fmt.Fprintln(stderr, "poo login: no key was given")
		return auth.Credential{}, true
	}
	return auth.Credential{Kind: auth.KindAPIKey, Key: key}, false
}

// readPassword reads a line from the terminal without echoing it.
func readPassword() (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return "", fmt.Errorf("not a terminal")
	}
	hidden, err := hiddenLine(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	return hidden, nil
}

// waitDuration is how long a browser sign-in waits for its callback.
func (o *loginOptions) waitDuration() time.Duration {
	if o.wait > 0 {
		return time.Duration(o.wait) * time.Second
	}
	return 5 * time.Minute
}

// waitForCallback waits for the sign-in to come back: on the loopback the flow's redirect names,
// or as a value the operator pastes in.
//
// The two are one wait: whichever arrives first ends it. A machine whose loopback the browser
// cannot reach is the reason the paste exists.
func waitForCallback(ctx context.Context, attempt auth.Attempt, wait time.Duration, ready func(), stderr io.Writer) (auth.Callback, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	redirect, err := url.Parse(attempt.Flow.RedirectURI)
	if err != nil {
		return auth.Callback{}, fmt.Errorf("the redirect URI %q does not parse: %w", attempt.Flow.RedirectURI, err)
	}
	listener, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		return auth.Callback{}, fmt.Errorf("the callback cannot listen on %s: %w. Paste the code instead", redirect.Host, err)
	}
	defer listener.Close()
	if ready != nil {
		ready()
	}

	arrived := make(chan auth.Callback, 1)
	failed := make(chan error, 1)
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != redirect.Path {
			http.NotFound(w, r)
			return
		}
		callback := auth.ParseCallback(r.URL.String())
		writeCallbackPage(w, callback)
		select {
		case arrived <- callback:
		default:
		}
	})
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			select {
			case failed <- err:
			default:
			}
		}
	}()
	defer server.Close()

	pasted := make(chan auth.Callback, 1)
	if interactive() {
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				pasted <- auth.ParseCallback(scanner.Text())
			}
		}()
	}
	select {
	case callback := <-arrived:
		fmt.Fprintln(stderr, "The sign-in came back.")
		return callback, nil
	case callback := <-pasted:
		if callback.Code == "" && callback.Error == "" {
			return auth.Callback{}, fmt.Errorf("nothing was pasted")
		}
		return callback, nil
	case err := <-failed:
		return auth.Callback{}, fmt.Errorf("the callback server stopped: %w", err)
	case <-ctx.Done():
		return auth.Callback{}, fmt.Errorf("the sign-in did not come back in time: run it again, or open the URL yourself")
	}
}

// writeCallbackPage answers the browser with what it should see.
func writeCallbackPage(w http.ResponseWriter, callback auth.Callback) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	body := `<p>Signed in. You can close this page.</p>`
	if callback.Error != "" {
		body = `<p>The sign-in was refused.</p>`
		w.WriteHeader(http.StatusBadRequest)
	}
	fmt.Fprintf(w, "<!doctype html><meta charset=\"utf-8\"><title>poo</title>%s\n", body)
}

// openBrowser opens a URL in the operator's browser.
func openBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("the browser did not open: open the URL above yourself (%w)", err)
	}
	return nil
}

// providerNames are the words a sign-in answers to on the command line: the provider's id, and
// for a flow whose id is long, the short name an operator types.
func providerNames(flow auth.Flow) []string {
	short := flow.Provider.ID
	if at := strings.LastIndex(short, "-"); at >= 0 && short[at+1:] == "codex" {
		short = "codex"
	}
	return []string{short, flow.Provider.ID}
}

// loginNames are the words the list of sign-ins prints.
func loginNames() []string {
	out := make([]string, 0, len(logins()))
	for _, flow := range logins() {
		out = append(out, providerNames(flow)[0])
	}
	return out
}
