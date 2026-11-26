package session

import (
	"context"

	"github.com/sdougbrown/poo"
)

// Resumed is a session that was loaded and restored.
type Resumed struct {
	// Session is the restored session. It writes to the store it was
	// loaded from, so the session carries on in the file it was
	// recorded in.
	Session *poo.Session
	// Loaded is what the load found: the records, the metadata and the
	// warnings.
	Loaded *Loaded
	// Restored is what the restore made of the records: the runs, the
	// messages and the job results that were queued again.
	Restored poo.Restored
}

// Resume loads a session from a store and restores it.
//
// The load takes the session's authority, recovers the session and
// reads its records: a torn tail is truncated, an unfinished job is
// ended, and an input that never reached the model is dropped. A logged
// session's unsettled run is marked interrupted; a durable session's is
// planned for a continuation. The restore rebuilds a session from those
// records, and the job results that were never delivered are queued
// again.
//
// A durable session's continuation is threaded into the restore, which
// stores it on the session: the session holds it, idle, until ResumeRun
// carries it out. poo.WithAutoResume makes the restore resume it before
// returning.
//
// The restored session writes to the store it was loaded from. The
// store holds the session's authority until it is closed, so a caller
// closes the store when the session is done with it.
//
// The restore needs the session's own settings: the tools it links, the
// models it offers and the profile it knows. The records hold the
// session's own metadata, and the restore checks it against those
// settings.
//
// The options are the ones the session is built with: the hooks and the
// observers a new session would carry.
func Resume(ctx context.Context, store Store, id string, opts poo.Options, fns ...poo.Option) (*Resumed, error) {
	loaded, err := store.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	opts.Sink = store
	opts.SessionID = loaded.ID
	// The plan is what the load left to carry on. It is the restore's,
	// because the core is what drives a run: the store plans, and the
	// session resumes. The slice is the caller's, so the option is added
	// to a copy.
	fns = append(append([]poo.Option(nil), fns...), poo.WithContinuation(loaded.Continuation))
	sess, restored, err := poo.Restore(opts, loaded.Records, fns...)
	if err != nil {
		return nil, err
	}
	// The store and the restore both report what changed. A caller prints
	// one list, not two.
	restored.Warnings = uniqueWarnings(append(restored.Warnings, loaded.Warnings...))
	loaded.Warnings = nil
	return &Resumed{Session: sess, Loaded: loaded, Restored: restored}, nil
}

// uniqueWarnings returns the warnings once, in the order they first
// appear.
func uniqueWarnings(warnings []string) []string {
	out := make([]string, 0, len(warnings))
	seen := map[string]bool{}
	for _, warning := range warnings {
		if warning == "" || seen[warning] {
			continue
		}
		seen[warning] = true
		out = append(out, warning)
	}
	return out
}
