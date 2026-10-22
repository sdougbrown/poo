package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sdougbrown/poo"
)

// Mode is a session's persistence mode.
type Mode string

const (
	// ModeLogged stores the session's records in a file. The file is
	// the session's: it is locked while the session is open, and it is
	// recovered when the session is loaded.
	ModeLogged Mode = "logged"
	// ModeEphemeral keeps the session's records in memory, for the
	// length of the process. Nothing is written, and there is nothing
	// to load.
	ModeEphemeral Mode = "ephemeral"
	// ModeDurable is reserved. v1 marks an interrupted run and moves
	// on; a durable session continues it.
	ModeDurable Mode = "durable"
)

// CheckMode returns the failure a mode names, when this build does not
// support it.
//
// A durable session is refused with ErrUnsupportedMode.
func CheckMode(mode Mode) error {
	switch mode {
	case ModeLogged, ModeEphemeral:
		return nil
	case ModeDurable:
		return ErrUnsupportedMode
	}
	return &Error{Code: CodeUnsupportedMode, Message: "unknown persistence mode: " + string(mode)}
}

// Store holds sessions' records.
//
// A store creates sessions, loads them, lists them and prunes them. It
// implements poo.RecordSink, so a caller hands it to a core session as
// the session's sink.
type Store interface {
	poo.RecordSink

	// Create starts a session in the store and makes it the store's
	// current session. It returns the session's id.
	Create(ctx context.Context, created *poo.SessionCreated) (string, error)

	// Load reads a session's records, recovers the session, and makes
	// it the store's current session.
	Load(ctx context.Context, id string) (*Loaded, error)

	// Pending returns the operations a session has not resolved.
	Pending(ctx context.Context, id string) (*Pending, error)

	// List returns the summaries of the sessions in the store.
	List(ctx context.Context) ([]Summary, error)

	// Prune deletes the sessions a policy marks.
	Prune(ctx context.Context, policy Policy) (*PruneResult, error)

	// Close releases the store's resources: the current session's file
	// and its lock.
	Close() error
}

// FileStore is the file-backed store: one JSONL file per session.
//
// The store holds one session open at a time: the session it created or
// loaded last. Its Append and Flush write to that session. A caller that
// serves several sessions at once gives each one its own store over the
// same directory.
//
// A session's file is <dir>/<session_id>.jsonl, and its lock is
// <dir>/<session_id>.lock.
type FileStore struct {
	// dir is the directory the session files live in.
	dir string
	// codec reads and writes the records.
	codec Codec
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
	policy RecoveryPolicy
	// mu guards the store's state.
	mu sync.Mutex
	// open is the session the store has open, if any.
	open *openSession
	// initFault is the initialization step the store fails at. It is a
	// test hook: the number counts the steps of a session's
	// construction.
	initFault int
	// appendFault is the number of appends to the open session that
	// succeed before an append fails. It is a test hook.
	appendFault int
}

// openSession is a session the store has open.
type openSession struct {
	// id identifies the session.
	id string
	// path is the session's file.
	path string
	// lock is the session's authority.
	lock *Authority
	// log writes the session's records.
	log *Log
	// tools are the tool classes the session was created with, by
	// name. A tool that is not named here has no class, and its
	// results are not synced.
	tools map[string]poo.ToolClass
}

// Option configures a store.
type Option func(*FileStore)

// WithCodec sets the codec the store reads and writes with.
//
// The default is the codec of the current session log format.
func WithCodec(codec Codec) Option {
	return func(s *FileStore) {
		if codec != nil {
			s.codec = codec
		}
	}
}

