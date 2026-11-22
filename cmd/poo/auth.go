package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo/provider/auth"
)

// authOptions are the flags of `poo auth status`.
type authOptions struct {
	sharedFlags
	// json prints the report as JSON.
	json bool
}

// bind reads the auth command's flags out of a parsed invocation.
func (o *authOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.json = inv.Bools["json"]
}

// authStatus is the report as JSON prints it: the store's status, in the names
// a caller of the command reads.
type authStatus struct {
	// File is the credential file the settings resolved.
	File string `json:"file"`
	// Format is the shape the file is written with: pi's or poo's.
	Format string `json:"format"`
	// Provider is the provider id the credential is stored under.
	Provider string `json:"provider"`
	// ClientID is the OAuth client the credential was granted to, when the file records one.
	ClientID string `json:"client_id,omitempty"`
	// AccountID is the ChatGPT account the token names.
	AccountID string `json:"account_id"`
	// Expires is when the access token stops working.
	Expires string `json:"expires"`
	// State is live, or refresh needed.
	State string `json:"state"`
}

// handleAuthStatus executes `poo auth status`.
//
// The command reports the credential the settings' file holds: the file, the
// shape it is written with, the provider, the account and whether the credential
// is live. It prints no secret, and it trades none: a report leaves the
// credential exactly as it found it.
//
// The credential is the one the file holds, whichever sign-in stored it: Codex's
// token pair, or a provider's key.
func handleAuthStatus(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o authOptions
	o.bind(inv)
	credentials, err := authCredentials(o.sharedFlags)
	if err != nil {
		fmt.Fprintf(stderr, "poo auth status: %v\n", err)
		return exit{usageExitCode}
	}
	status, err := credentials.Status(auth.Codex)
	if err != nil {
		// The file's entry may be a provider's key instead of Codex's token pair, which has no
		// tokens and no account. The question the fallback asks is whether an entry is there, not
		// whether it reads: an entry that is there and does not decode is the failure to report,
		// and looking past it would drop what the operator needs to know.
		if held, heldErr := credentials.Holds(auth.Codex.ID); heldErr == nil && !held {
			status, err = keyStatus(credentials, err)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "poo auth status: %v\n", err)
		return exit{operationalExitCode}
	}
	report := authStatus{
		File:      status.Path,
		Format:    status.Format,
		Provider:  status.Provider,
		ClientID:  status.ClientID,
		AccountID: status.AccountID,
		State:     credentialState(status),
	}
	// A credential with no expiry has no time to report: a key is sent as it is, and a time
	// rendered for it would be a time nothing enforces.
	if !status.Expires.IsZero() {
		report.Expires = status.Expires.UTC().Format(time.RFC3339)
	}
	if o.json {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "poo auth status: %v\n", err)
			return exit{operationalExitCode}
		}
		fmt.Fprintln(stdout, string(encoded))
		return exit{}
	}
	printAuthStatus(report, status, stdout)
	return exit{}
}

func handleAuthRefresh(ctx context.Context, inv clicontract.Invocation, stderr io.Writer) exit {
	leeway := inv.Durations["leeway"]
	if inv.Supplied["leeway"] && leeway <= 0 {
		fmt.Fprintln(stderr, "poo auth refresh: --leeway must be positive")
		return exit{usageExitCode}
	}
	var flags sharedFlags
	flags.bind(inv)
	credentials, err := authCredentials(flags)
	if err != nil {
		fmt.Fprintf(stderr, "poo auth refresh: %v\n", err)
		return exit{usageExitCode}
	}
	credentials.Leeway = leeway
	if inv.Bools["force"] {
		_, err = credentials.Refresh(ctx, auth.Codex)
	} else {
		_, err = credentials.Access(ctx, auth.Codex)
	}
	if err != nil {
		fmt.Fprintf(stderr, "poo auth refresh: %v\n", err)
		return exit{operationalExitCode}
	}
	return exit{}
}

func authCredentials(flags sharedFlags) (auth.Credentials, error) {
	settings, err := flags.settings("")
	if err != nil {
		return auth.Credentials{}, err
	}
	path := settings.CredentialFile()
	if path == "" {
		return auth.Credentials{}, fmt.Errorf("the credential file cannot be resolved: set auth_file")
	}
	return auth.Credentials{Path: path}, nil
}

// printAuthStatus prints the report as the fields an operator reads.
func printAuthStatus(report authStatus, status auth.Status, stdout io.Writer) {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "FILE\t%s\n", report.File)
	fmt.Fprintf(w, "FORMAT\t%s\n", report.Format)
	fmt.Fprintf(w, "PROVIDER\t%s\n", report.Provider)
	fmt.Fprintf(w, "CLIENT\t%s\n", report.ClientID)
	fmt.Fprintf(w, "ACCOUNT\t%s\n", report.AccountID)
	if status.Expires.IsZero() {
		fmt.Fprintf(w, "EXPIRES\t%s (%s)\n", "none", "it does not expire")
	} else {
		fmt.Fprintf(w, "EXPIRES\t%s (%s)\n", report.Expires, life(status.Expires))
	}
	fmt.Fprintf(w, "STATE\t%s\n", report.State)
	_ = w.Flush()
}

// keyStatus reports the key credential the file holds: the first key provider whose
// entry reads.
//
// A held entry that does not read is the failure to report: the operator has a
// credential in the file, and what is wrong with it is the one thing the report can
// act on. No held entry at all leaves the earlier failure in place, which names the
// provider the operator was trying to use.
func keyStatus(credentials auth.Credentials, earlier error) (auth.Status, error) {
	for _, provider := range auth.KeyProviders() {
		status, err := credentials.Status(provider)
		if err == nil {
			return status, nil
		}
		if held, holdErr := credentials.Holds(provider.ID); holdErr == nil && held {
			return auth.Status{}, err
		}
	}
	return auth.Status{}, earlier
}

// credentialState is the word a report gives a credential's state: a credential poo
// would use as it is, or one it trades before using it.
func credentialState(status auth.Status) string {
	// A key is sent as it is: nothing trades it, so there is nothing to refresh and nothing
	// that can be stale.
	if status.Kind == auth.KindAPIKey || status.Live {
		return "live"
	}
	return "refresh needed"
}

// life renders when a token stops working as time from now, for a reader that
// is asking how long it holds.
func life(expires time.Time) string {
	left := time.Until(expires)
	if left < 0 {
		return "expired " + (-left).Round(time.Minute).String() + " ago"
	}
	return "in " + left.Round(time.Minute).String()
}
