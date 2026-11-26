package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/session"
)

// daemonOptions are the flags of `poo daemon`.
type daemonOptions struct {
	sharedFlags
	endpointFlags
	// session is the session id to load, or create if it does not
	// exist.
	session string
	// autoResume resumes a pending run on load.
	autoResume bool
	// autoResumeGiven reports that the flag was on the command line,
	// so an absent flag takes the settings' value, or the default.
	autoResumeGiven bool
	// workingDir is the directory the tools work in.
	workingDir string
}

// daemonFlagSpecs declares the flags of `poo daemon`: the endpoint, then
// the daemon's own, in the order the help prints them. The shared flags
// are the app's common flags, so they are not declared here.
func daemonFlagSpecs() []clicontract.Flag {
	specs := endpointFlagSpecs()
	return append(specs,
		clicontract.Flag{Name: "session", Kind: clicontract.String, Usage: "The session id to load, or create if it does not exist"},
		clicontract.Flag{Name: "auto-resume", Kind: clicontract.Bool, Usage: "Resume a pending run on load (default: true)"},
		clicontract.Flag{Name: "working-dir", Kind: clicontract.String, Usage: "The directory the tools work in (default: the current one)"},
	)
}

// bind reads the daemon's flags out of a parsed invocation.
func (o *daemonOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.endpointFlags.bind(inv)
	o.session = inv.Flags["session"]
	o.autoResume = inv.Bools["auto-resume"]
	o.autoResumeGiven = inv.Supplied["auto-resume"]
	o.workingDir = inv.Flags["working-dir"]
}

// validate checks the flags the daemon's front door shares with the
// server: a bad policy value or half a model is refused before the
// session starts.
func (o *daemonOptions) validate(name string, stderr io.Writer) bool {
	if err := o.checkUnmatched(); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return false
	}
	if err := o.endpointFlags.validate(); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return false
	}
	return true
}