// WithClock sets the store's clock.
//
// The default is time.Now.
func WithClock(now func() time.Time) Option {
	return func(s *FileStore) {
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
	return func(s *FileStore) { s.build = build }
}

// WithSettingsHash sets the hash of the settings the store was made
// with.
//
// A session written with other settings is loaded with a warning.
func WithSettingsHash(hash string) Option {
	return func(s *FileStore) { s.settingsHash = hash }
}

// WithRecoveryPolicy sets the policy that resolves a loaded session's
// pending operations.
//
// The default is the interrupt policy: v1 marks everything interrupted.
func WithRecoveryPolicy(policy RecoveryPolicy) Option {
	return func(s *FileStore) {
		if policy != nil {
			s.policy = policy
		}
	}
}

// NewStore returns a store over a session directory.
//
// The directory is created when it is missing. It holds the user's
// prompts and answers, so it is created private to its owner.
func NewStore(dir string, opts ...Option) (*FileStore, error) {
	s := &FileStore{
		dir:    dir,
		codec:  NewCodec(),
		now:    time.Now,
		policy: InterruptPolicy{},
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, &Error{Code: CodeStoreError, Message: "creating " + dir, Err: err}
	}
	return s, nil
}

// Dir returns the directory the store keeps its sessions in.
func (s *FileStore) Dir() string { return s.dir }

// path returns the path of a session's file.
func (s *FileStore) path(id string) string {
	return filepath.Join(s.dir, id+".jsonl")
}

// Create starts a session in the store.
//
// The session's file is built in a temporary file and renamed into
// place, so a failure leaves no session behind: a half-written log is
// never a session. The store's authority is taken before the first
// record is written, and it is held until the store closes.
//
// The created record's mode must be a mode this build supports: a
// durable session is refused with ErrUnsupportedMode.
func (s *FileStore) Create(ctx context.Context, created *poo.SessionCreated) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if created == nil {
		return "", &Error{Code: CodeStoreError, Message: "the session has no header"}
	}
	if err := CheckMode(Mode(created.Mode)); err != nil {
		return "", err
	}
	id := created.SessionID
	if id == "" {
		return "", &Error{Code: CodeStoreError, Message: "the session has no id"}
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

	lock, err := Acquire(LockPath(s.dir, id))
	if err != nil {
		return "", err
	}
	path := s.path(id)
	if _, err := os.Stat(path); err == nil {
		// The session is already there: a log holds one session, and
		// a second one would take the first one's records with it.
		lock.Release()
		return "", &Error{Code: CodeSessionExists, Message: "the session already exists: " + id}
	} else if !errors.Is(err, fs.ErrNotExist) {
		lock.Release()
		return "", &Error{Code: CodeStoreError, Message: "stating " + path, Err: err}
	}
	if err := s.writeSessionFile(ctx, path, created); err != nil {
		lock.Release()
		return "", err
	}
	log, err := openLog(path, 1)
	if err != nil {
		os.Remove(path)
		lock.Release()
		return "", &Error{Code: CodeStoreError, Message: "opening " + path, Err: err}
	}
	log.appendFault = s.appendFault
	s.open = &openSession{
		id:    id,
		path:  path,
		lock:  lock,
		log:   log,
		tools: toolClasses(created.Tools),
	}
	return id, nil
}

// writeSessionFile writes a new session's file.
//
// The file is built in a temporary file and renamed into place. A
// failure at any step of the construction leaves neither the session's
// file nor the temporary file behind.
func (s *FileStore) writeSessionFile(ctx context.Context, path string, created *poo.SessionCreated) error {
	tmp := path + ".tmp"
	// A temporary file from a crashed construction is stale: the lock
	// says no one is building this session.
	os.Remove(tmp)

	log, err := Open(tmp)
	if err != nil {
		return &Error{Code: CodeStoreError, Message: "creating " + tmp, Err: err}
	}
	// The steps of the construction, in order. A test can fail any of
	// them, and the construction must leave nothing behind either way.
	cleanup := func() {
		log.Close()
		os.Remove(tmp)
		os.Remove(path)
	}
	if s.initFault == 1 {
		cleanup()
		return errInjected
	}
	if err := log.Append(ctx, created); err != nil {
		cleanup()
		return &Error{Code: CodeStoreError, Message: "writing " + tmp, Err: err}
	}
	if s.initFault == 2 {
		cleanup()
		return errInjected
	}
	if err := log.Sync(ctx); err != nil {
		cleanup()
		return &Error{Code: CodeStoreError, Message: "syncing " + tmp, Err: err}
	}
	if s.initFault == 3 {
		cleanup()
		return errInjected
	}
	if err := log.Close(); err != nil {
		os.Remove(tmp)
		os.Remove(path)
		return &Error{Code: CodeStoreError, Message: "closing " + tmp, Err: err}
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		os.Remove(path)
		return &Error{Code: CodeStoreError, Message: "renaming " + tmp, Err: err}
	}
	if s.initFault == 4 {
		os.Remove(tmp)
		os.Remove(path)
		return errInjected
	}
	if err := syncDir(s.dir); err != nil {
		os.Remove(tmp)
		os.Remove(path)
		return &Error{Code: CodeStoreError, Message: "syncing " + s.dir, Err: err}
	}
	return nil
}

// Load reads a session's records, recovers the session, and opens it.
//
// The load takes the authority on the session, so a session another
// process holds is refused with ErrSessionLocked, and a lock that was
// taken for a load that failed is given up before the failure is
// returned.
//
// The load recovers the session: a torn tail is truncated, an unsettled
// run is marked interrupted, an unfinished job is ended, and an input
// that never reached the model is dropped. The recovery is idempotent,
// so a load that failed partway is finished by the next one, without
// writing a record twice.
func (s *FileStore) Load(ctx context.Context, id string) (*Loaded, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.closeOpen(); err != nil {
		return nil, err
	}
	lock, err := Acquire(LockPath(s.dir, id))
	if err != nil {
		return nil, err
	}
	loaded, err := s.loadLocked(ctx, id, lock)
	if err != nil {
		lock.Release()
		return nil, err
	}
	return loaded, nil
}

// loadLocked loads a session with the authority already taken.
//
// The authority is the caller's: a failure gives it up.
func (s *FileStore) loadLocked(ctx context.Context, id string, lock *Authority) (*Loaded, error) {
	path := s.path(id)
	lines, torn, err := s.readLog(path)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, &Error{Code: CodeCorruptLog, Message: "the log holds no session"}
	}
	// The format is in the log's first line. A log in another format
	// is migrated before it is read.
	format, err := logFormat(lines[0])
	if err != nil {
		return nil, err
	}
	if format != s.codec.Version() {
		migrated, err := s.codec.Migrate(format, lines)
		if err != nil {
			return nil, err
		}
		lines = migrated
	}
	recs, err := s.decodeRecords(lines)
	if err != nil {
		return nil, err
	}
	created, ok := recs[0].(*poo.SessionCreated)
	if !ok {
		return nil, &Error{Code: CodeCorruptLog, Message: "the log does not start with a session"}
	}
	if err := CheckMode(Mode(created.Mode)); err != nil {
		return nil, err
	}
	meta, err := metadataOf(lines[0])
	if err != nil {
		return nil, err
	}

	log, err := openLog(path, int64(len(recs)))
	if err != nil {
		return nil, &Error{Code: CodeStoreError, Message: "opening " + path, Err: err}
	}
	log.appendFault = s.appendFault
	// The recovery appends to the log. A failure partway leaves the
	// log with what it wrote, and the next load finishes the job.
	if err := s.recover(ctx, log, pendingOf(id, recs)); err != nil {
		log.Close()
		return nil, err
	}
	// The records the session holds are the ones on disk, recovery
	// included.
	final, _, err := s.readLog(path)
	if err != nil {
		log.Close()
		return nil, err
	}
	recs, err = s.decodeRecords(final)
	if err != nil {
		log.Close()
		return nil, err
	}

	s.open = &openSession{
		id:    id,
		path:  path,
		lock:  lock,
		log:   log,
		tools: toolClasses(meta.Tools),
	}
	return &Loaded{
		ID:            id,
		Metadata:      meta,
		Records:       recs,
		RecoveredTail: torn,
		Requeue:       requeueOf(recs),
		Warnings:      s.warnings(meta),
	}, nil
}

