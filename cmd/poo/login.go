package main

import (
	"bufio"
	"context"
	"errors"
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
	"syscall"
	"time"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/internal/term"
	"github.com/sdougbrown/poo/provider/auth"
	"github.com/sdougbrown/poo/provider/catalog"
)

// ollamaCloud is the second way in: a key, with no browser and no redirect.
var ollamaCloud = auth.Flow{
	Name:     "Ollama Cloud",
	Provider: auth.OllamaCloud,
	Method:   auth.MethodKey,
}

// opencode is the third way in: an OpenCode Console key, which the pay-as-you-go
// and the Go endpoints both read.
var opencode = auth.Flow{
	Name:     "OpenCode",
	Provider: auth.Opencode,
	Method:   auth.MethodKey,
}

// typesafe is the fourth way in: a TypeSafe key, which the decisions endpoint
// reads and no chat one does.
var typesafe = auth.Flow{
	Name:     "TypeSafe",
	Provider: auth.Typesafe,
	Method:   auth.MethodKey,
}

// google is the fifth way in: a Gemini API key, which the AI Studio console
// issues and the Interactions endpoint reads.
var google = auth.Flow{
	Name:     "Google Gemini",
	Provider: auth.Google,
	Method:   auth.MethodKey,
}

// googlePlan is the sixth way in: the Google AI plan's own sign-in, which needs
// the operator's app client and answers on the plan's quota. It is offered with
// what is missing named, so a client that is not registered yet reads as one.
func googlePlan() auth.Flow {
	return auth.GoogleAIPlanFlow()
}

// probeClient is the client a sign-in's probe uses. The binary leaves it nil, and
// the catalog makes its own; a test stands a fixture in its place, the way listenOn
// stands in for a listener, because a probe reaches a provider and no test does.
var probeClient *http.Client

// logins are the sign-ins this command offers, in the order the picker shows them.
func logins() []auth.Flow {
	return []auth.Flow{auth.CodexLogin, ollamaCloud, opencode, typesafe, google, googlePlan()}
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
	// waitSet reports whether --wait was typed, which a key flow refuses: the value alone
	// cannot tell a request from the default.
	waitSet bool
	// provider is the sign-in named on the command line, if one was.
	provider string
	// command is the name the shared sign-in's diagnostics answer by: the
	// command that runs them, so a picker the welcome front door shares
	// does not answer as a command the operator never typed.
	command string
}

// bind reads the login command's flags out of a parsed invocation.
func (o *loginOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.force = inv.Bools["force"]
	o.noBrowser = inv.Bools["no-browser"]
	o.wait = inv.Ints["wait"]
	o.waitSet = inv.Supplied["wait"]
	if len(inv.Args) > 0 {
		o.provider = inv.Args[0]
	}
}

// handleLogin executes `poo login`.
//
// The command signs in to a provider and stores the credential in poo's own file. Which provider,
// and how, is a choice: a bare `poo login` offers the list, and a name picks one. An existing
// credential is replaced only when asked — by --force, or by answering the prompt.
//
// A key is typed into the terminal's prompt, or read from stdin. There is no flag for it: an
// argument is visible in the process list and stays in the shell's history.
func handleLogin(ctx context.Context, inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o loginOptions
	o.bind(inv)
	o.command = "poo login"
	// The list is the question, and a run with no terminal to ask on has no answer to it, so it
	// says what to type instead of guessing.
	if o.provider == "" && !interactive() {
		fmt.Fprintf(stderr, "poo login: name a provider: %s\n", strings.Join(loginNames(), ", "))
		return exit{usageExitCode}
	}
	flow, ok := o.choose(bufio.NewScanner(os.Stdin), stderr)
	if !ok {
		return exit{usageExitCode}
	}
	if err := o.refuseBrowserFlags(flow); err != nil {
		fmt.Fprintf(stderr, "poo login: %v\n", err)
		return exit{usageExitCode}
	}
	settingsPath, settings, err := resolveSettings(o.settingsFile, "")
	if err != nil {
		fmt.Fprintf(stderr, "poo login: %v\n", err)
		return exit{usageExitCode}
	}
	if _, failed := o.signInAndStore(ctx, flow, settingsPath, settings, nil, stdout, stderr); failed {
		return exit{operationalExitCode}
	}
	return exit{}
}

