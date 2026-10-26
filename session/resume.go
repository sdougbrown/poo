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
// reads its records: a torn tail is truncated, an unsettled run is
// marked interrupted, an unfinished job is ended, and an input that
// never reached the model is dropped. The restore rebuilds a session
// from those records, and the job results that were never delivered are
// queued again.
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
