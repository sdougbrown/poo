package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// The mode the lease file is created with.
//
// The lease file holds no secret, but it belongs to the session, so it
// is created with the session's own mode.
const leaseFileMode = 0o600

// DefaultLeaseTTL is how long a lease lasts before it expires when the
// options set no TTL.
//
// A lease that is not renewed within this long is expired, and a dead
// owner is taken over.
const DefaultLeaseTTL = 30 * time.Second

// leaseState is the lease's on-disk form.
//
// OwnerID names the owner, for humans: the takeover warning says who
// the session was taken from. Token is the fencing token of the
// acquisition that wrote the lease. It is unique to one acquisition,
// so two acquisitions that carry the same name — a stale authority and
// its takeover in one process, or one caller-supplied name across
// processes — never renew or release each other's lease.
type leaseState struct {
	OwnerID     string    `json:"owner_id"`
	Token       string    `json:"token"`
	PID         int       `json:"pid"`
	Host        string    `json:"host"`
	AcquiredAt  time.Time `json:"acquired_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// LeasePath returns the path of a session's lease file.
func LeasePath(dir, id string) string {
	return filepath.Join(dir, id+".lease")
}

// LeaseLocked reports whether a session's lease is live.
//
// A lease is live when it exists and has not expired: now is before the
// lease's expiry. A lease that is missing or expired is not locked. The
// check does not create the lease file: a session that was never leased
// has no lease and is not locked.
func LeaseLocked(path string, now time.Time) (bool, error) {
	state, err := readLease(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return now.Before(state.ExpiresAt), nil
}

// LeaseOptions configures a lease.
type LeaseOptions struct {
	// TTL is how long a lease lasts before it expires.
	//
	// The default is DefaultLeaseTTL.
	TTL time.Duration
	// HeartbeatInterval is how often the owner renews the lease.
	//
	// When zero, it is the TTL divided by three.
	HeartbeatInterval time.Duration
	// OwnerID names the owner.
	//
	// When empty, it is the process's id.
	OwnerID string
	// Now is the lease's clock.
	//
	// AcquireLease defaults an unset Now to time.Now. A store that opens
	// a session substitutes its own clock, the one WithClock sets,
	// before it calls AcquireLease.
	Now func() time.Time
}

// LeaseAuthority is the exclusive right to write a session, held by a
// lease.
//
// The authority is a lease on the session's lease file, renewed by a
// heartbeat. A lease that is not renewed within its TTL is expired, and
// a dead owner is taken over. Once the authority is lost, it is fenced:
// a write fails with ErrLeaseLost, and a late release never touches a
// new owner's lease.
//
// The fencing is by the acquisition's token, not by the owner's name:
// two acquisitions that carry the same name never renew or release each
// other's lease.
type LeaseAuthority struct {
	path     string
	lockPath string
	ttl      time.Duration
	interval time.Duration
	token    string
	now      func() time.Time
	lost     chan struct{}
	stop     chan struct{}
	lostOnce sync.Once
	stopOnce sync.Once
	mu       sync.Mutex
	released bool
	warning  string
}

// AcquireLease takes the lease on a session.
//
// The acquire does not wait: a session another process holds is refused
// with ErrSessionLocked, so a second process never writes into a
// session that is live. A lease that has expired is taken over, and the
// takeover carries a warning naming the previous owner and its expiry.
//
// The acquire, the heartbeat, and the release each take a short
// critical-section flock on the session's lock file, and the lease is
// written by an atomic rename: the flock is never held for the
// session's life, only for each critical section.
func AcquireLease(dir, id string, opts LeaseOptions) (*LeaseAuthority, error) {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	interval := opts.HeartbeatInterval
	if interval <= 0 {
		interval = ttl / 3
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	ownerID := opts.OwnerID
	if ownerID == "" {
		ownerID = strconv.Itoa(os.Getpid())
	}
	host, _ := os.Hostname()

	token, err := newLeaseToken()
	if err != nil {
		return nil, err
	}

	path := LeasePath(dir, id)
	lockPath := LockPath(dir, id)

	f, err := takeLeaseFlock(lockPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	nowT := now()
	var warning string
	state := leaseState{
		OwnerID:     ownerID,
		Token:       token,
		PID:         os.Getpid(),
		Host:        host,
		AcquiredAt:  nowT,
		HeartbeatAt: nowT,
		ExpiresAt:   nowT.Add(ttl),
	}
	if existing, err := readLease(path); err == nil {
		if nowT.Before(existing.ExpiresAt) {
			return nil, ErrSessionLocked
		}
		warning = fmt.Sprintf("the lease was taken over from %s, which expired at %s",
			existing.OwnerID, existing.ExpiresAt.Format(time.RFC3339))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := writeLease(path, state); err != nil {
		return nil, err
	}

	a := &LeaseAuthority{
		path:     path,
		lockPath: lockPath,
		ttl:      ttl,
		interval: interval,
		token:    token,
		now:      now,
		lost:     make(chan struct{}),
		stop:     make(chan struct{}),
		warning:  warning,
	}
	go a.heartbeat()
	return a, nil
}

// Warning returns the warning the acquire carried, when it took over an
// expired lease.
//
// The warning names the previous owner and its expiry, so a supervisor
// can say who it took the session from.
func (a *LeaseAuthority) Warning() string { return a.warning }

// CheckHeld reports whether the authority is still held.
//
// Once the authority is lost, a write fails with ErrLeaseLost.
func (a *LeaseAuthority) CheckHeld() error {
	if a == nil {
		return nil
	}
	if a.isLost() {
		return ErrLeaseLost
	}
	return nil
}

// LeaseLost is the channel that closes when the authority is lost.
func (a *LeaseAuthority) LeaseLost() <-chan struct{} {
	return a.lost
}

// Release gives the authority up.
//
// The lease is removed only when the lease on disk carries this
// authority's token: a lease that was taken over is left alone. A
// release of a lost lease succeeds and never touches a new owner's
// lease.
//
// The heartbeat stops at the first release, whatever happens to the
// cleanup, and the authority counts as released only once the lease is
// gone: a release that fails at the flock, the read, or the removal
// leaves the authority retryable, and a later release tries again. A
// lease whose cleanup never succeeds expires on its own.
func (a *LeaseAuthority) Release() error {
	if a == nil {
		return nil
	}
	// The heartbeat stops at once: a tick that renewed a lease the
	// cleanup could not remove would keep it live.
	a.stopOnce.Do(func() { close(a.stop) })

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.released {
		return nil
	}
	if a.isLost() {
		// A lost lease is not this authority's: never touch it, and
		// there is nothing left to clean up.
		a.released = true
		return nil
	}

	f, err := takeLeaseFlock(a.lockPath)
	if err != nil {
		return err
	}
	defer f.Close()

	state, err := readLease(a.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		// The lease is already gone: the release is done.
		a.released = true
		return nil
	}
	if state.Token != a.token {
		// The lease belongs to another acquisition: leave it alone.
		a.released = true
		return nil
	}
	if err := os.Remove(a.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return &Error{Code: CodeStoreError, Message: "removing " + a.path, Err: err}
	}
	a.released = true
	return nil
}

// Close gives the authority up.
//
// It is the release: a close of a lost lease succeeds and never touches
// a new owner's lease.
func (a *LeaseAuthority) Close() error {
	return a.Release()
}

// heartbeat renews the lease at the interval, until the authority is
// released or the authority is lost.
func (a *LeaseAuthority) heartbeat() {
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		select {
		case <-a.stop:
			return
		case <-ticker.C:
			a.heartbeatTick()
		}
	}
}

// heartbeatTick renews the lease once.
//
// The renewal takes the short critical-section flock, reads the lease,
// and renews it when the lease still carries this authority's token. A
// renewal that fails, or that finds another acquisition's token on
// disk, loses the authority: the lease was taken over, and the
// authority is fenced.
func (a *LeaseAuthority) heartbeatTick() {
	if a.isLost() {
		return
	}
	f, err := takeLeaseFlock(a.lockPath)
	if err != nil {
		a.markLost()
		return
	}
	defer f.Close()
	state, err := readLease(a.path)
	if err != nil {
		a.markLost()
		return
	}
	if state.Token != a.token {
		a.markLost()
		return
	}
	nowT := a.now()
	state.HeartbeatAt = nowT
	state.ExpiresAt = nowT.Add(a.ttl)
	if err := writeLease(a.path, *state); err != nil {
		a.markLost()
		return
	}
}

// markLost loses the authority: it closes the lost channel once and
// stops the heartbeat.
func (a *LeaseAuthority) markLost() {
	a.lostOnce.Do(func() { close(a.lost) })
	a.stopOnce.Do(func() { close(a.stop) })
}

// isLost reports whether the authority is lost.
func (a *LeaseAuthority) isLost() bool {
	select {
	case <-a.lost:
		return true
	default:
		return false
	}
}

// takeLeaseFlock takes the session's lock file for a short critical
// section.
//
// The lock is the session's lock, the same file the flock authority
// holds. The wait lets a critical section pass a probe without waiting
// on a real holder; a refusal costs at most the probe wait. The lock is
// never held for the session's life, only for the critical section.
func takeLeaseFlock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, lockFileMode)
	if err != nil {
		return nil, &Error{Code: CodeStoreError, Message: "opening " + path, Err: err}
	}
	deadline := time.Now().Add(acquireProbeWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !isLockedErr(err) {
			f.Close()
			return nil, &Error{Code: CodeStoreError, Message: "locking " + path, Err: err}
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, ErrSessionLocked
		}
		time.Sleep(acquireProbeRetry)
	}
}

// newLeaseToken returns the fencing token of one acquisition.
//
// The token is sixteen random bytes in hex, so two acquisitions never
// share one: neither the stale authority and its takeover in one
// process, nor two processes that were given the same owner name, can
// be mistaken for each other.
func newLeaseToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", &Error{Code: CodeStoreError, Message: "generating the lease's token", Err: err}
	}
	return hex.EncodeToString(b[:]), nil
}

// readLease reads the lease at the path.
//
// A missing lease is the not-exist error, so a caller can tell a session
// that was never leased from one that was.
func readLease(path string) (*leaseState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, &Error{Code: CodeStoreError, Message: "reading " + path, Err: err}
	}
	var state leaseState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, &Error{Code: CodeCorruptLog, Message: "the lease does not hold a session", Err: err}
	}
	return &state, nil
}

// writeLease writes the lease to the path.
//
// The lease is built in a temporary file and renamed into place, and the
// directory is synced: a failure leaves the previous lease whole, and a
// rename is only durable when the directory that holds it is synced.
func writeLease(path string, state leaseState) error {
	tmp := path + ".tmp"
	os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, leaseFileMode)
	if err != nil {
		return &Error{Code: CodeStoreError, Message: "creating " + tmp, Err: err}
	}
	data, err := json.Marshal(state)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return &Error{Code: CodeStoreError, Message: "encoding the lease", Err: err}
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return &Error{Code: CodeStoreError, Message: "writing " + tmp, Err: err}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return &Error{Code: CodeStoreError, Message: "syncing " + tmp, Err: err}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return &Error{Code: CodeStoreError, Message: "closing " + tmp, Err: err}
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return &Error{Code: CodeStoreError, Message: "renaming " + tmp, Err: err}
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return &Error{Code: CodeStoreError, Message: "syncing " + filepath.Dir(path), Err: err}
	}
	return nil
}
