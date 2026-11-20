package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/provider/auth"
	"github.com/sdougbrown/poo/provider/catalog"
)

// The welcome front door is what a bare `poo` runs when discovery finds
// no settings file. It is an operator's first run, with nothing configured
// to open a session on. It asks for what a session needs: a credential, a
// chat model, a starting profile. It writes the settings file that records
// the answers. The front door that follows opens on what was just set up.

// runWelcome walks an operator through their first run. It returns the
// settings file it wrote. The boolean says whether the front door carries
// on to the interface it names. Every answer is read from in, so the whole
// flow works with one scan of the input.
func runWelcome(stdout, stderr io.Writer) (string, bool) {
	ctx := context.Background()
	in := bufio.NewScanner(os.Stdin)

	fmt.Fprintf(stderr, "Welcome to poo.\n\nA session needs a sign-in and a chat model. The answers go into a settings file you can edit later.\n\n")

	// A sign-in whose provider serves no chat turns is a dead end the
	// credential file alone cannot leave. The operator signs in again, with
	// a provider whose models can run chat.
	again := false
	for {
		if !welcomeSignIn(ctx, in, stdout, stderr, again) {
			return "", false
		}
		model, ok, stalled := welcomeModel(stderr, in)
		if ok {
			return welcomeFinish(stderr, in, model)
		}
		if !stalled {
			return "", false
		}
		fmt.Fprint(stderr, "Sign in with another provider? [y/N] ")
		if !askYes(in) {
			fmt.Fprintln(stderr, "poo: nothing was set up. Run `poo` again, or name the chat model in a settings file.")
			return "", false
		}
		again = true
	}
}

// welcomeFinish asks for the starting profile and writes the settings
// file the answers go into.
func welcomeFinish(stderr io.Writer, in *bufio.Scanner, model string) (string, bool) {
	profileName, ok := welcomeProfile(stderr, in)
	if !ok {
		return "", false
	}
	path, err := writeWelcomeSettings(model, profileName)
	if err != nil {
		fmt.Fprintf(stderr, "poo: writing the settings: %v\n", err)
		return "", false
	}
	fmt.Fprintf(stderr, "\nThe settings are in %s. Edit them there: the model, the profile, the tools, and more.\n", path)
	return path, true
}

// welcomeSignIn makes sure a credential is in poo's own file. The one
// that is there counts. Otherwise the operator picks a sign-in from the
// list the login command offers, and the flow runs it. Again is a
// re-sign-in: a credential is already in the file. The flow is here
// because it serves no chat model. The operator's pick replaces it.
func welcomeSignIn(ctx context.Context, in *bufio.Scanner, stdout, stderr io.Writer, again bool) bool {
	credPath, err := pooCredentialFile("", &config.Settings{Version: 1})
	if err != nil {
		fmt.Fprintf(stderr, "poo: %v\n", err)
		return false
	}
	if !again {
		if _, err := os.Stat(credPath); err == nil {
			fmt.Fprintf(stderr, "A credential is already in %s.\n", credPath)
			return true
		}
	}
	fmt.Fprintln(stderr, "Sign in with:")
	for i, flow := range logins() {
		fmt.Fprintf(stderr, "  %d. %s\n", i+1, flow.Name)
	}
	fmt.Fprintf(stderr, "Which one? [1-%d] ", len(logins()))
	choice := askChoice(in, stderr, len(logins()), 0)
	if choice == 0 {
		fmt.Fprintf(stderr, "poo: nothing was chosen. Run `poo` again to set up, or `poo login` to sign in on its own.\n")
		return false
	}
	o := loginOptions{force: again}
	_, failed := o.signInAndStore(ctx, logins()[choice-1], "", &config.Settings{Version: 1}, in, stdout, stderr)
	return !failed
}

// welcomeModel asks which model the chat role runs on. The catalog comes from the
// settings the flow is about to write. They are the defaults, with the credential the
// step before put in place. Stalled reports a catalog that holds no chat model the
// credential can run. A re-sign-in can leave that state. A refused answer is not
// stalled, and nothing re-runs it.
func welcomeModel(stderr io.Writer, in *bufio.Scanner) (model string, ok bool, stalled bool) {
	settings, err := config.LoadDefaults()
	if err != nil {
		fmt.Fprintf(stderr, "poo: %v\n", err)
		return "", false, false
	}
	options := settings.CatalogOptions()
	options.UserAgent = userAgent()
	credentials := auth.Credentials{Path: settings.CredentialFile()}
	models := []string{}
	cat := catalog.New(options)
	for _, info := range cat.List() {
		if !info.HasOp(poo.OpChat) {
			continue
		}
		if !credentialServes(cat, info.Provider, credentials) {
			continue
		}
		models = append(models, info.Name)
	}
	if len(models) == 0 {
		fmt.Fprintln(stderr, "poo: no model in the catalog serves chat. The credential signs in to a provider whose models are not chat models, and no key in the environment wakes one.")
		return "", false, true
	}
	fmt.Fprintln(stderr, "The models that can run chat:")
	for i, name := range models {
		fmt.Fprintf(stderr, "  %d. %s\n", i+1, name)
	}
	fmt.Fprintf(stderr, "Chat model? [1-%d] ", len(models))
	choice := askChoice(in, stderr, len(models), 1)
	if choice == 0 {
		fmt.Fprintln(stderr, "poo: no model was chosen. Run `poo` again to set up, or name the chat model in a settings file.")
		return "", false, false
	}
	return models[choice-1], true, false
}