// recover resolves a session's pending operations and puts them on disk.
//
// The recovery appends only what is missing, so it is idempotent: a
// recovery that failed partway is finished by the next load.
func (s *FileStore) recover(ctx context.Context, log *Log, pending *Pending) error {
	policy := s.policy
	if policy == nil {
		policy = InterruptPolicy{Now: s.now}
	}
	if err := policy.Resolve(ctx, log, pending); err != nil {
		return &Error{Code: CodeStoreError, Message: "recovering the session", Err: err}
	}
	if err := log.Sync(ctx); err != nil {
		return &Error{Code: CodeStoreError, Message: "syncing the session", Err: err}
	}
	return nil
}

// warnings reports what changed since the session was written.
//
// A different build or different settings are not failures: the records
// are still readable. A caller that cares reports them.
func (s *FileStore) warnings(meta Metadata) []string {
	var out []string
	if s.build.Name != "" && meta.Build.Name != "" && s.build != meta.Build {
		out = append(out, "the session was written by "+meta.Build.Name+" "+meta.Build.Version)
	}
	if s.settingsHash != "" && meta.SettingsHash != "" && s.settingsHash != meta.SettingsHash {
		out = append(out, "the session was written with other settings")
	}
	return out
}

// readLog reads a session's records.
//
// A tail that was cut short is truncated from the file: the bytes after
// the last newline are not a record, and a reader must not see them.
// The read reports that it truncated the file.
func (s *FileStore) readLog(path string) ([][]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, ErrSessionNotFound
		}
		return nil, false, &Error{Code: CodeStoreError, Message: "reading " + path, Err: err}
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	if data[len(data)-1] == '\n' {
		return bytesLines(data), false, nil
	}
	// The log was cut in the middle of a record. The bytes after the
	// last newline are not a record.
	whole := lastNewline(data) + 1
	if err := os.Truncate(path, int64(whole)); err != nil {
		return nil, false, &Error{Code: CodeStoreError, Message: "truncating " + path, Err: err}
	}
	return bytesLines(data[:whole]), true, nil
}

