package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/session"
)

// The store the package ships.
var _ session.Store = (*Store)(nil)

// Store is the SQLite-backed session store.
//
// The store holds one session open at a time: the session it created or
// loaded last. Its Append and Flush write to that session. A caller that
// serves several sessions at once gives each one its own store over the
// same database.
//
// A session's records are rows in the records table, and its lease is a
// row in the leases table. Every write is a transaction, and the queries
// are indexed.
type Store struct {
	// db is the database.
	db *sql.DB
	// path is the database's path.
	path string
	// codec reads and writes the records.
	codec session.Codec
	// now is the store's clock.
	now func() time.Time
	// build names the binary that owns the store. A session written
	// by another build is loaded with a warning.
	build poo.BuildInfo
	// settingsHash is the hash of the settings the store was made
	// with. A session written with other settings is loaded with a
	// warning.
	settingsHash string
	// policy resolves the pending operations of a session that is
	// loaded.
	policy session.RecoveryPolicy
	// durable is how a durable session's continuation is planned.
	durable session.DurableOptions
	// lease is how the store opens a session: every open session's owner
	// is a lease, renewed by a heartbeat, and a dead owner is taken over
	// after the lease expires. A store given no lease options opens
	// sessions with the default lease.
	lease LeaseOptions
	// mu guards the store's state.
	mu sync.Mutex
	// open is the session the store has open, if any.
	open *openSession
}

// openSession is a session the store has open.
type openSession struct {
	// id identifies the session.
	id string
	// owner is the session's authority: the lease it was opened with,
	// held until the store closes.
	owner *LeaseAuthority
	// tools are the tool classes the session was created with, by
	// name. A tool that is not named here has no class, and its
	// results are not synced.
	tools map[string]poo.ToolKind
	// seq is the number the session's next record is stamped with. The
	// store keeps it so a write does not count the session's records
	// first.
	seq int64
	// syncs counts the syncs the open session made.
	syncs int
}

// Option configures a store.
type Option func(*Store)

// WithCodec sets the codec the store reads and writes with.
//
// The default is the codec of the current session log format.
func WithCodec(codec session.Codec) Option {
	return func(s *Store) {
		if codec != nil {
			s.codec = codec
		}
	}
}

// WithClock sets the store's clock.
//
// The default is time.Now.
func WithClock(now func() time.Time) Option {
	return func(s *Store) {
		if now != nil {
			s.now = now
		}
	}
}

// WithBuild names the binary that owns the store.
//
// A session written by another build is loaded with a warning: the
// records are still readable, but the binary that wrote them is not
// this one.
func WithBuild(build poo.BuildInfo) Option {
	return func(s *Store) { s.build = build }
}

// WithSettingsHash sets the hash of the settings the store was made
// with.
//
// A session written with other settings is loaded with a warning.
func WithSettingsHash(hash string) Option {
	return func(s *Store) { s.settingsHash = hash }
}

// WithRecoveryPolicy sets the policy that resolves a loaded session's
// pending operations.
//
// The default is the interrupt policy: it marks everything interrupted.
// The policy resolves a logged or ephemeral session; a durable session's
// pending operations are planned, and DurableOptions say how.
func WithRecoveryPolicy(policy session.RecoveryPolicy) Option {
	return func(s *Store) {
		if policy != nil {
			s.policy = policy
		}
	}
}

// WithDurableOptions sets how a durable session's continuation is planned.
//
// The load reads the planner's fallback lookup from the session's own
// header, so a plan reads the class and the idempotency the session
// recorded, whatever the store was opened with. A ToolLookup in the
// options outranks it.
func WithDurableOptions(opts session.DurableOptions) Option {
	return func(s *Store) { s.durable = opts }
}

