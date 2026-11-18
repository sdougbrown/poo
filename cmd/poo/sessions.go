package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/session"
)

// sessionsOptions are the flags of the sessions commands.
type sessionsOptions struct {
	sharedFlags
	// json prints the sessions as JSON.
	json bool
	// olderThan is how old a session's last activity must be.
	olderThan int
	// keep is how many sessions the prune keeps.
	keep int
	// dryRun lists the sessions the prune would delete.
	dryRun bool
}

// bind reads the sessions flags out of a parsed invocation.
func (o *sessionsOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.json = inv.Bools["json"]
	o.olderThan = inv.Ints["older-than"]
	o.keep = inv.Ints["keep"]
	o.dryRun = inv.Bools["dry-run"]
}

// handleSessionsList executes `poo sessions list`.
func handleSessionsList(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o sessionsOptions
	o.bind(inv)
	store, err := o.openStore()
	if err != nil {
		fmt.Fprintf(stderr, "poo sessions list: %v\n", err)
		return exit{usageExitCode}
	}
	defer store.Close()

	summaries, err := store.List(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "poo sessions list: %v\n", err)
		return exit{operationalExitCode}
	}
	if o.json {
		encoded, err := json.MarshalIndent(summaries, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "poo sessions list: %v\n", err)
			return exit{operationalExitCode}
		}
		fmt.Fprintln(stdout, string(encoded))
		return exit{}
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCREATED\tPROFILE\tRUNS\tREASON\tSIZE\tLOCKED")
	for _, summary := range summaries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%d\t%t\n",
			summary.ID,
			summary.CreatedAt.Format(time.RFC3339),
			summary.Profile,
			summary.Runs,
			summary.LastReason,
			summary.Size,
			summary.Locked)
	}
	_ = w.Flush()
	return exit{}
}

// handleSessionsPrune executes `poo sessions prune`.
//
// The policy is the settings' retention, and the flags change it. A
// prune with no policy at all deletes nothing and says so: the exit
// code is the store's no-policy code.
func handleSessionsPrune(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o sessionsOptions
	o.bind(inv)
	if o.olderThan < 0 {
		fmt.Fprintf(stderr, "poo sessions prune: --older-than must not be negative\n")
		return exit{usageExitCode}
	}
	if o.keep < 0 {
		fmt.Fprintf(stderr, "poo sessions prune: --keep must not be negative\n")
		return exit{usageExitCode}
	}
	settings, err := o.settings("")
	if err != nil {
		fmt.Fprintf(stderr, "poo sessions prune: %v\n", err)
		return exit{usageExitCode}
	}
	store, err := o.openStore()
	if err != nil {
		fmt.Fprintf(stderr, "poo sessions prune: %v\n", err)
		return exit{usageExitCode}
	}
	defer store.Close()

	policy := settings.RetentionPolicy()
	if o.olderThan > 0 {
		policy.MaxAge = time.Duration(o.olderThan) * 24 * time.Hour
	}
	if o.keep > 0 {
		policy.Keep = o.keep
	}
	policy.DryRun = o.dryRun

	result, err := store.Prune(context.Background(), policy)
	if err != nil {
		if errors.Is(err, session.ErrNoPolicy) {
			fmt.Fprintf(stderr, "poo sessions prune: there is no retention policy: the settings name none and the flags give none\n")
			return exit{usageExitCode}
		}
		fmt.Fprintf(stderr, "poo sessions prune: %v\n", err)
		return exit{operationalExitCode}
	}
	verb := "deleted"
	if policy.DryRun {
		verb = "would delete"
	}
	for _, summary := range result.Deleted {
		fmt.Fprintf(stdout, "%s %s\n", verb, summary.ID)
	}
	for _, summary := range result.Skipped {
		fmt.Fprintf(stderr, "poo sessions prune: skipped %s: another process holds its lock\n", summary.ID)
	}
	return exit{}
}

// openStore opens the store the sessions live in.
func (o *sessionsOptions) openStore() (*session.FileStore, error) {
	return o.sharedFlags.openStore("")
}

// openStore opens the store the settings and the flags name.
//
// The directory is the one a server would open: session.dir, then the
// --session-dir flag, with the home directory expanded. The working
// directory is where the settings are discovered from, so a command that
// runs in another one reads the settings that one names.
func (f *sharedFlags) openStore(workingDir string) (*session.FileStore, error) {
	settings, err := f.settings(workingDir)
	if err != nil {
		return nil, err
	}
	overrides, err := f.overrides()
	if err != nil {
		return nil, err
	}
	dir, err := config.SessionDirectory(settings, overrides)
	if err != nil {
		return nil, err
	}
	return session.NewStore(dir)
}
