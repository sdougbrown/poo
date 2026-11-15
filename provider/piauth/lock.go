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
// pi locks the file with proper-lockfile, which takes the lock by creating
// the directory <file>.lock with mkdir. Agreement on the rules is what makes
// two processes exclude each other.
//
// The lock is the directory <auth.json>.lock, holding nothing. A holder keeps
// the directory's mtime fresh. A writer may break a lock older than the
// staleness bound.
//
// The directory stays empty because pi releases its lock with rmdir. A
// marker file in there would leave pi unable to release.
//
// Ownership is the directory's identity, not only its mtime. A takeover
// creates a new directory, and its identity says the lock changed hands,
// whatever stamp the taker leaves. The mtime is checked beside that identity:
// a stamp that changed under this holder means the lock is taken. What
// remains is the window every mkdir lock has: a takeover between the last
// check and the write is not detectable.
//
// A bare inode number is not identity enough. A filesystem hands a removed
// directory's number out again, often to the very next mkdir. A takeover
// inside one coarse mtime tick then looks exactly like the holder's own
// directory. So the holder keeps an open handle on that directory for the
// whole hold. A kernel does not free an inode a live reference holds, so no
// taker can be handed the number this holder is keeping alive. The holder
// states the handle as well. A filesystem that frees the directory anyway, as
// a FUSE mount of an ext4 image does, fails that stat.
//
// A filesystem that will not take the stamp ends the holder's keepalive
// rather than leaving a lock no later writer can judge. The refresh is
// reported as not stored instead of written under a lock that may already
// be someone else's.
const (
	// lockSuffix names the directory the lock is.
	lockSuffix = ".lock"

	// lockStale is how old the lock directory's mtime may get before
	// another writer may break the lock. pi's async refresh waits out a
	// lock for thirty seconds. A shorter bound here would break pi's lock
	// while pi still holds it.
	lockStale = 30 * time.Second

	// lockRefresh is how often the holder restamps the lock directory, so a
	// refresh that takes seconds does not go stale under it. pi's
	// synchronous path breaks a lock ten seconds old, the shortest bound
	// either side uses. This stays under it.
	lockRefresh = 5 * time.Second

	// lockDirMode is the mode the lock directory is created with.
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
	// dir is the directory this holder created. Its identity says the
	// directory is still this holder's, whatever the mtime says.
	dir os.FileInfo
	// handle is this holder's open reference to that directory, held for
	// the whole hold. It keeps dir's inode from being handed to a taker,
	// and it is closed only when the lock is released. Windows will not
	// remove a directory with an open handle, its own process's included,
	// so release closes first. poo ships for Linux and macOS, which allow
	// both.
	handle *os.File
	// done stops the refresh, and stopped says it has stopped.
	done    chan struct{}
	stopped chan struct{}
	// mu guards mtime and lost, and it is held across a whole check and
	// stamp. A check that ran beside a stamp would read the half-written
	// stamp and take this lock for another writer's.
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
// Every deadline here is the wall clock's, not the clock a test ages a
// credential with, because the lock's business is the filesystem's time.
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
			// The holder released between the mkdir and the stat, so the
			// lock is free and the next attempt takes it.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: locking %s: %w", ErrStorage, path, err)
		}
		if time.Since(info.ModTime()) >= lockStale {
			// The holder is gone. pi breaks a lock this old too, so
			// breaking it keeps a dead holder from blocking the file.
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
//
// The identity is read through the handle, so record and reference are one
// inode: the one the mkdir created.
func held(lockPath string) (*lock, error) {
	handle, err := os.Open(lockPath)
	if err != nil {
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("%w: locking %s: %w", ErrStorage, lockPath, err)
	}
	info, err := handle.Stat()
	if err != nil {
		_ = handle.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("%w: locking %s: %w", ErrStorage, lockPath, err)
	}
	hold := &lock{
		path:    lockPath,
		dir:     info,
		handle:  handle,
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

// keep puts a fresh mtime on the lock directory until the lock is released.
// A refresh that takes a while then does not go stale under it.
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

// touch puts this holder's time on the lock directory, after checking that
// the directory is still this holder's. A tick that stamped another writer's
// directory would hand this holder a lock that writer believes it owns.
//
// The check, the stamp and the record are one critical section. A check never
// reads a stamp this holder is in the middle of changing.
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
	// record holds what it kept.
	l.mtime = info.ModTime()
	return nil
}

// owns reports whether the directory is still the one this holder created
// and still carries the mtime this holder left on it. The caller holds mu.
//
// The handle's own stat comes first. A directory whose inode the filesystem
// freed under this holder is one a taker replaced. Such a filesystem can hand
// the taker the number the holder recorded.
func (l *lock) owns() error {
	if _, err := l.handle.Stat(); err != nil {
		return fmt.Errorf("the held directory %s is gone: %w", l.path, err)
	}
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
// A directory that is no longer this holder's is left alone, because it
// belongs to whoever took it over. Removing it would hand the file to a
// third writer while the taker believes it holds the lock. A release that
// cannot remove the directory reports nothing, as pi's own release does. The
// directory then goes stale and the next writer breaks it.
//
// The handle is closed before the directory is removed, because Windows will
// not remove a directory its own process holds open. The check above has
// already settled that the directory is this holder's.
func (l *lock) release() {
	close(l.done)
	<-l.stopped
	l.mu.Lock()
	defer l.mu.Unlock()
	owned := l.owns() == nil
	_ = l.handle.Close()
	if !owned {
		return
	}
	_ = os.Remove(l.path)
}