// WithLeaseOptions sets the lease options the store opens sessions
// with.
//
// Leases are not optional: the options tune the lease every session
// opens under. Every open session's owner is a lease: the owner is
// renewed by a heartbeat, and a dead owner is taken over after the lease
// expires. A session that is fenced is refused: its writes fail with
// ErrLeaseLost.
//
// The default is the default lease: the TTL session.DefaultLeaseTTL, the
// heartbeat at the TTL divided by three, the process's id for the owner,
// and the store's clock.
func WithLeaseOptions(opts LeaseOptions) Option {
	return func(s *Store) {
		s.lease = opts
	}
}

// New returns a store over a SQLite database at path.
//
// The database is created when it is missing, and the schema is
// migrated. The store opens with WAL journaling and a busy timeout, and
// every transaction it begins is an immediate one.
//
// Every session the store opens is owned by a lease: a store given no
// lease options opens sessions with the default lease. A session another
// process holds is refused, whatever the store was opened with.
func New(path string, opts ...Option) (*Store, error) {
	s := &Store{
		codec:  session.NewCodec(),
		now:    time.Now,
		policy: session.InterruptPolicy{},
		lease:  LeaseOptions{TTL: session.DefaultLeaseTTL},
	}
	for _, opt := range opts {
		opt(s)
	}
	db, err := Open(path)
	if err != nil {
		return nil, err
	}
	s.db = db
	s.path = path
	return s, nil
}

// Path returns the database's path.
func (s *Store) Path() string { return s.path }

// Create starts a session in the store.
//
// The session's row and its header record are built in one transaction,
// so a failure leaves no session behind. The session's lease is taken
// before the first record is written, and it is held until the store
// closes.
//
// The created record's mode must be a mode this build supports.
func (s *Store) Create(ctx context.Context, created *poo.SessionCreatedRecord) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if created == nil {
		return "", &session.Error{Code: session.CodeStoreError, Message: "the session has no header"}
	}
	if err := session.CheckMode(session.Mode(created.Mode)); err != nil {
		return "", err
	}
	id := created.SessionID
	if id == "" {
		return "", &session.Error{Code: session.CodeStoreError, Message: "the session has no id"}
	}
	if created.Format == "" {
		created.Format = s.codec.Version()
	}
	if created.CreatedAt == "" {
		created.CreatedAt = s.now().UTC().Format(time.RFC3339Nano)
	}

	// The store holds one session at a time: the session it is about to
	// open takes the place of the one it had.
	if err := s.closeOpen(); err != nil {
		return "", err
	}

	owner, err := s.takeOwner(id)
	if err != nil {
		return "", err
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, id).Scan(&exists); err != nil {
		owner.Release()
		return "", err
	}
	if exists > 0 {
		owner.Release()
		return "", &session.Error{Code: session.CodeSessionExists, Message: "the session already exists: " + id}
	}
	meta := session.MetadataOfRecord(created)
	metadataJSON, err := json.Marshal(meta)
	if err != nil {
		owner.Release()
		return "", err
	}
	nowT := s.now()
	poo.Stamp(created, 1, nowT)
	line, err := s.codec.Encode(created)
	if err != nil {
		owner.Release()
		return "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		owner.Release()
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions (id, format, mode, created_at, updated_at, metadata_json) VALUES (?, ?, ?, ?, ?, ?)`,
		id, created.Format, created.Mode, created.CreatedAt, nowT.Format(time.RFC3339Nano), string(metadataJSON)); err != nil {
		tx.Rollback()
		owner.Release()
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO records (session_id, type, run_id, payload, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, created.Meta().Record, "", string(line), nowT.Format(time.RFC3339Nano)); err != nil {
		tx.Rollback()
		owner.Release()
		return "", err
	}
	// The header is the session's first record, so the count the session's
	// numbering continues from is the one this transaction wrote.
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM records WHERE session_id = ?`, id).Scan(&count); err != nil {
		tx.Rollback()
		owner.Release()
		return "", err
	}
	if err := tx.Commit(); err != nil {
		owner.Release()
		return "", err
	}
	s.open = &openSession{
		id:    id,
		owner: owner,
		tools: toolClasses(created.Tools),
		seq:   int64(count) + 1,
	}
	return id, nil
}

