package main

import (
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
// shape it is written with, the provider, the account and whether the token is
// live. It prints no token, and it trades none: a report leaves the credential
// exactly as it found it.
func handleAuthStatus(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o authOptions
	o.bind(inv)
	settings, err := o.settings("")
	if err != nil {
		fmt.Fprintf(stderr, "poo auth status: %v\n", err)
		return exit{usageExitCode}
	}
	path := settings.CredentialFile()
	if path == "" {
		fmt.Fprintln(stderr, "poo auth status: the credential file cannot be resolved: set auth_file")
		return exit{usageExitCode}
	}
	status, err := auth.Credentials{Path: path}.Status(auth.Codex)
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
		Expires:   status.Expires.UTC().Format(time.RFC3339),
		State:     credentialState(status),
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

// printAuthStatus prints the report as the fields an operator reads.
func printAuthStatus(report authStatus, status auth.Status, stdout io.Writer) {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "FILE\t%s\n", report.File)
	fmt.Fprintf(w, "FORMAT\t%s\n", report.Format)
	fmt.Fprintf(w, "PROVIDER\t%s\n", report.Provider)
	fmt.Fprintf(w, "CLIENT\t%s\n", report.ClientID)
	fmt.Fprintf(w, "ACCOUNT\t%s\n", report.AccountID)
	fmt.Fprintf(w, "EXPIRES\t%s (%s)\n", report.Expires, life(status.Expires))
	fmt.Fprintf(w, "STATE\t%s\n", report.State)
	_ = w.Flush()
}

// credentialState is the word a report gives a credential's state: a token poo
// would use as it is, or one it trades before using it.
func credentialState(status auth.Status) string {
	if status.Live {
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