// decodeRecords decodes the lines of a log.
func (s *FileStore) decodeRecords(lines [][]byte) ([]poo.Record, error) {
	recs := make([]poo.Record, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		rec, err := s.codec.Decode(line)
		if err != nil {
			return nil, err
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

// Append writes one record to the open session.
//
// The record is written as one write of the line and its newline. A run's
// boundaries and a side-effecting tool's result are synced before the
// append returns: those are the records a crash must not lose.
func (s *FileStore) Append(ctx context.Context, rec poo.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return &Error{Code: CodeStoreError, Message: "the store has no open session"}
	}
	if err := s.open.log.Append(ctx, rec); err != nil {
		return &Error{Code: CodeStoreError, Message: "writing " + rec.Meta().Record, Err: err}
	}
	if s.needsSync(rec) {
		if err := s.open.log.Sync(ctx); err != nil {
			return &Error{Code: CodeStoreError, Message: "syncing " + rec.Meta().Record, Err: err}
		}
	}
	return nil
}

// Flush pushes the open session's records out.
//
// Every record is written as it comes, so there is nothing buffered.
func (s *FileStore) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return nil
	}
	if err := s.open.log.Flush(ctx); err != nil {
		return &Error{Code: CodeStoreError, Message: "flushing the session", Err: err}
	}
	return nil
}

// needsSync reports whether a record must be on disk before the session
// carries on.
//
// A run's boundaries and a side-effecting tool's result are the records
// a crash must not lose: the run's outcome, and the effect that
// happened.
func (s *FileStore) needsSync(rec poo.Record) bool {
	switch r := rec.(type) {
	case *poo.RunStarted, *poo.RunSettled:
		return true
	case *poo.ToolResultRecord:
		return s.open.tools[r.Tool] == poo.ClassSideEffecting
	}
	return false
}

// Close releases the store's resources: the open session's file and its
// lock.
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeOpen()
}

// closeOpen closes the open session, if there is one.
//
// The caller holds the lock.
func (s *FileStore) closeOpen() error {
	if s.open == nil {
		return nil
	}
	open := s.open
	s.open = nil
	logErr := open.log.Close()
	lockErr := open.lock.Release()
	if logErr != nil {
		return logErr
	}
	return lockErr
}

// Pending returns the operations a session has not resolved.
//
// The pending operations are what a recovery would resolve: a run that
// started and never settled, a tool call with no result, a permission
// ask with no answer, a job that started and never ended, an input that
// was queued and never reached the model.
//
// The read takes no authority: it is a question about the records, and
// the store's own session is answered from memory.
func (s *FileStore) Pending(ctx context.Context, id string) (*Pending, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.open != nil && s.open.id == id {
		return pendingOf(id, s.recordsOf(s.open)), nil
	}
	lines, _, err := s.readLog(s.path(id))
	if err != nil {
		return nil, err
	}
	recs, err := s.decodeRecords(lines)
	if err != nil {
		return nil, err
	}
	return pendingOf(id, recs), nil
}