// Metadata returns the header of a session the store has not opened.
//
// The read takes no authority: it reads the row the session's create
// wrote and does not open the session. A session the store does not hold
// is its not-found failure.
func (s *Store) Metadata(ctx context.Context, id string) (session.Metadata, error) {
	if err := session.ValidateID(id); err != nil {
		return session.Metadata{}, err
	}
	var metadataJSON string
	err := s.db.QueryRowContext(ctx, `SELECT metadata_json FROM sessions WHERE id = ?`, id).Scan(&metadataJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return session.Metadata{}, session.ErrSessionNotFound
		}
		return session.Metadata{}, &session.Error{Code: session.CodeStoreError, Message: "reading the session's header: " + id, Err: err}
	}
	var meta session.Metadata
	if err := json.Unmarshal([]byte(metadataJSON), &meta); err != nil {
		return session.Metadata{}, &session.Error{Code: session.CodeStoreError, Message: "reading the session's header: " + id, Err: err}
	}
	return meta, nil
}

// takeOwner takes the owner on a session: its lease. Every open session
// has one, and a store given no lease options opens sessions with the
// default lease, so a session another process holds is refused whatever
// the store was opened with.
//
// The authority is held for the store's own life — a lease's heartbeat
// ends only at its release — so it takes no context from the open call.
// The lease runs on the store's clock unless its options name one, so a
// store given a clock leases, and prunes, on that one timeline.
func (s *Store) takeOwner(id string) (*LeaseAuthority, error) {
	opts := s.lease
	if opts.Now == nil {
		opts.Now = s.now
	}
	return AcquireLease(s.db, id, opts)
}

// Load reads a session's records, recovers the session, and opens it.
//
// The load takes the lease on the session, so a session another process
// holds is refused with ErrSessionLocked, and the lease that was taken
// for a load that failed is given up before the failure is returned.
//
// The load recovers the session: an unfinished job is ended, and an
// input that never reached the model is dropped. A logged session's
// unsettled run is marked interrupted; a durable session's is planned
// for a continuation, which the load returns as the session's plan. The
// recovery is idempotent, so a load that failed partway is finished by
// the next one, without writing a record twice.
func (s *Store) Load(ctx context.Context, id string) (*session.Loaded, error) {
	if err := session.ValidateID(id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.closeOpen(); err != nil {
		return nil, err
	}
	owner, err := s.takeOwner(id)
	if err != nil {
		return nil, err
	}
	loaded, err := s.loadLocked(ctx, id, owner)
	if err != nil {
		owner.Release()
		return nil, err
	}
	return loaded, nil
}

// loadLocked loads a session with the owner already taken.
//
// The owner is the caller's: a failure gives it up.
func (s *Store) loadLocked(ctx context.Context, id string, owner *LeaseAuthority) (*session.Loaded, error) {
	var metadataJSON string
	err := s.db.QueryRowContext(ctx, `SELECT metadata_json FROM sessions WHERE id = ?`, id).Scan(&metadataJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, session.ErrSessionNotFound
		}
		return nil, err
	}
	var meta session.Metadata
	if err := json.Unmarshal([]byte(metadataJSON), &meta); err != nil {
		return nil, err
	}
	if err := session.CheckMode(meta.Mode); err != nil {
		return nil, err
	}
	recs, _, err := s.recordsFor(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, &session.Error{Code: session.CodeCorruptLog, Message: "the log holds no session"}
	}
	sink, err := s.makeSink(ctx, id)
	if err != nil {
		return nil, err
	}
	pending := session.PendingOf(id, recs)
	// The recovery resolves the session's pending operations. A durable
	// session's are planned instead, so its run continues. A failure
	// partway leaves the records with what it wrote, and the next load
	// finishes the job.
	continuation, err := s.resolve(ctx, sink, pending, meta.Mode, meta)
	if err != nil {
		return nil, &session.Error{Code: session.CodeStoreError, Message: "recovering the session", Err: err}
	}
	// The records the session holds are the ones in the table, recovery
	// included.
	final, _, err := s.recordsFor(ctx, id)
	if err != nil {
		return nil, err
	}
	// The count is the number the session's records continue from: the
	// recovery records the load wrote are included.
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM records WHERE session_id = ?`, id).Scan(&count); err != nil {
		return nil, err
	}
	s.open = &openSession{
		id:    id,
		owner: owner,
		tools: toolClasses(meta.Tools),
		seq:   int64(count) + 1,
	}
	return &session.Loaded{
		ID:           id,
		Metadata:     meta,
		Records:      final,
		Jobs:         poo.PendingJobResults(final),
		Continuation: continuation,
		Warnings:     s.warnings(meta),
	}, nil
}

// makeSink returns a sink that writes a session's recovery records to the
// records table, continuing its numbering at the session's current count.
func (s *Store) makeSink(ctx context.Context, id string) (poo.RecordSink, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM records WHERE session_id = ?`, id).Scan(&count); err != nil {
		return nil, err
	}
	return &recordSink{
		db:    s.db,
		id:    id,
		codec: s.codec,
		now:   s.now,
		seq:   int64(count),
	}, nil
}

