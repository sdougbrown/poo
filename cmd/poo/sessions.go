package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/session"
)

// runSessions executes `poo sessions`.
//
// The command lists the sessions the store holds and prunes the ones a
// retention policy marks.
func runSessions(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "list":
		return runSessionsList(args[1:])
	case "prune":
		return runSessionsPrune(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "poo sessions: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

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

// register adds the sessions flags to a flag set.
func (o *sessionsOptions) register(fs *flag.FlagSet) {
	o.sharedFlags.register(fs)
	fs.BoolVar(&o.json, "json", false, "Print the sessions as JSON")
	fs.IntVar(&o.olderThan, "older-than", 0, "Delete the sessions whose last activity is older than this many days")
	fs.IntVar(&o.keep, "keep", 0, "Keep this many sessions, by last activity")
	fs.BoolVar(&o.dryRun, "dry-run", false, "List the sessions the prune would delete and delete nothing")
}

// runSessionsList executes `poo sessions list`.
func runSessionsList(args []string) int {
	fs := flag.NewFlagSet("poo sessions list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var o sessionsOptions
	o.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	store, err := o.openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo sessions list: %v\n", err)
		return 2
	}
	defer store.Close()

	summaries, err := store.List(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo sessions list: %v\n", err)
		return 1
	}
	if o.json {
		encoded, err := json.MarshalIndent(summaries, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo sessions list: %v\n", err)
			return 1
		}
		fmt.Println(string(encoded))
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
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
	return 0
}

// runSessionsPrune executes `poo sessions prune`.
//
// The policy is the settings' retention, and the flags change it. A
// prune with no policy at all deletes nothing and says so: the exit
// code is the store's no-policy code.
func runSessionsPrune(args []string) int {
	fs := flag.NewFlagSet("poo sessions prune", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	var o sessionsOptions
	o.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if o.olderThan < 0 {
		fmt.Fprintf(os.Stderr, "poo sessions prune: --older-than must not be negative\n")
		return 2
	}
	if o.keep < 0 {
		fmt.Fprintf(os.Stderr, "poo sessions prune: --keep must not be negative\n")
		return 2
	}
	settings, err := o.settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo sessions prune: %v\n", err)
		return 2
	}
	store, err := o.openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo sessions prune: %v\n", err)
		return 2
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
			fmt.Fprintf(os.Stderr, "poo sessions prune: there is no retention policy: the settings name none and the flags give none\n")
			return 2
		}
		fmt.Fprintf(os.Stderr, "poo sessions prune: %v\n", err)
		return 1
	}
	verb := "deleted"
	if policy.DryRun {
		verb = "would delete"
	}
	for _, summary := range result.Deleted {
		fmt.Printf("%s %s\n", verb, summary.ID)
	}
	for _, summary := range result.Skipped {
		fmt.Fprintf(os.Stderr, "poo sessions prune: skipped %s: another process holds its lock\n", summary.ID)
	}
	return 0
}

// openStore opens the store the sessions live in.
func (o *sessionsOptions) openStore() (*session.FileStore, error) {
	settings, err := o.settings()
	if err != nil {
		return nil, err
	}
	overrides, err := o.overrides()
	if err != nil {
		return nil, err
	}
	dir, err := config.SessionDirectory(settings, overrides)
	if err != nil {
		return nil, err
	}
	return session.NewStore(dir)
}
