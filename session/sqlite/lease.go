package sqlite

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/sdougbrown/poo/session"
)

// LeaseOptions configures a lease.
//
// It mirrors the file store's lease options: the TTL, the heartbeat
// interval, the owner's name, and the clock. The defaults are this
// package's: the TTL session.DefaultLeaseTTL, a heartbeat at the TTL
// divided by three, the process's id for the owner, and an unset Now is
// the store's clock, which WithClock sets.
//
// The file store's lease does not follow its store's clock: it hands its
// options to AcquireLease, whose nil-Now default is time.Now.
type LeaseOptions struct {
	// TTL is how long a lease lasts before it expires.
	//
	// The default is session.DefaultLeaseTTL.
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
	// The default is the store's clock: WithClock sets it.
	Now func() time.Time
}

// LeaseAuthority is the exclusive right to write a session, held by a
// lease.
//
// It is the SQLite-backed mirror of the file store's lease authority:
// the lease is a row, renewed by a heartbeat, and a dead owner is taken
// over after the lease expires. Once the authority is lost, it is
// fenced: a write fails with ErrLeaseLost, and a late release never
// touches a new owner's lease.
//
// The fencing is by the acquisition's token, not by the owner's name:
// two acquisitions that carry the same name never renew or release each
// other's lease. Every acquire, heartbeat, and release is an immediate
// transaction, so a read-then-write is atomic.
type LeaseAuthority struct {
	db        *sql.DB
	sessionID string
	ttl       time.Duration
	interval  time.Duration
	token     string
	now       func() time.Time
	lost      chan struct{}
	stop      chan struct{}
	// done closes when the heartbeat has ended, so a release can wait for
	// the renewal loop instead of letting it outlive the authority.
	done     chan struct{}
	lostOnce sync.Once
	stopOnce sync.Once
	mu       sync.Mutex
	released bool
	warning  string
	// tickHook is called at the top of heartbeatTick. It is a test hook:
	// unset in production, it lets a test hold a tick in flight.
	tickHook func()
}

// AcquireLease takes the lease on a session.
//
// The acquire does not wait: a session another process holds is refused
// with ErrSessionLocked, so a second process never writes into a
// session that is live. A lease that has expired is taken over, and the
// takeover carries a warning naming the previous owner and its expiry.
//
// The acquire is one immediate transaction: it reads the lease and
// replaces it in a single read-then-write, so two concurrent acquires
// never both take a session.
func AcquireLease(db *sql.DB, sessionID string, opts LeaseOptions) (*LeaseAuthority, error) {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = session.DefaultLeaseTTL
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

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	nowT := now()
	var warning string
	var existingOwner, existingExpires string
	err = tx.QueryRow(`SELECT owner_id, expires_at FROM leases WHERE session_id = ?`, sessionID).Scan(&existingOwner, &existingExpires)
	if err == nil {
		existingExp, perr := time.Parse(time.RFC3339Nano, existingExpires)
		if perr != nil {
			tx.Rollback()
			return nil, perr
		}
		if nowT.Before(existingExp) {
			tx.Rollback()
			return nil, session.ErrSessionLocked
		}
		warning = fmt.Sprintf("the lease was taken over from %s, which expired at %s",
			existingOwner, existingExp.Format(time.RFC3339))
	} else if !errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		return nil, err
	}
	_, err = tx.Exec(`INSERT OR REPLACE INTO leases (session_id, owner_id, pid, host, token, acquired_at, heartbeat_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, ownerID, os.Getpid(), host, token,
		nowT.Format(time.RFC3339Nano), nowT.Format(time.RFC3339Nano), nowT.Add(ttl).Format(time.RFC3339Nano))
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	a := &LeaseAuthority{
		db:        db,
		sessionID: sessionID,
		ttl:       ttl,
		interval:  interval,
		token:     token,
		now:       now,
		lost:      make(chan struct{}),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		warning:   warning,
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
		return session.ErrLeaseLost
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
// cleanup, and the release waits for it to end: no renewal runs behind
// the cleanup, and no heartbeat outlives the release. The authority
// counts as released only once the lease is gone: a release that fails
// at the read or the removal leaves the authority retryable, and a later
// release tries again. A lease whose cleanup never succeeds expires on
// its own.
func (a *LeaseAuthority) Release() error {
	if a == nil {
		return nil
	}
	// The heartbeat stops at once: a tick that renewed a lease the
	// cleanup could not remove would keep it live.
	a.stopOnce.Do(func() { close(a.stop) })
	<-a.done

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

	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var token string
	err = tx.QueryRow(`SELECT token FROM leases WHERE session_id = ?`, a.sessionID).Scan(&token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The lease is already gone: the release is done.
			tx.Rollback()
			a.released = true
			return nil
		}
		tx.Rollback()
		return err
	}
	if token != a.token {
		// The lease belongs to another acquisition: leave it alone.
		tx.Rollback()
		a.released = true
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM leases WHERE session_id = ? AND token = ?`, a.sessionID, a.token); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
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
//
// The heartbeat's lifetime is the lease's own: it takes no context from
// the caller, so a request-scoped context that ends does not stop the
// renewal. It closes the authority's done channel when it ends, so a
// release never returns while a tick can still write.
func (a *LeaseAuthority) heartbeat() {
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	defer close(a.done)
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
// The renewal is one immediate transaction: it reads the lease and
// renews it when the lease still carries this authority's token. A
// renewal that fails, or that finds another acquisition's token on
// disk, loses the authority: the lease was taken over, and the
// authority is fenced.
func (a *LeaseAuthority) heartbeatTick() {
	a.mu.Lock()
	hook := a.tickHook
	a.mu.Unlock()
	if hook != nil {
		hook()
	}
	if a.isLost() {
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		a.markLost()
		return
	}
	defer tx.Rollback()

	var token string
	err = tx.QueryRow(`SELECT token FROM leases WHERE session_id = ?`, a.sessionID).Scan(&token)
	if err != nil {
		tx.Rollback()
		a.markLost()
		return
	}
	if token != a.token {
		// The lease was taken over: the authority is fenced.
		tx.Rollback()
		a.markLost()
		return
	}
	nowT := a.now()
	if _, err := tx.Exec(`UPDATE leases SET heartbeat_at = ?, expires_at = ? WHERE session_id = ? AND token = ?`,
		nowT.Format(time.RFC3339Nano), nowT.Add(a.ttl).Format(time.RFC3339Nano), a.sessionID, a.token); err != nil {
		tx.Rollback()
		a.markLost()
		return
	}
	if err := tx.Commit(); err != nil {
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

// LeaseLocked reports whether a session's lease is live.
//
// A lease is live when it exists and has not expired: now is before the
// lease's expiry. A lease that is missing or expired is not locked. The
// check does not create the lease row: a session that was never leased
// has no lease and is not locked.
func LeaseLocked(db *sql.DB, sessionID string, now time.Time) (bool, error) {
	var expiresAt string
	err := db.QueryRow(`SELECT expires_at FROM leases WHERE session_id = ?`, sessionID).Scan(&expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	exp, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return false, err
	}
	return now.Before(exp), nil
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
		return "", &session.Error{Code: session.CodeStoreError, Message: "generating the lease's token", Err: err}
	}
	return hex.EncodeToString(b[:]), nil
}