// signInAndStore runs the sign-in the flow names and writes the credential to poo's
// own file. It is the middle the login command and the welcome front door share. It
// resolves the file the settings name, asks the replace question, runs the sign-in,
// and writes the credential. The operator hears where it went. The lines the key
// flows read come from in, which a multi-step flow owns so one scan of its input
// feeds every answer. A caller with no flow of its own passes nil, and a sign-in
// reads the terminal's own input.
func (o *loginOptions) signInAndStore(ctx context.Context, flow auth.Flow, settingsPath string, settings *config.Settings, in *bufio.Scanner, stdout, stderr io.Writer) (auth.Status, bool) {
	target, err := pooCredentialFile(settingsPath, settings)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", o.command, err)
		return auth.Status{}, true
	}
	credentials := auth.Credentials{Path: target, ReadOnly: settings.AuthReadonlyEnabled()}
	// The refusal comes before the sign-in, not after it: a credential the file cannot
	// hold is not one to spend a grant on.
	if credentials.ReadOnly {
		fmt.Fprintf(stderr, "%s: %v\n", o.command, fmt.Errorf("%w: poo does not write it", auth.ErrReadOnly))
		return auth.Status{}, true
	}
	mayReplace, failed := replace(credentials, flow.Provider, o.force, o.command, in, stderr)
	if failed {
		return auth.Status{}, true
	}
	entry, failed := o.signIn(ctx, flow, in, stdout, stderr)
	if failed {
		return auth.Status{}, true
	}
	status, err := credentials.Set(ctx, flow.Provider, entry, mayReplace)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", o.command, err)
		return auth.Status{}, true
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
	o.probeSignedIn(ctx, flow, settingsPath, stdout)
	return status, false
}

// probeSignedIn asks the endpoints the provider serves what they serve, now that its
// credential is stored.
//
// A hosted provider's models are its own list, so the key is worth nothing to a session
// until the list is read: without this the first `poo models`, or a `/models` in the repl,
// is what teaches the catalog what the operator just bought. The settings are read again
// because the offering is decided when they load, and the key that wakes an endpoint was
// not there when the caller loaded them.
//
// A provider whose endpoints declare their models — Codex, TypeSafe — has nothing to ask,
// and a probe that fails leaves the catalog as it was: a sign-in that stored the credential
// is a success whatever the endpoint answers.
func (o *loginOptions) probeSignedIn(ctx context.Context, flow auth.Flow, settingsPath string, stdout io.Writer) {
	settings, err := o.reloadSettings(settingsPath)
	if err != nil {
		return
	}
	options := settings.CatalogOptions()
	options.HTTPClient = probeClient
	// The probes name the client, the way every other catalog build in the binary
	// does: a provider that gates or reads its traffic by user agent sees poo.
	options.UserAgent = userAgent()
	// The cached layer is not applied here: this catalog probes below, and a refresh
	// reads the cache itself, so applying it twice reads the file twice and warns
	// twice when it cannot be read. catalog.Build strips it for the same reason.
	options.Cached = false
	cat := catalog.New(options)
	served := make([]string, 0, 4)
	for name, endpoint := range cat.Endpoints() {
		if endpoint.SkipProbe || !config.ReadsProviderKey(endpoint.Credential, flow.Provider) {
			continue
		}
		_ = cat.ReprobeEndpoint(ctx, name)
		served = append(served, name)
	}
	if len(served) == 0 {
		return
	}
	// The count is what the probe reported, not what a layer declares: a key the
	// endpoint refuses leaves the record's placeholder model in place, and a sign-in
	// that says the provider serves one model at that moment says nothing useful.
	count := 0
	for _, name := range served {
		count += cat.Reported(name)
	}
	if count == 0 {
		return
	}
	fmt.Fprintf(stdout, "\n%s serves %d model(s), and they are in the catalog.\n", strings.Join(served, ", "), count)
}

// reloadSettings reads the settings the sign-in wrote a credential for. A caller with no
// settings file of its own — the welcome front door — reads the defaults discovery finds.
func (o *loginOptions) reloadSettings(settingsPath string) (*config.Settings, error) {
	if settingsPath == "" {
		return config.LoadDefaults()
	}
	return config.Load(settingsPath)
}

// refuseBrowserFlags refuses the flags a browser sign-in takes when the chosen sign-in is not one.
//
// --no-browser and --wait are the browser flow's: a key flow opens no browser, so there is
// nothing to suppress and nothing to wait for. A flag accepted and ignored tells the operator it
// did something, so it is refused where it is read.
func (o *loginOptions) refuseBrowserFlags(flow auth.Flow) error {
	if flow.Method != auth.MethodKey {
		return nil
	}
	if o.noBrowser {
		return fmt.Errorf("--no-browser does not apply to the %s sign-in: it takes a key, and no browser opens", flow.Name)
	}
	if o.waitSet {
		return fmt.Errorf("--wait does not apply to the %s sign-in: it takes a key, and it is not waiting for a browser to come back", flow.Name)
	}
	return nil
}

