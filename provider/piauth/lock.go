package piauth

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"
)

// The lock pi takes on its auth.json.
//
// pi locks the file with proper-lockfile, and that library takes the lock
// by creating the directory <file>.lock with mkdir: the mkdir is what
// makes the acquisition atomic, and the directory is what says a holder is
// there. This package speaks the same three rules, because agreeing on them
// is what makes two processes exclude each other:
//
//   - the lock is the directory <auth.json>.lock, holding nothing;
//   - a holder keeps the directory's mtime fresh while it holds it;
//   - a writer may break a lock whose mtime is older than the staleness
//     bound.
//
// The directory holds nothing on purpose: pi releases its lock with
// rmdir, which fails on a directory that holds a file, so a marker file in
// there would leave pi unable to release.
//
// Ownership is the directory's identity and not only its mtime. A writer
// that breaks a lock and takes it creates a new directory, so the inode
// differs even when the filesystem's mtime granularity would give the
// two directories the same stamp; a filesystem that rounds the mtime still
// cannot hide a different directory. The mtime is checked beside the
// identity, so a lock whose stamp changed under this holder is treated as
// taken rather than as this holder's. What remains is the window every
// mkdir lock has: a takeover between the last check and the write itself is
// not detectable, and no amount of stamping closes it. pi's lock is the
// same rule with the mtime alone.
//
// A filesystem that will not take the stamp ends the holder's keepalive
// instead of leaving a lock no later writer can judge: the refresh is
// reported as not stored rather than written under a lock that may already
// be someone else's.
const (
	// lockSuffix names the directory the lock is.
	lockSuffix = ".lock"

	// lockStale is how old the lock directory's mtime may get before
	// another writer may break the lock. pi's async refresh waits out a
	// lock for thirty seconds, so a shorter bound here would break pi's
	// lock while pi is still holding it.
	lockStale = 30 * time.Second

	// lockRefresh is how often the holder puts a fresh mtime on the lock
	// directory, so a refresh that takes seconds does not go stale under
	// it. pi's synchronous path breaks a lock that is ten seconds old,
	// which is the shortest bound either side uses, so this stays under
	// it.
	lockRefresh = 5 * time.Second

	// lockDirMode is the mode the lock directory is created with. It
	// holds nothing, and a writer of the auth file needs the directory
	// only to exist.
	lockDirMode = 0o700

	// lockWaitBase and lockWaitMax bound one wait between two attempts on
	// a held lock: the wait doubles with each attempt, up to the cap.
	lockWaitBase = 10 * time.Millisecond
	lockWaitMax  = 500 * time.Millisecond
)

// lock is a held lock on one auth.json.
type lock struct {
	// path is the lock directory's path.
	path string
	// dir is the directory this holder created. Its identity is what says
	// the directory is still this holder's, whatever the mtime says.
	dir os.FileInfo
	// done stops the refresh, and stopped says it has stopped.
	done    chan struct{}
	stopped chan struct{}
	// mu guards mtime and lost, and it is held across a whole check and
	// stamp: a check that ran beside a stamp would read the stamp the
	// holder is in the middle of writing and take its own lock for
	// another writer's.
	mu sync.Mutex
	// mtime is the mtime this holder last saw on the directory.
	mtime time.Time
	// lost is the failure the refresh found, and the reason this holder
	// may not write.
	lost error
}

