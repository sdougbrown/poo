package main

import (
	"os"
	"path/filepath"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/session/sqlite"
)

// init registers the store backends the binary links, so the settings'
// session.durable.store name dispatches to them.
func init() {
	if err := config.RegisterStoreBackend("sqlite", openSqliteStore); err != nil {
		panic(err)
	}
}

// openSqliteStore opens the SQLite store a durable session's records go
// to.
//
// The lease options are the settings': the TTL the settings name, and
// the planner's budget the settings' resumption limit, the way the
// file store takes them, and the build is the process's, so a session
// written by another build is loaded with a warning. The database lives
// in the session directory, beside the files a file store would write,
// and the directory is created when it is missing, the way the file
// store creates its own.
func openSqliteStore(dir string, settings config.DurableSettings, build poo.BuildInfo) (session.Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, &session.Error{Code: session.CodeStoreError, Message: "creating " + dir, Err: err}
	}
	lease := sqlite.LeaseOptions{TTL: settings.LeaseTTL}
	durable := session.DurableOptions{MaxResumptions: settings.MaxResumptions}
	return sqlite.New(filepath.Join(dir, "sessions.db"), sqlite.WithBuild(build), sqlite.WithLeaseOptions(lease), sqlite.WithDurableOptions(durable))
}
