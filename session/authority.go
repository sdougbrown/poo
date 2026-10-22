package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// The mode the lock file is created with.
//
// The lock file holds no secret, but it belongs to the session, so it is
// created with the session's own mode.
const lockFileMode = 0o600

// Authority is the exclusive right to write a session.
//
// The authority is an exclusive flock on the session's lock file. It is
// taken when a session is created or loaded, and it is held until the
// store closes. The kernel releases it when the process dies, so a stale
// owner never blocks a recovery: a killed process leaves the lock free.
type Authority struct {
	// f is the lock file.
	f *os.File
	// path is the lock file's path.
	path string
}

// LockPath returns the path of a session's lock file.
//
// The lock file is never deleted: a lock that names no session is a
// harmless leftover, and deleting one would break the mutual exclusion
// a reader of the directory expects.
func LockPath(dir, id string) string {
	return filepath.Join(dir, id+".lock")
}

// Acquire takes the authority on a session.
//
// The acquire does not wait: a session another process holds is refused
// with ErrSessionLocked, so a second process never writes into a
// session that is live.
//
// The lock file is created when it is missing. The file is the session's
// lock, and its presence says the session was opened at least once.
func Acquire(path string) (*Authority, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, lockFileMode)
	if err != nil {
		return nil, &Error{Code: CodeStoreError, Message: "opening " + path, Err: err}
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if isLockedErr(err) {
			return nil, ErrSessionLocked
		}
		return nil, &Error{Code: CodeStoreError, Message: "locking " + path, Err: err}
	}
	return &Authority{f: f, path: path}, nil
}

// Path returns the path of the lock file the authority is held on.
func (a *Authority) Path() string { return a.path }

// Release gives the authority up.
//
// The lock file stays: it is the session's lock, and the next holder
// takes it again.
func (a *Authority) Release() error {
	if a == nil || a.f == nil {
		return nil
	}
	f := a.f
	a.f = nil
	unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	closeErr := f.Close()
	if unlockErr != nil {
		return &Error{Code: CodeStoreError, Message: "unlocking " + a.path, Err: unlockErr}
	}
	if closeErr != nil {
		return &Error{Code: CodeStoreError, Message: "closing " + a.path, Err: closeErr}
	}
	return nil
}

// Locked reports whether a session's lock is held by another process.
//
// The check does not create the lock file: a session that was never
// opened has no lock and is not locked.
func Locked(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR, lockFileMode)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, &Error{Code: CodeStoreError, Message: "opening " + path, Err: err}
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if isLockedErr(err) {
			return true, nil
		}
		return false, &Error{Code: CodeStoreError, Message: "locking " + path, Err: err}
	}
	// The check took the lock and gives it up again: the session is
	// free.
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}

// isLockedErr reports whether a flock failure means the lock is held.
func isLockedErr(err error) bool {
	return err == syscall.EWOULDBLOCK || err == syscall.EAGAIN
}
