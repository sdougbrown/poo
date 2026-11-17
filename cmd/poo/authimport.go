package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/provider/auth"
)

// authImportOptions are the flags of `poo auth import`.
type authImportOptions struct {
	sharedFlags
	// force replaces a credential poo's file already holds, without asking.
	force bool
	// from names the file the credential is copied out of.
	from string
}

// bind reads the import command's flags out of a parsed invocation.
func (o *authImportOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.force = inv.Bools["force"]
	o.from = inv.Flags["from"]
}

// handleAuthImportPi executes `poo auth import pi`.
//
// The command copies the Codex credential pi's auth.json holds into poo's own
// file, once. It is a one-way door, and it says so: a refresh rotates the refresh
// token, so from poo's first refresh pi's copy is stale, and pi's own refresh would
// invalidate poo's. One refresher per credential is the rule; this is how an
// operator picks which.
func handleAuthImportPi(ctx context.Context, inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o authImportOptions
	o.bind(inv)
	settingsPath, settings, err := resolveSettings(o.settingsFile, "")
	if err != nil {
		fmt.Fprintf(stderr, "poo auth import: %v\n", err)
		return exit{usageExitCode}
	}
	target, err := pooCredentialFile(settingsPath, settings)
	if err != nil {
		fmt.Fprintf(stderr, "poo auth import: %v\n", err)
		return exit{usageExitCode}
	}
	source, err := piCredentialFile(settingsPath, settings, o.from)
	if err != nil {
		fmt.Fprintf(stderr, "poo auth import: %v\n", err)
		return exit{usageExitCode}
	}
	if source == "" {
		fmt.Fprintln(stderr, "poo auth import: pi's auth.json was not found: name it with --from or pi_auth_file")
		return exit{usageExitCode}
	}
	if sameFile(source, target) {
		fmt.Fprintf(stderr, "poo auth import: %s is the file poo reads: there is nothing to copy\n", source)
		return exit{usageExitCode}
	}
	entry, err := readPiCredential(source)
	if err != nil {
		fmt.Fprintf(stderr, "poo auth import: %v\n", err)
		return exit{operationalExitCode}
	}
	poo := auth.Credentials{Path: target}
	mayReplace, failed := replace(poo, auth.Codex, o.force, "poo auth import", stderr)
	if failed {
		return exit{operationalExitCode}
	}
	// The copy records the client pi's grant belongs to, so the credential says where it came
	// from instead of relying on a default: from here poo refreshes pi's grant as pi does. A
	// source that records a client of its own keeps it — the grant is what decides.
	if entry.ClientID == "" {
		entry.ClientID = auth.DefaultClientID
	}
	if entry.TokenURL == "" {
		entry.TokenURL = auth.DefaultTokenURL
	}
	status, err := poo.Set(ctx, auth.Codex, entry, mayReplace)
	if err != nil {
		fmt.Fprintf(stderr, "poo auth import: %v\n", err)
		return exit{operationalExitCode}
	}
	fmt.Fprintf(stdout, "Copied the %s credential from %s to %s.\n", status.Provider, source, status.Path)
	fmt.Fprintf(stdout, "It is signed in as %s and expires %s.\n", status.AccountID, status.Expires.UTC().Format("2006-01-02 15:04 MST"))
	// The copy is only worth making if poo reads it. A settings file that names no
	// auth_file still resolves to whatever credential file was there first, and that
	// is the file the copy was taken from.
	if reads := settings.CredentialFile(); reads != "" && !sameFile(reads, status.Path) {
		fmt.Fprintf(stdout, "\nThe settings still resolve the credential to %s. Point poo at the copy:\n  auth_file: %s\n", reads, status.Path)
	}
	fmt.Fprint(stdout, `
From here poo owns the refresh. A refresh rotates the refresh token, so:
  - pi's copy is stale from poo's first refresh, and
  - if pi refreshes first, poo's credential stops working.
Run one of them, not both. `+"`poo auth status`"+` says which file poo reads.
`)
	return exit{}
}

// pooCredentialFile is the file poo writes: the one auth_file names, and otherwise
// poo's own in the user's config directory.
//
// It is answered apart from the file the settings resolve the credential to: that
// resolution is what a session reads, and after an import it may be either tool's
// file. The import writes poo's.
func pooCredentialFile(settingsPath string, settings *config.Settings) (string, error) {
	if settings.AuthFile != "" {
		return config.ResolveSettingPath(settingsPath, settings.AuthFile)
	}
	return config.DefaultAuthFile()
}

// piCredentialFile is the file pi's credential is read from: the one --from names,
// then the one pi_auth_file names, and otherwise pi's own default location.
func piCredentialFile(settingsPath string, settings *config.Settings, from string) (string, error) {
	named := from
	if named == "" {
		named = settings.PiAuthFile
	}
	if named != "" {
		path, err := config.ResolveSettingPath(settingsPath, named)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("%s is not there: %w", path, err)
		}
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("the home directory cannot be resolved: %w", err)
	}
	path := filepath.Join(home, ".pi", "agent", "auth.json")
	if _, err := os.Stat(path); err != nil {
		return "", nil
	}
	return path, nil
}

// readPiCredential reads the Codex credential out of pi's auth.json.
//
// The package that keeps a credential reads it: the entry's fields and its oauth rule are
// one thing, and a second decoder here would be a copy that a field added to the format
// would not reach.
func readPiCredential(path string) (auth.Credential, error) {
	return auth.Credentials{Path: path}.Read(auth.Codex)
}

// sameFile reports whether two paths name one file.
func sameFile(first, second string) bool {
	if first == second {
		return true
	}
	one, err := os.Stat(first)
	if err != nil {
		return false
	}
	two, err := os.Stat(second)
	if err != nil {
		return false
	}
	return os.SameFile(one, two)
}