// recordsOf returns the records of the open session.
//
// The records are the ones the store wrote, so they are the ones on
// disk.
func (s *FileStore) recordsOf(open *openSession) []poo.Record {
	lines, _, err := s.readLog(open.path)
	if err != nil {
		return nil
	}
	recs, err := s.decodeRecords(lines)
	if err != nil {
		return nil
	}
	return recs
}

// List returns the summaries of the sessions in the store.
//
// A summary is what a reader of the session directory wants: the
// session's id, when it was created, its profile and build, how many
// runs it had, why its last run settled, how big its file is, and
// whether another process holds it.
func (s *FileStore) List(ctx context.Context) ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

// listLocked lists the sessions in the store.
//
// The caller holds the lock.
func (s *FileStore) listLocked() ([]Summary, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, &Error{Code: CodeStoreError, Message: "reading " + s.dir, Err: err}
	}
	out := make([]Summary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		sum, err := s.summarize(id)
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				// The session was deleted while the directory was
				// being read.
				continue
			}
			return nil, err
		}
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// summarize computes the summary of one session.
func (s *FileStore) summarize(id string) (Summary, error) {
	path := s.path(id)
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Summary{}, ErrSessionNotFound
		}
		return Summary{}, &Error{Code: CodeStoreError, Message: "stating " + path, Err: err}
	}
	sum := Summary{ID: id, Size: info.Size()}
	scan, err := scanSummary(path)
	if err != nil {
		return Summary{}, err
	}
	sum.CreatedAt = scan.created
	sum.Mode = scan.mode
	sum.Profile = scan.profile
	sum.Build = scan.build
	sum.Runs = scan.runs
	sum.LastReason = scan.lastReason
	sum.LastActivity = scan.lastActivity
	locked, err := Locked(LockPath(s.dir, id))
	if err != nil {
		return Summary{}, err
	}
	sum.Locked = locked
	return sum, nil
}