// credentialServes reports whether the credential a model's endpoint reads
// is in reach. A static endpoint needs none. An env-key endpoint reads the
// variable it names, or the file entry for the key provider that reads it.
// The Codex endpoint reads the credential of its own that the file holds.
// The catalog offers a model whatever the credential's state is. Without
// this check the picker would hand an operator models they cannot run.
func credentialServes(cat *catalog.Catalog, provider string, credentials auth.Credentials) bool {
	if provider == auth.ProviderCodex {
		held, err := credentials.Holds(provider)
		return err == nil && held
	}
	endpoint, ok := cat.Endpoints()[provider]
	if !ok || endpoint.Credential.Source != catalog.CredentialEnv {
		return true
	}
	env := endpoint.Credential.Env
	if os.Getenv(env) != "" {
		return true
	}
	for _, p := range auth.KeyProviders() {
		if p.Env != env {
			continue
		}
		held, err := credentials.Holds(p.ID)
		return err == nil && held
	}
	// An environment variable no key provider reads — a hosted provider's,
	// mostly — has no entry in poo's credential file to fall back on.
	return false
}

// welcomeProfile asks which of the profiles the binary links a session
// starts under. The default is the one sessions run under when nothing
// names a profile.
func welcomeProfile(stderr io.Writer, in *bufio.Scanner) (string, bool) {
	names := profile.Names(linkedProfiles())
	def := slices.Index(names, defaultProfile) + 1
	fmt.Fprintln(stderr, "The profiles a session runs under:")
	for i, name := range names {
		fmt.Fprintf(stderr, "  %d. %s\n", i+1, name)
	}
	fmt.Fprintf(stderr, "Starting profile? [1-%d, empty for %s] ", len(names), defaultProfile)
	choice := askChoice(in, stderr, len(names), def)
	if choice == 0 {
		fmt.Fprintln(stderr, "poo: no profile was chosen. Run `poo` again to set up, or name the profile in a settings file.")
		return "", false
	}
	return names[choice-1], true
}

// askChoice reads one answer line. A number from 1 to max picks one. An
// empty line picks the default, which 0 says was never offered. An input
// that ends first, or a line that is neither, is no answer.
func askChoice(in *bufio.Scanner, stderr io.Writer, max, def int) int {
	if !in.Scan() {
		return 0
	}
	line := strings.TrimSpace(in.Text())
	if line == "" {
		return def
	}
	var choice int
	if _, err := fmt.Sscanf(line, "%d", &choice); err != nil || choice < 1 || choice > max {
		fmt.Fprintln(stderr, "That is not one of the choices.")
		return 0
	}
	return choice
}

// askYes reads one answer line, and reads anything but yes as no.
func askYes(in *bufio.Scanner) bool {
	if !in.Scan() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(in.Text())) {
	case "y", "yes":
		return true
	}
	return false
}

// writeWelcomeSettings writes the settings file the answers go into: the
// user config directory's own, the one discovery finds on the next run.
// The document is the smallest one that says what was answered, so the
// file an operator opens to change things reads as it was written.
//
// The file is created, not overwritten. A settings file already there is
// one discovery will find on the next run. A flow that clobbered it would
// trade the operator's settings for its own answers.
func writeWelcomeSettings(model, profileName string) (string, error) {
	dir, err := config.UserConfigDir()
	if err != nil {
		return "", err
	}
	// The credential store makes this directory its own when it writes,
	// and the settings sit beside the credential.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	document := fmt.Sprintf(`{
  "version": 1,
  "profile": %q,
  "roles": {
    "chat": %q
  }
}
`, profileName, model)
	path := filepath.Join(dir, config.SettingsFileName)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%s is already there: remove it first, or run `poo` with it as the settings", path)
		}
		return "", err
	}
	defer file.Close()
	if _, err := file.WriteString(document); err != nil {
		return "", err
	}
	return path, nil
}