// handleDaemon executes `poo daemon`.
//
// The daemon hosts a long-lived feed-driven session: it starts the
// session's feeds, and waits for a signal or a lost lease without
// reading stdin. A signal settles the session through the normal path
// and releases the lease; a lost lease is another owner's session, so
// the daemon ends rather than write into it.
func handleDaemon(parent context.Context, inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o daemonOptions
	o.bind(inv)
	if !o.validate("poo daemon", stderr) {
		return exit{usageExitCode}
	}

	workDir, err := workingDirectory(o.workingDir)
	if err != nil {
		fmt.Fprintf(stderr, "poo daemon: %v\n", err)
		return exit{usageExitCode}
	}
	settings, err := o.settings(workDir)
	if err != nil {
		fmt.Fprintf(stderr, "poo daemon: %v\n", err)
		return exit{usageExitCode}
	}
	o.endpointFlags.apply(settings)

	// The mode is resolved once: the flag over the settings over the
	// default, and the one value goes to the build and the store.
	mode := "durable"
	if settings.Session.Mode != nil && *settings.Session.Mode != "" {
		mode = *settings.Session.Mode
	}
	if o.ephemeral {
		mode = "ephemeral"
	}
	autoResume := true
	if settings.Session.Durable.AutoResume != nil {
		autoResume = *settings.Session.Durable.AutoResume
	}
	if o.autoResumeGiven {
		autoResume = o.autoResume
	}
	settings.Session.Durable.AutoResume = &autoResume

	overrides, err := o.overrides()
	if err != nil {
		fmt.Fprintf(stderr, "poo daemon: %v\n", err)
		return exit{usageExitCode}
	}
	overrides.SessionMode = mode

	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		Overrides:      config.EnvOverrides(overrides),
		WorkingDir:     workDir,
		DefaultProfile: defaultProfile,
		UserPromptFile: defaultUserPromptFile(),
		Tools:          linkedTools(),
		Profiles:       linkedProfiles(),
		Executors:      executorRegistry(),
		// The command has no wire client, so the chain is the decide
		// asker alone, and it is there only when a decider model is
		// configured.
		Askers: []string{"decide"},
		Build:  poo.BuildInfo{Name: "poo", Version: buildVersion().String()},
	})
	if err != nil {
		fmt.Fprintf(stderr, "poo daemon: %v\n", err)
		return exit{usageExitCode}
	}
	if runtime.Warning != "" {
		fmt.Fprintf(stderr, "poo daemon: warning: %s\n", runtime.Warning)
	}

	// The model the daemon talks to, and the command's own sections.
	model := runtime.Options.Model
	runtime.Options.Prompt = append(runtime.Options.Prompt, promptMutation(runtime, model))

	// The store holds the session's records and its lease.
	store, err := runtime.OpenStore(mode, "")
	if err != nil {
		fmt.Fprintf(stderr, "poo daemon: %v\n", err)
		return exit{operationalExitCode}
	}

	sessionID := o.session
	if sessionID == "" {
		sessionID = session.NewID()
	}

	ctx, stop := context.WithCancel(parent)
	defer stop()

	var sess *poo.Session
	var hadPending bool
	var pendingRunID string
	if _, err := store.Metadata(ctx, sessionID); err == nil {
		// The session is there: load it, and take over its lease when
		// the owner is dead.
		resumed, err := runtime.ResumeSession(ctx, store, sessionID)
		if err != nil {
			_ = store.Close()
			fmt.Fprintf(stderr, "poo daemon: %v\n", err)
			return exit{operationalExitCode}
		}
		sess = resumed.Session
		if resumed.Restored.PendingRun != nil {
			hadPending = true
			pendingRunID = resumed.Restored.PendingRun.RunID
		}
	} else if !errors.Is(err, session.ErrSessionNotFound) {
		_ = store.Close()
		fmt.Fprintf(stderr, "poo daemon: %v\n", err)
		// A refused argument is the usage failure, not an operational
		// one: the id names something outside the session directory.
		if errors.Is(err, session.ErrInvalidSessionID) {
			return exit{usageExitCode}
		}
		return exit{operationalExitCode}
	} else {
		created := runtime.CreatedRecord(mode, workDir, sessionID)
		sess, err = runtime.CreateSession(ctx, store, created)
		if err != nil {
			_ = store.Close()
			fmt.Fprintf(stderr, "poo daemon: %v\n", err)
			if errors.Is(err, session.ErrInvalidSessionID) {
				return exit{usageExitCode}
			}
			return exit{operationalExitCode}
		}
	}

	// The lifecycle, one fact per line.
	fmt.Fprintf(stderr, "poo daemon: started\n")
	fmt.Fprintf(stderr, "poo daemon: session %s\n", sessionID)
	if feeds := runningFeeds(runtime.Options.Feeds); feeds > 0 {
		fmt.Fprintf(stderr, "poo daemon: %d feed(s)\n", feeds)
	}
	if hadPending && autoResume {
		fmt.Fprintf(stderr, "poo daemon: the session was taken over: a run was left unsettled\n")
	}

	// A pending run the restore did not carry on is the daemon's to
	// resume: the settings' auto-resume is the restore's, and the flag
	// is the daemon's.
	if autoResume {
		if pending := sess.PendingRun(); pending != nil {
			// The restore did not carry it on: the daemon does.
			if _, err := sess.ResumeRun(ctx); err != nil {
				// A resume that failed is not health: the daemon was
				// started for the run, and it will never run it.
				fmt.Fprintf(stderr, "poo daemon: resuming the pending run: %v\n", err)
				_ = sess.Close()
				_ = store.Close()
				return exit{operationalExitCode}
			}
			fmt.Fprintf(stderr, "poo daemon: resumed %s\n", pending.RunID)
		} else if hadPending {
			// The restore carried it on: the settings' auto-resume did.
			// A resume the restore could not finish leaves the session
			// broken, and a supervisor must not read a broken takeover
			// as a healthy resident.
			if sess.Broken() {
				fmt.Fprintf(stderr, "poo daemon: resuming the pending run: the session is broken\n")
				_ = sess.Close()
				_ = store.Close()
				return exit{operationalExitCode}
			}
			fmt.Fprintf(stderr, "poo daemon: resumed %s\n", pendingRunID)
		} else {
			fmt.Fprintf(stderr, "poo daemon: idle\n")
		}
	} else if hadPending {
		fmt.Fprintf(stderr, "poo daemon: run %s was left unsettled; auto-resume is off\n", pendingRunID)
	} else {
		fmt.Fprintf(stderr, "poo daemon: idle\n")
	}

	// The lease the store holds, when its ownership is a lease. A store
	// that holds its authority for the process's life offers no channel,
	// and the wait is the signal alone.
	var leaseLost <-chan struct{}
	if watcher, ok := store.(poo.LeaseLostWatch); ok {
		leaseLost = watcher.LeaseLost()
	}

	// The first signal settles the session through the normal path and
	// releases the lease.
	interrupts := make(chan os.Signal, 2)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	go func() {
		<-interrupts
		stop()
	}()

	select {
	case <-ctx.Done():
		// The signal: stop the feeds, flush the records, and release
		// the lease.
		_ = sess.Close()
		_ = store.Close()
		fmt.Fprintf(stderr, "poo daemon: shutdown\n")
		return exit{}
	case <-leaseLost:
		// The lease is another owner's now: end rather than write into
		// a session that is no longer this one's.
		fmt.Fprintf(stderr, "poo daemon: the lease was lost: a takeover owner now holds the session\n")
		return exit{operationalExitCode}
	}
}

// runningFeeds counts the feeds a session starts: the ones that are not
// optional.
func runningFeeds(feeds []poo.FeedSpec) int {
	n := 0
	for _, f := range feeds {
		if !f.Optional {
			n++
		}
	}
	return n
}