// Prune deletes the sessions a policy marks.
//
// The prune takes each candidate's lock before it deletes it, so a
// session another process is using is skipped, and a session that is
// loaded while the prune runs is either loaded whole or is gone: the
// lock keeps the two apart.
//
// Nothing is deleted without a policy: a prune with neither an age nor
// a count fails with ErrNoPolicy, and the command line turns that into
// an exit code.
func (s *FileStore) Prune(ctx context.Context, policy Policy) (*PruneResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if policy.MaxAge <= 0 && policy.Keep <= 0 {
		return nil, ErrNoPolicy
	}
	now := policy.Now
	if now.IsZero() {
		now = s.now()
	}
	summaries, err := s.listLocked()
	if err != nil {
		return nil, err
	}
	// The count rule keeps the newest sessions, and it counts the
	// locked ones: a locked session is one of the sessions that are
	// kept.
	byActivity := make([]Summary, len(summaries))
	copy(byActivity, summaries)
	sort.Slice(byActivity, func(i, j int) bool {
		return byActivity[i].LastActivity.After(byActivity[j].LastActivity)
	})
	kept := map[string]bool{}
	var boundary time.Time
	if policy.Keep > 0 {
		for i, sum := range byActivity {
			if i >= policy.Keep {
				break
			}
			kept[sum.ID] = true
		}
		if policy.Keep <= len(byActivity) {
			boundary = byActivity[policy.Keep-1].LastActivity
		}
	}

	result := &PruneResult{}
	for _, sum := range byActivity {
		if !s.markedBy(sum, policy, now, kept, boundary) {
			continue
		}
		lock, err := Acquire(LockPath(s.dir, sum.ID))
		if err != nil {
			if errors.Is(err, ErrSessionLocked) {
				result.Skipped = append(result.Skipped, sum)
				continue
			}
			return nil, err
		}
		// The session's activity may have moved since the listing, so
		// the rules are checked again under the lock.
		fresh, err := s.summarize(sum.ID)
		if err != nil {
			lock.Release()
			if errors.Is(err, ErrSessionNotFound) {
				// The session was deleted while the prune ran.
				continue
			}
			return nil, err
		}
		// The prune holds the lock, so the session is not locked by
		// another process.
		fresh.Locked = false
		if !s.markedBy(fresh, policy, now, kept, boundary) {
			lock.Release()
			continue
		}
		if !policy.DryRun {
			if err := os.Remove(s.path(sum.ID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				lock.Release()
				return nil, &Error{Code: CodeStoreError, Message: "removing " + s.path(sum.ID), Err: err}
			}
		}
		lock.Release()
		result.Deleted = append(result.Deleted, fresh)
	}
	// A temporary file from a crashed construction is stale once it is
	// old enough, and it is removed under the session's lock.
	removed, err := s.removeStaleTmp(now)
	if err != nil {
		return nil, err
	}
	result.Removed = removed
	return result, nil
}

// markedBy reports whether a policy marks a session.
//
// With both rules set, a session is marked when either rule marks it.
func (s *FileStore) markedBy(sum Summary, policy Policy, now time.Time, kept map[string]bool, boundary time.Time) bool {
	if policy.MaxAge > 0 && now.Sub(sum.LastActivity) > policy.MaxAge {
		return true
	}
	if policy.Keep > 0 && !kept[sum.ID] {
		// A session that became newer than the sessions that are kept
		// is kept too: the count rule is about the newest sessions.
		if boundary.IsZero() || !sum.LastActivity.After(boundary) {
			return true
		}
	}
	return false
}

// removeStaleTmp removes the temporary files of constructions that were
// cut short.
//
// A temporary file is stale when it is older than an hour: a build that
// is running holds its session's lock, and the removal takes that lock.
func (s *FileStore) removeStaleTmp(now time.Time) ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, &Error{Code: CodeStoreError, Message: "reading " + s.dir, Err: err}
	}
	var removed []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl.tmp") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) < StaleTmpAge {
			continue
		}
		id := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".jsonl.tmp"), ".tmp")
		lock, err := Acquire(LockPath(s.dir, id))
		if err != nil {
			if errors.Is(err, ErrSessionLocked) {
				continue
			}
			return nil, err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			lock.Release()
			return nil, &Error{Code: CodeStoreError, Message: "removing " + path, Err: err}
		}
		lock.Release()
		removed = append(removed, path)
	}
	return removed, nil
}

// toolClasses returns the tool classes of a session, by name.
func toolClasses(tools []poo.SessionTool) map[string]poo.ToolClass {
	out := make(map[string]poo.ToolClass, len(tools))
	for _, tool := range tools {
		out[tool.Name] = tool.Class
	}
	return out
}

// openLog opens a session's file for appending, continuing its
// numbering at the given sequence number.
func openLog(path string, seq int64) (*Log, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Log{f: f, path: path, seq: seq}, nil
}

// syncDir puts a directory's entries on disk.
//
// A rename is only durable when the directory that holds it is synced.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// lastNewline returns the offset of the last newline in some bytes, or
// -1 when there is none.
func lastNewline(data []byte) int {
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == '\n' {
			return i
		}
	}
	return -1
}

// logFormat returns the format a log's first line names.
func logFormat(line []byte) (string, error) {
	var envelope struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return "", &Error{Code: CodeCorruptLog, Message: "the log does not start with a record", Err: err}
	}
	return envelope.Format, nil
}

// scanSummary reads the summary of a session's file.
//
// The scan reads what a summary needs and no more: a summary is about
// the session's shape, not about its messages. A log with a torn tail
// is read as far as it goes.
func scanSummary(path string) (*scanned, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &Error{Code: CodeStoreError, Message: "reading " + path, Err: err}
	}
	defer f.Close()
	out := &scanned{}
	dec := json.NewDecoder(bufio.NewReader(f))
	for {
		var line summaryLine
		if err := dec.Decode(&line); err != nil {
			// A log that was cut short ends the scan: the rest of the
			// log is not a summary.
			break
		}
		out.add(line)
	}
	return out, nil
}

// errInjected is the failure a test injects.
var errInjected = errors.New("session: the injected failure")