// recordSink writes a session's recovery records to the records table.
type recordSink struct {
	db    *sql.DB
	id    string
	codec session.Codec
	now   func() time.Time
	seq   int64
}

// Append writes one record.
func (r *recordSink) Append(ctx context.Context, rec poo.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.seq++
	at := r.now()
	poo.Stamp(rec, r.seq, at)
	line, err := r.codec.Encode(rec)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO records (session_id, type, run_id, payload, created_at) VALUES (?, ?, ?, ?, ?)`,
		r.id, rec.Meta().Record, runIDOf(rec), string(line), at.Format(time.RFC3339Nano)); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, at.Format(time.RFC3339Nano), r.id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Flush pushes the records out.
//
// Every record is written as it comes, so there is nothing buffered.
func (r *recordSink) Flush(ctx context.Context) error {
	return ctx.Err()
}

// resolve picks the resolver a session's mode names.
//
// A durable session's run is planned for a continuation; every other
// session's pending operations are settled, which is what this build does
// for a logged session.
func (s *Store) resolve(ctx context.Context, sink poo.RecordSink, pending *session.Pending, mode session.Mode, meta session.Metadata) (*session.ContinuationPlan, error) {
	if mode == session.ModeDurable {
		return s.durablePlanner(meta).Plan(ctx, sink, pending)
	}
	policy := s.policy
	if policy == nil {
		policy = session.InterruptPolicy{Now: s.now}
	}
	return nil, policy.Resolve(ctx, sink, pending)
}

// durablePlanner returns the planner a session is loaded with.
//
// The planner reads the session's own tools as its fallback lookup: the
// header is what the session was created with, and a live tool set is the
// caller's to supply.
func (s *Store) durablePlanner(meta session.Metadata) session.DurablePlanner {
	opts := s.durable
	opts.Tools = meta.Tools
	if opts.Now == nil {
		opts.Now = s.now
	}
	return session.DurablePlanner{Options: opts}
}

// warnings reports what changed since the session was written.
//
// A different build or different settings are not failures: the records
// are still readable. A caller that cares reports them.
func (s *Store) warnings(meta session.Metadata) []string {
	var out []string
	if s.build.Name != "" && meta.Build.Name != "" && s.build != meta.Build {
		out = append(out, "the session was made by "+meta.Build.Name+" "+meta.Build.Version+
			", and this build is "+s.build.Name+" "+s.build.Version)
	}
	if s.settingsHash != "" && meta.SettingsHash != "" && s.settingsHash != meta.SettingsHash {
		out = append(out, "the settings changed since the session was created")
	}
	return out
}

// Append writes one record to the open session.
//
// The record is written as one transaction. A run's boundaries, a run's
// resumption, and a side-effecting tool's start and result are synced
// before the append returns: those are the records a crash must not lose.
func (s *Store) Append(ctx context.Context, rec poo.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return &session.Error{Code: session.CodeStoreError, Message: "the store has no open session"}
	}
	if err := s.open.owner.CheckHeld(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	nextSeq := s.open.seq
	at := s.now()
	poo.Stamp(rec, nextSeq, at)
	line, err := s.codec.Encode(rec)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO records (session_id, type, run_id, payload, created_at) VALUES (?, ?, ?, ?, ?)`,
		s.open.id, rec.Meta().Record, runIDOf(rec), string(line), at.Format(time.RFC3339Nano)); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, at.Format(time.RFC3339Nano), s.open.id); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.open.seq = nextSeq + 1
	// The sync is the commit: with the WAL's full synchronous mode, the
	// commit puts the record on disk, so a crash cannot lose it.
	if session.NeedsSync(rec, s.open.tools) {
		s.open.syncs++
	}
	return nil
}