// acquire takes the lock of an auth.json, waiting for its holder for at
// most wait.
//
// The lock's business is the filesystem's time, so every deadline here is
// the wall clock's, not the clock a test ages a credential with.
func acquire(ctx context.Context, path string, wait time.Duration) (*lock, error) {
	lockPath := path + lockSuffix
	deadline := time.Now().Add(wait)
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := os.Mkdir(lockPath, lockDirMode)
		if err == nil {
			return held(lockPath)
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("%w: locking %s: %w", ErrStorage, path, err)
		}
		info, err := os.Stat(lockPath)
		if errors.Is(err, fs.ErrNotExist) {
			// The holder released between the mkdir and the stat: the
			// lock is free, so the next attempt takes it.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: locking %s: %w", ErrStorage, path, err)
		}
		if time.Since(info.ModTime()) >= lockStale {
			// The holder is gone, and pi breaks a lock this old too, so
			// breaking it is what keeps a dead holder from blocking the
			// file for good.
			if err := os.Remove(lockPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("%w: breaking the stale lock %s: %w", ErrStorage, lockPath, err)
			}
			continue
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, lockPath)
		}
		pause := min(waitDelay(attempt), time.Until(deadline))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pause):
		}
	}
}

// held starts a lock's refresh and hands the lock over. The mkdir that took
// the lock also stamped the directory, so the fresh lock needs no stamp.
func held(lockPath string) (*lock, error) {
	info, err := os.Stat(lockPath)
	if err != nil {
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("%w: locking %s: %w", ErrStorage, lockPath, err)
	}
	hold := &lock{
		path:    lockPath,
		dir:     info,
		mtime:   info.ModTime(),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go hold.keep()
	return hold, nil
}

// waitDelay is how long the next attempt on a held lock waits.
func waitDelay(attempt int) time.Duration {
	delay := lockWaitBase
	for i := 0; i < attempt && delay < lockWaitMax; i++ {
		delay *= 2
	}
	return min(delay, lockWaitMax)
}

// keep puts a fresh mtime on the lock directory until the lock is released,
// so a refresh that takes a while does not go stale under it.
func (l *lock) keep() {
	defer close(l.stopped)
	ticker := time.NewTicker(lockRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-ticker.C:
		}
		if err := l.touch(); err != nil {
			l.mu.Lock()
			l.lost = err
			l.mu.Unlock()
			return
		}
	}
}

// touch puts this holder's time on the lock directory, after checking the
// directory is still this holder's: a tick that stamped a directory another
// writer took would hand this holder a lock that writer believes it owns.
//
// The check, the stamp and the record are one critical section, so the
// pre-write check never reads a stamp this holder is in the middle of
// changing.
func (l *lock) touch() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lost != nil {
		return l.lost
	}
	if err := l.owns(); err != nil {
		return err
	}
	now := time.Now()
	if err := os.Chtimes(l.path, now, now); err != nil {
		return fmt.Errorf("stamping %s: %w", l.path, err)
	}
	info, err := os.Stat(l.path)
	if err != nil {
		return fmt.Errorf("stating %s: %w", l.path, err)
	}
	// The filesystem may keep less of the stamp than it was given, so the
	// record is what the filesystem kept and not what was asked for.
	l.mtime = info.ModTime()
	return nil
}

// owns reports whether the directory is still the one this holder created
// and still carries the mtime this holder left on it. The caller holds mu.
func (l *lock) owns() error {
	info, err := os.Stat(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s was taken away", l.path)
	}
	if err != nil {
		return fmt.Errorf("stating %s: %w", l.path, err)
	}
	if !os.SameFile(l.dir, info) {
		return fmt.Errorf("another writer took %s", l.path)
	}
	if !info.ModTime().Equal(l.mtime) {
		return fmt.Errorf("another writer restamped %s", l.path)
	}
	return nil
}

// verify reports whether this holder still owns the lock.
//
// A writer calls it before a write, because a lock taken from this holder is
// a write the other writer does not know about.
func (l *lock) verify() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lost != nil {
		return l.lost
	}
	return l.owns()
}

// release gives the lock up.
//
// A directory that is no longer this holder's is left alone: it belongs to
// whoever took it over, and removing it would hand the file to a third
// writer while the taker believes it holds the lock. A release that cannot
// remove the directory reports nothing, the way pi's own release swallows
// its failure: the directory goes stale and the next writer breaks it.
func (l *lock) release() {
	close(l.done)
	<-l.stopped
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owns() != nil {
		return
	}
	_ = os.Remove(l.path)
}