// choose returns the sign-in the request names, or the one an operator picks from the list
// it reads the answer on in. The scanner is the caller's own, so a flow that asks several
// questions feeds every answer through one scan of its input.
func (o *loginOptions) choose(in *bufio.Scanner, stderr io.Writer) (auth.Flow, bool) {
	if o.provider != "" {
		for _, flow := range logins() {
			if slices.Contains(providerNames(flow), o.provider) || flow.Name == o.provider {
				return flow, true
			}
		}
		fmt.Fprintf(stderr, "%s: %q is not a sign-in poo knows: %s\n", o.command, o.provider, strings.Join(loginNames(), ", "))
		return auth.Flow{}, false
	}
	fmt.Fprintln(stderr, "Sign in with:")
	for i, flow := range logins() {
		fmt.Fprintf(stderr, "  %d. %s\n", i+1, flow.Name)
	}
	fmt.Fprintf(stderr, "Which one? [1-%d] ", len(logins()))
	if !in.Scan() {
		fmt.Fprintf(stderr, "%s: nothing was chosen\n", o.command)
		return auth.Flow{}, false
	}
	var choice int
	if _, err := fmt.Sscanf(strings.TrimSpace(in.Text()), "%d", &choice); err != nil ||
		choice < 1 || choice > len(logins()) {
		fmt.Fprintf(stderr, "%s: that is not one of the sign-ins\n", o.command)
		return auth.Flow{}, false
	}
	return logins()[choice-1], true
}

// signIn runs the flow the operator chose.
func (o *loginOptions) signIn(ctx context.Context, flow auth.Flow, in *bufio.Scanner, stdout, stderr io.Writer) (auth.Credential, bool) {
	if flow.Method == auth.MethodKey {
		return keyCredential(flow, o.command, os.Stdin, in, stderr)
	}
	attempt, err := flow.Authorize()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", o.command, err)
		return auth.Credential{}, true
	}
	fmt.Fprintf(stderr, "Open this URL to sign in:\n\n  %s\n\n", attempt.URL)
	if !o.noBrowser {
		if err := openBrowser(attempt.URL); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", o.command, err)
		}
	}
	if interactive() {
		fmt.Fprintln(stderr, "Waiting for the sign-in to come back. If the browser cannot reach this machine,")
		fmt.Fprintln(stderr, "paste the URL it landed on: the state in its query is what the sign-in is")
		fmt.Fprintln(stderr, "checked against, and a code on its own carries none.")
	}
	callback, err := waitForCallback(ctx, attempt, o.waitDuration(), nil, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", o.command, err)
		return auth.Credential{}, true
	}
	credential, err := attempt.Exchange(ctx, nil, callback)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", o.command, err)
		return auth.Credential{}, true
	}
	return credential, false
}

// keyCredential reads the key a key flow signs in with.
//
// The key comes from the terminal's prompt, or from stdin, and from nowhere else. An argument
// would be visible in the process list and would stay in the shell's history, so a key is never
// written on the command line.
//
// The two reads are exclusive: one input is one of them. A terminal that ends its prompt with
// nothing typed has given no key, and asking the same terminal again would sit on a prompt that
// is not there.
func keyCredential(flow auth.Flow, command string, stdin *os.File, in *bufio.Scanner, stderr io.Writer) (auth.Credential, bool) {
	key := ""
	if interactive() {
		fmt.Fprintf(stderr, "%s key: ", flow.Name)
		if read, err := term.ReadPassword(stdin); err == nil {
			key = read
			fmt.Fprintln(stderr)
		}
	} else {
		scanner := in
		if scanner == nil {
			scanner = bufio.NewScanner(stdin)
		}
		if scanner.Scan() {
			key = strings.TrimSpace(scanner.Text())
		}
	}
	if key == "" {
		fmt.Fprintf(stderr, "%s: no key was given\n", command)
		return auth.Credential{}, true
	}
	return auth.Credential{Kind: auth.KindAPIKey, Key: key}, false
}

// waitDuration is how long a browser sign-in waits for its callback.
//
// A --wait that was typed is used as it was typed, zero included: 0 asks for no wait at all. The
// default is for a run that asked for nothing, so a typed value and the flag's absence do not
// read the same.
func (o *loginOptions) waitDuration() time.Duration {
	if o.waitSet {
		return time.Duration(o.wait) * time.Second
	}
	return 5 * time.Minute
}

// listenOn opens the listener a callback wait holds.
//
// The var is the test's seam: a test that already holds the listener open hands it back here, so
// nothing sits between the kernel handing out a port and the wait binding it.
var listenOn = net.Listen

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
	listener, err := listenOn("tcp", redirect.Host)
	if err != nil {
		return auth.Callback{}, listenerFailure(redirect.Host, err)
	}
	defer listener.Close()

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
	// Ready means an accept loop is serving the listener, not merely
	// that the socket is bound: the caller may connect the moment the
	// wait says so.
	if ready != nil {
		ready()
	}
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

// listenerFailure is what the wait says when it cannot bind its callback port.
//
// The advice to free the port is the held port's alone. A bind fails for other reasons too — a
// host that does not resolve, an address this machine does not have — and telling the operator
// to free a port that was never taken sends them after nothing. The wrapped error names what did
// happen either way.
func listenerFailure(host string, err error) error {
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Errorf(
			"the callback cannot listen on %s: %w: the port is already taken, so free it and run the sign-in again",
			host, err)
	}
	return fmt.Errorf("the callback cannot listen on %s: %w", host, err)
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