// LeaseLost is the channel that closes when the open session's lease is
// lost, and nil when the store has no open session.
//
// Every open session is leased, so a session that writes watches the
// channel: the lease it lost fences every write, and the session's run
// stops at once.
func (s *Store) LeaseLost() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return nil
	}
	return s.open.owner.LeaseLost()
}

// Flush pushes the open session's records out.
//
// Every record is written as it comes, so there is nothing buffered.
func (s *Store) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return nil
	}
	if err := s.open.owner.CheckHeld(); err != nil {
		return err
	}
	return ctx.Err()
}

// Close releases the store's resources: the open session's lease and the
// database.
//
// The database closes whatever the open session's release said: a lease
// that could not be released is reported, but it is not the database's
// to hold hostage. Both failures are reported, the lease's first.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	releaseErr := s.closeOpen()
	dbErr := s.db.Close()
	return errors.Join(releaseErr, dbErr)
}

// closeOpen closes the open session, if there is one.
//
// The caller holds the lock.
func (s *Store) closeOpen() error {
	if s.open == nil {
		return nil
	}
	open := s.open
	s.open = nil
	return open.owner.Release()
}

// Pending returns the operations a session has not resolved.
//
// The pending operations are what a recovery would resolve: a run that
// started and never settled, a tool call with no result, a permission
// ask with no answer, a job that started and never ended, a feed's
// attempt that started and never ended, and an input that was queued and
// never reached the model.
//
// The read takes no authority: it is a question about the records.
func (s *Store) Pending(ctx context.Context, id string) (*session.Pending, error) {
	if err := session.ValidateID(id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, _, err := s.recordsFor(ctx, id)
	if err != nil {
		return nil, err
	}
	return session.PendingOf(id, recs), nil
}

// List returns the summaries of the sessions in the store.
//
// A summary is what a reader of the store wants: the session's id, when
// it was created, the directory it works in, its profile and build, how
// many runs it had, why its last run settled, how big its records are,
// and whether a process holds its lease.
func (s *Store) List(ctx context.Context) ([]session.Listing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(ctx)
}

// listLocked lists the sessions in the store.
//
// The caller holds the lock.
func (s *Store) listLocked(ctx context.Context) ([]session.Listing, error) {
	var out []session.Listing
	rows, err := s.db.QueryContext(ctx, `SELECT id, metadata_json FROM sessions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, metadataJSON string
		if err := rows.Scan(&id, &metadataJSON); err != nil {
			return nil, err
		}
		var meta session.Metadata
		if err := json.Unmarshal([]byte(metadataJSON), &meta); err != nil {
			return nil, err
		}
		recs, size, err := s.recordsFor(ctx, id)
		if err != nil {
			return nil, err
		}
		sum := session.Projection{}.Summarize(meta, recs)
		sum.Size = size
		locked, err := LeaseLocked(s.db, id, s.now())
		if err != nil {
			return nil, err
		}
		sum.Locked = locked
		out = append(out, sum)
	}
	return out, nil
}

// Prune deletes the sessions a policy marks.
//
// A live-leased session is never pruned: the lease holds it. A
// session that is loaded while the prune runs is either loaded whole or
// is gone: the store's lock keeps the two apart.
//
// Nothing is deleted without a policy: a prune with neither an age nor
// a count fails with ErrNoPolicy.
func (s *Store) Prune(ctx context.Context, policy session.Policy) (*session.PruneResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if policy.MaxAge <= 0 && policy.Keep <= 0 {
		return nil, session.ErrNoPolicy
	}
	now := policy.Now
	if now.IsZero() {
		now = s.now()
	}
	summaries, err := s.listLocked(ctx)
	if err != nil {
		return nil, err
	}
	byActivity, marked := session.MarkedBy(policy, summaries, now)

	result := &session.PruneResult{}
	for _, sum := range byActivity {
		if !marked(sum) {
			continue
		}
		// A live-leased session is never pruned: the lease holds it.
		leased, err := LeaseLocked(s.db, sum.ID, now)
		if err != nil {
			return nil, err
		}
		if leased {
			result.Skipped = append(result.Skipped, sum)
			continue
		}
		if !policy.DryRun {
			if err := s.deleteSession(ctx, sum.ID); err != nil {
				return nil, err
			}
		}
		result.Deleted = append(result.Deleted, sum)
	}
	return result, nil
}

// deleteSession deletes a session's rows: its records, its lease, and its
// summary, in one transaction.
func (s *Store) deleteSession(ctx context.Context, id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM records WHERE session_id = ?`, id); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM leases WHERE session_id = ?`, id); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ReadRecords reads the open session's records back, oldest first.
//
// It is the read side poo.RecordReader asks for: the session's history
// tool and its strategies read what the store retains through it. The
// records are whole rows, so a reader sees them whole.
func (s *Store) ReadRecords(ctx context.Context) ([]poo.Record, error) {
	s.mu.Lock()
	if s.open == nil {
		s.mu.Unlock()
		return nil, &session.Error{Code: session.CodeStoreError, Message: "the store has no open session"}
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, &session.Error{Code: session.CodeStoreError, Message: "reading the retained chain", Err: err}
	}
	recs, _, err := s.recordsFor(ctx, s.open.id)
	s.mu.Unlock()
	return recs, err
}

// recordsFor returns a session's records, in order, and their size.
func (s *Store) recordsFor(ctx context.Context, id string) ([]poo.Record, int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM records WHERE session_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var recs []poo.Record
	var size int64
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, 0, err
		}
		size += int64(len(payload))
		rec, err := s.codec.Decode([]byte(payload))
		if err != nil {
			return nil, 0, err
		}
		recs = append(recs, rec)
	}
	return recs, size, nil
}

// toolClasses returns the tool classes of a session, by name.
func toolClasses(tools []poo.ToolInfo) map[string]poo.ToolKind {
	out := make(map[string]poo.ToolKind, len(tools))
	for _, tool := range tools {
		out[tool.Name] = tool.Class
	}
	return out
}

// runIDOf returns the run id a record names, and the empty string for a
// record that names no run.
func runIDOf(rec poo.Record) string {
	switch r := rec.(type) {
	case *poo.RunStartedRecord:
		return r.RunID
	case *poo.RunSettledRecord:
		return r.RunID
	case *poo.ToolCallRecord:
		return r.RunID
	case *poo.ToolResultRecord:
		return r.RunID
	case *poo.MessageRecord:
		return r.RunID
	case *poo.ToolStartedRecord:
		return r.RunID
	case *poo.RunResumedRecord:
		return r.RunID
	case *poo.PermissionAskedRecord:
		return r.RunID
	case *poo.PermissionAnsweredRecord:
		return r.RunID
	case *poo.JobStartedRecord:
		return r.RunID
	case *poo.JobEndedRecord:
		return r.RunID
	case *poo.TurnHookRecord:
		return r.RunID
	}
	return ""
}
