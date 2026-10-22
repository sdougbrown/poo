package poo

import (
	"errors"
	"fmt"
	"strings"
)

// The restore: a session rebuilt from the records of an earlier one.
//
// The records hold everything a session needs: the messages the model
// saw, the messages the hooks appended, the steering, the compactions
// and each run's rendered prompt. A restore reads them back, and it
// never asks the model or a hook anything: the records already hold
// what the hooks said and what the prompt was.

// The wire's codes for a session that does not fit this binary.
const (
	// CodeUnknownProfile names a profile this build does not offer.
	CodeUnknownProfile = "unknown_profile"
	// CodeUnknownModel names a model the current settings do not
	// offer.
	CodeUnknownModel = "unknown_model"
	// CodeSessionIncompatible names a session whose tools do not fit
	// this build.
	CodeSessionIncompatible = "session_incompatible"
	// CodeUnsupportedMode names a persistence mode this build does not
	// support.
	CodeUnsupportedMode = "unsupported_mode"
	// CodeUnansweredCall names a session whose history holds a tool call
	// the model would see without an answer.
	CodeUnansweredCall = "unanswered_call"
)

// The errors a restore returns.
var (
	// ErrUnknownProfile means the session was made with a profile this
	// build does not offer.
	ErrUnknownProfile = errors.New("poo: the session's profile is unknown")
	// ErrUnknownModel means the session was made with a model the
	// current settings do not offer.
	ErrUnknownModel = errors.New("poo: the session's model is unknown")
	// ErrSessionIncompatible means the session's tools do not fit this
	// build.
	ErrSessionIncompatible = errors.New("poo: the session does not fit this build")
	// ErrNoSessionHeader means the records hold no session header, so
	// there is no session to restore.
	ErrNoSessionHeader = errors.New("poo: the records hold no session header")
	// ErrCodecVersion means the records are in another format version.
	ErrCodecVersion = errors.New("poo: the records are in another format version")
	// ErrUnsupportedMode means the session's persistence mode is not
	// supported by this build.
	ErrUnsupportedMode = errors.New("poo: the session's persistence mode is not supported")
	// ErrUnansweredCall means the records hold a tool call the model saw
	// with no result, so the next request would be one the provider
	// rejects.
	ErrUnansweredCall = errors.New("poo: the session holds a tool call with no result")
)

// IncompatibleError is the error a failed compatibility check returns.
//
// The code is the wire's code, so a client can tell an unknown profile
// from an unknown model from a session that does not fit, and the
// differences say what does not fit.
type IncompatibleError struct {
	// Code is the wire's code.
	Code string `json:"code"`
	// Message says what is wrong.
	Message string `json:"message"`
	// Differences are the differences the check found.
	Differences []string `json:"differences,omitempty"`
}

// Error returns the message and the differences.
func (e *IncompatibleError) Error() string {
	if len(e.Differences) == 0 {
		return e.Message
	}
	return e.Message + ": " + strings.Join(e.Differences, "; ")
}

// Is reports whether the error is the sentinel of its code.
//
// A caller that wants to tell the codes apart uses errors.Is with the
// sentinels, and a client that wants the code reads the field.
func (e *IncompatibleError) Is(target error) bool {
	switch target {
	case ErrUnknownProfile:
		return e.Code == CodeUnknownProfile
	case ErrUnknownModel:
		return e.Code == CodeUnknownModel
	case ErrSessionIncompatible:
		return e.Code == CodeSessionIncompatible
	}
	return false
}

// CodecError is the error a record format this build cannot read
// returns.
type CodecError struct {
	// Format is the version the records are in.
	Format string `json:"format"`
	// Expected is the version this build reads.
	Expected string `json:"expected"`
}

// Error returns the message.
func (e *CodecError) Error() string {
	return fmt.Sprintf("poo: the records are in the format %q, and this build reads %q", e.Format, e.Expected)
}

// Is reports that the error is a codec mismatch.
func (e *CodecError) Is(target error) bool { return target == ErrCodecVersion }

// UnsupportedModeError is the error a session in a mode this build does
// not support returns.
type UnsupportedModeError struct {
	// Mode is the unsupported mode.
	Mode string `json:"mode"`
}

// Error returns the message.
func (e *UnsupportedModeError) Error() string {
	return fmt.Sprintf("poo: the session's persistence mode %q is not supported", e.Mode)
}

// Is reports that the error is the unsupported-mode sentinel.
func (e *UnsupportedModeError) Is(target error) bool { return target == ErrUnsupportedMode }

// UnansweredCallError is the error a session with a tool call the model
// would see unanswered returns.
type UnansweredCallError struct {
	// CallID is the provider's id of the unanswered call.
	CallID string `json:"call_id"`
}

// Error returns the message.
func (e *UnansweredCallError) Error() string {
	return fmt.Sprintf("poo: the session holds the tool call %q with no result", e.CallID)
}

// Is reports that the error is the unanswered-call sentinel.
func (e *UnansweredCallError) Is(target error) bool { return target == ErrUnansweredCall }

// Restored is what a restore produced.
type Restored struct {
	// Metadata is the session's metadata, as the records hold it.
	Metadata SessionMetadata `json:"metadata"`
	// Warnings are the differences the check found and allowed: a
	// changed settings hash or a changed build.
	Warnings []string `json:"warnings,omitempty"`
	// Runs are the runs the records hold, in order.
	Runs []string `json:"runs,omitempty"`
	// Interrupted are the runs that were interrupted.
	Interrupted []string `json:"interrupted,omitempty"`
	// Jobs are the job results that were queued again.
	Jobs []JobResult `json:"jobs,omitempty"`
	// Messages is how many messages the restored history holds.
	Messages int `json:"messages"`
	// Turns is how many turns the restored history holds.
	Turns int `json:"turns"`
	// Compactions is how many recorded compactions were applied.
	Compactions int `json:"compactions"`
}

// Restore rebuilds a session from the records of an earlier one.
//
// The history comes from the records: the messages, the hook-appended
// messages, the steering, the compactions and each run's recorded
// system prompt. No hook runs and no prompt is rendered again, so a
// session that was recorded under another prompt keeps that prompt.
//
// The job results that were never delivered are queued again, so they
// follow the session's idle policy like any other result.
//
// The session takes new runs when the restore returns.
func Restore(opts Options, records []Record) (*Session, Restored, error) {
	created := sessionHeader(records)
	if created == nil {
		return nil, Restored{}, ErrNoSessionHeader
	}
	if created.Format != "" && created.Format != FormatVersion {
		return nil, Restored{}, &CodecError{Format: created.Format, Expected: FormatVersion}
	}
	if created.Mode == "durable" {
		return nil, Restored{}, &UnsupportedModeError{Mode: created.Mode}
	}
	restored := Restored{Metadata: created.Metadata()}

	// The prompt is the recorded one. It is never rendered from the
	// sections again, so a change in the prompt's sections cannot reach
	// a session that was recorded under another one.
	if text, ok := recordedPrompt(records); ok {
		opts.SystemPrompt = text
	}
	if opts.SessionID == "" {
		opts.SessionID = restored.Metadata.SessionID
	}
	if opts.WorkingDir == "" {
		opts.WorkingDir = restored.Metadata.WorkingDir
	}
	// The profile is the recorded one: the effective profile comes from
	// the metadata, not from the current default.
	if restored.Metadata.Profile != "" {
		opts.Profile = restored.Metadata.Profile
	}
	// The run's shape is the recorded one: the tools mode and the
	// reasoning effort are part of what the session sent, so the
	// restored session sends them again.
	opts.ToolsMode = created.ToolsMode
	opts.ReasoningEffort = created.ReasoningEffort
	// The chat role is the recorded one: the effective role comes from
	// the metadata, not from the current defaults.
	if role := restored.Metadata.Roles.Chat; role != nil {
		model := *role
		if model.ContextLimit <= 0 {
			model.ContextLimit = opts.Model.ContextLimit
		}
		if model.ContextLimit <= 0 {
			model.ContextLimit = created.ContextLimit
		}
		opts.Model = model
	}
	// The request's shape is the recorded one too: the role's model may
	// carry it, and the header's own fields fill it when it does not.
	if created.ToolsMode != "" {
		opts.Model.Tools.Mode = ToolsMode(created.ToolsMode)
	}
	if created.ReasoningEffort != "" {
		opts.Model.Reasoning = ReasoningControl{Mode: ReasoningEffort, Param: ReasoningPathEffort}
	}
	// The session-start hooks do not run: the records hold the messages
	// they appended, and running them again would say it twice.
	opts.SessionStart = nil

	s, err := New(opts)
	if err != nil {
		return nil, Restored{}, err
	}
	// The check needs the session's own tools: the core registers its
	// job tools when the session is built.
	warnings, err := checkCompatibility(s.opts, restored.Metadata)
	if err != nil {
		s.Close()
		return nil, Restored{}, err
	}
	restored.Warnings = warnings
	if err := s.rebuild(records, &restored); err != nil {
		s.Close()
		return nil, Restored{}, err
	}
	if callID := s.unansweredCall(); callID != "" {
		s.Close()
		return nil, Restored{}, &UnansweredCallError{CallID: callID}
	}
	// The results that were never delivered are queued again, in
	// completion order, and the idle policy acts on them.
	for _, job := range pendingJobResults(records) {
		s.queueJobResult(job)
		restored.Jobs = append(restored.Jobs, publicJobResult(job))
	}
	return s, restored, nil
}

// sessionHeader returns the session's header record, or nil when the
// records hold none.
func sessionHeader(records []Record) *SessionCreated {
	for _, rec := range records {
		if created, ok := rec.(*SessionCreated); ok {
			return created
		}
	}
	return nil
}

// recordedPrompt returns the last system prompt the records hold.
//
// The prompt is the one the last run carried: a session continues with
// the prompt it was running under.
func recordedPrompt(records []Record) (string, bool) {
	text := ""
	found := false
	for _, rec := range records {
		prompt, ok := rec.(*SystemPromptRecord)
		if !ok {
			continue
		}
		text = prompt.Text
		found = true
	}
	return text, found
}

// checkCompatibility checks the session's metadata against this build
// and the current settings.
//
// The profile must exist, every role must name a model the settings
// offer, and every recorded tool must still be linked with the same
// class and background mode. A different settings hash or build is
// allowed, and reported as a warning.
func checkCompatibility(opts Options, meta SessionMetadata) ([]string, error) {
	if meta.Profile != "" && !hasProfile(opts.Profiles, meta.Profile) {
		return nil, &IncompatibleError{
			Code: CodeUnknownProfile,
			Message: fmt.Sprintf("poo: the session was made with the profile %q, and this build does not offer it",
				meta.Profile),
		}
	}
	if opts.Models != nil {
		for _, role := range meta.Roles.Models() {
			name := modelLabel(*role)
			if _, ok := opts.Models[name]; !ok {
				return nil, &IncompatibleError{
					Code:    CodeUnknownModel,
					Message: fmt.Sprintf("poo: the session's model %q is not in this build's settings", name),
				}
			}
		}
	}
	if diffs := toolDifferences(meta.Tools, opts.Tools); len(diffs) > 0 {
		return nil, &IncompatibleError{
			Code:        CodeSessionIncompatible,
			Message:     "poo: the session does not fit this build",
			Differences: diffs,
		}
	}
	var warnings []string
	if meta.SettingsHash != "" && opts.SettingsHash != "" && meta.SettingsHash != opts.SettingsHash {
		warnings = append(warnings, "the settings changed since the session was created")
	}
	if meta.Build.Name != "" && opts.Build.Name != "" && meta.Build != opts.Build {
		warnings = append(warnings, fmt.Sprintf("the session was made by %s %s, and this build is %s %s",
			meta.Build.Name, meta.Build.Version, opts.Build.Name, opts.Build.Version))
	}
	return warnings, nil
}

// hasProfile reports whether the profiles hold a name.
func hasProfile(profiles []Profile, name string) bool {
	for _, p := range profiles {
		if p.Name == name {
			return true
		}
	}
	return false
}

// toolDifferences returns the differences between the recorded tools and
// the tools this build links.
//
// A tool that is gone, that changed its class or that changed its
// background mode is a difference: the session was made under a tool
// set this build does not have.
func toolDifferences(recorded []ToolInfo, current map[string]Tool) []string {
	var diffs []string
	for _, info := range recorded {
		tool, ok := current[info.Name]
		if !ok || tool == nil {
			diffs = append(diffs, fmt.Sprintf("the tool %q is no longer linked", info.Name))
			continue
		}
		spec := tool.Spec()
		if spec.Class != info.Class {
			diffs = append(diffs, fmt.Sprintf("the tool %q changed from %q to %q", info.Name, info.Class, spec.Class))
		}
		// A record written before the header carried a background mode
		// says nothing about it, so it is not a difference.
		if info.Background != "" && spec.Background != info.Background {
			diffs = append(diffs, fmt.Sprintf("the tool %q changed its background mode from %q to %q",
				info.Name, info.Background, spec.Background))
		}
	}
	return diffs
}

// PendingJobResults returns the job results the records hold that were
// never delivered.
//
// A result that was delivered is in the model's history already. A
// result that was not is queued again, in completion order, so it
// follows the session's idle policy.
func PendingJobResults(records []Record) []JobResult {
	out := make([]JobResult, 0, len(records))
	for _, job := range pendingJobResults(records) {
		out = append(out, publicJobResult(job))
	}
	return out
}

// pendingJobResults returns the job results that were never delivered,
// in completion order.
func pendingJobResults(records []Record) []jobResult {
	delivered := map[string]bool{}
	for _, rec := range records {
		if d, ok := rec.(*InputDelivered); ok {
			delivered[d.InputID] = true
		}
	}
	var out []jobResult
	for _, rec := range records {
		ended, ok := rec.(*JobEnded)
		if !ok {
			continue
		}
		if delivered[ended.InputID] {
			continue
		}
		out = append(out, jobResult{
			id:      ended.InputID,
			job:     ended.Job,
			handle:  ended.Handle,
			tool:    ended.Tool,
			runID:   ended.RunID,
			outcome: ended.Outcome,
			text:    ended.Text,
			fatal:   ended.Fatal,
		})
	}
	return out
}

// publicJobResult turns an internal result into the public one.
func publicJobResult(job jobResult) JobResult {
	return JobResult{
		Job:     job.job,
		Handle:  job.handle,
		Tool:    job.tool,
		Text:    job.text,
		Outcome: jobOutcomeToCallOutcome(job.outcome),
		Fatal:   job.fatal,
	}
}

// turnKey names one turn of one run.
type turnKey struct {
	// run is the run's identifier.
	run string
	// turn is the turn's number in its run.
	turn int
}

// rebuild rebuilds the session's state from the records.
//
// The history comes from the records in order, and the compactions are
// applied as they were recorded: the strategy that made one never runs
// again.
func (s *Session) rebuild(records []Record, out *Restored) error {
	s.seedCounters(records)
	runs, runTurns := scanRuns(records, out)
	// The session's turn numbers are the ones the records were written
	// with: the runs in order, and each run's turns in order.
	offset := 0
	sessionTurn := map[turnKey]int{}
	for _, id := range runs {
		for t := 1; t <= runTurns[id]; t++ {
			sessionTurn[turnKey{run: id, turn: t}] = offset + t
		}
		offset += runTurns[id]
	}
	s.mu.Lock()
	s.turnSeq = offset
	s.turns = make([]TurnSummary, 0, offset)
	s.spans = make([]turnSpan, 0, offset)
	for _, id := range runs {
		for t := 1; t <= runTurns[id]; t++ {
			s.turns = append(s.turns, TurnSummary{Index: t, Session: offset - runTurns[id] + t})
			s.spans = append(s.spans, turnSpan{turn: offset - runTurns[id] + t, run: t})
		}
	}
	s.mu.Unlock()
	out.Turns = offset

	// The turn that is being filled: the session's number and the run's
	// number, and the messages the turn holds.
	placed := 0
	for _, rec := range records {
		switch r := rec.(type) {
		case *MessageRecord:
			msg := Message{
				Role:         r.Role,
				Content:      r.Content,
				ID:           r.MessageID,
				Origin:       r.Origin,
				DeliveryNote: r.DeliveryNote,
				Partial:      r.Partial,
				Cause:        r.Cause,
			}
			s.appendMessages(msg)
			s.recordMessage(msg)
			if r.Turn > 0 {
				placed = s.placeTurn(placed, sessionTurn, r.RunID, r.Turn)
			}
			s.rememberMessageResult(r)
		case *ToolCallRecord:
			s.rememberCall(r, sessionTurn)
		case *ToolResultRecord:
			s.rememberRecordedResult(r, sessionTurn)
		case *CompactionRecord:
			if err := s.applyRecordedCompaction(r.Compaction); err != nil {
				return fmt.Errorf("poo: restoring the compaction of the run %s: %w", r.RunID, err)
			}
			out.Compactions++
		case *RunSettled:
			if r.Reason == ReasonInterrupted {
				out.Interrupted = append(out.Interrupted, r.RunID)
			}
		}
	}
	// The turns that produced no message stand where the view stands
	// when the records end.
	s.placeRemainingTurns(placed)
	out.Messages = len(s.history)
	return nil
}

// seedCounters continues the session's counters past the ones the
// records hold.
//
// A handle, a message id and a job result's input id each name one
// thing, and they never repeat within a session. A restored session
// carries on after the ones the records already used, so its own calls
// and results are never confused with the earlier ones.
func (s *Session) seedCounters(records []Record) {
	handles := 0
	messages := 0
	inputs := 0
	jobs := 0
	for _, rec := range records {
		switch r := rec.(type) {
		case *ToolCallRecord:
			handles = max(handles, handleSeq(r.Handle))
		case *ToolResultRecord:
			handles = max(handles, handleSeq(r.Handle))
		case *MessageRecord:
			messages = max(messages, messageSeq(r.MessageID))
		case *JobStarted:
			jobs = max(jobs, seqOf(r.Job, 'j'))
		case *JobEnded:
			jobs = max(jobs, seqOf(r.Job, 'j'))
			inputs = max(inputs, jobInputSeq(s.opts.SessionID, r.InputID))
		}
	}
	s.handleSeq = handles
	s.inputSeq = inputs
	s.jobSeq = jobs
	s.messageIDs.Set(messages)
}

// scanRuns returns the runs the records hold, in the order they
// started, and the number of turns each of them made.
//
// The turn count comes from the settle record, so the session's turn
// numbers are the ones the records were written with.
func scanRuns(records []Record, out *Restored) ([]string, map[string]int) {
	var order []string
	seen := map[string]bool{}
	turns := map[string]int{}
	note := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		order = append(order, id)
	}
	for _, rec := range records {
		switch r := rec.(type) {
		case *RunStarted:
			note(r.RunID)
			out.Runs = append(out.Runs, r.RunID)
		case *RunSettled:
			note(r.RunID)
			if r.Turns > turns[r.RunID] {
				turns[r.RunID] = r.Turns
			}
		case *MessageRecord:
			if r.Turn > turns[r.RunID] {
				turns[r.RunID] = r.Turn
			}
		case *Usage:
			if r.Turn > turns[r.RunID] {
				turns[r.RunID] = r.Turn
			}
		case *ToolCallRecord:
			if r.Turn > turns[r.RunID] {
				turns[r.RunID] = r.Turn
			}
		case *ToolResultRecord:
			if r.Turn > turns[r.RunID] {
				turns[r.RunID] = r.Turn
			}
		}
	}
	return order, turns
}

// placeTurn places the turn a message belongs to, and reports how many
// turns are placed.
//
// The turns are placed in the order the records hold them, and a turn
// that produced no message is placed where the next turn starts: it
// holds nothing, and its place is where the view stood.
func (s *Session) placeTurn(placed int, sessionTurn map[turnKey]int, run string, turn int) int {
	st, ok := sessionTurn[turnKey{run: run, turn: turn}]
	if !ok || st < 1 || st > len(s.spans) {
		return placed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// The turns between the last placed one and this one produced no
	// message: they hold nothing, and they stand where this turn
	// starts.
	for t := placed + 1; t < st; t++ {
		s.spans[t-1].start = len(s.history) - 1
		s.spans[t-1].end = len(s.history) - 1
	}
	span := &s.spans[st-1]
	if st > placed {
		// The turn's first message: the span starts where it stands.
		span.start = len(s.history) - 1
		placed = st
	}
	span.end = len(s.history)
	return placed
}

// placeRemainingTurns places the turns that produced no message at the
// end of the view.
func (s *Session) placeRemainingTurns(placed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for t := placed + 1; t <= len(s.spans); t++ {
		s.spans[t-1].start = len(s.history)
		s.spans[t-1].end = len(s.history)
	}
}

// rememberCall remembers a call the records hold.
func (s *Session) rememberCall(r *ToolCallRecord, sessionTurn map[turnKey]int) {
	st := sessionTurn[turnKey{run: r.RunID, turn: r.Turn}]
	if st < 1 || st > len(s.turns) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	summary := s.turns[st-1]
	for _, call := range summary.Calls {
		if call.Handle == r.Handle {
			return
		}
	}
	calls := append(append([]ToolCallSummary(nil), summary.Calls...), ToolCallSummary{Handle: r.Handle, Tool: r.Tool})
	summary.Calls = calls
	s.turns[st-1] = summary
}

// rememberRecordedResult remembers a result the records hold.
//
// The result is the model-facing one: the record holds the text the
// model read, and a redacted result holds nothing but what the redaction
// left.
func (s *Session) rememberRecordedResult(r *ToolResultRecord, sessionTurn map[turnKey]int) {
	st := sessionTurn[turnKey{run: r.RunID, turn: r.Turn}]
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[r.Handle] = ToolResult{Text: r.Text, Structured: r.Structured}
	s.resultMeta[r.Handle] = resultMeta{tool: r.Tool, turn: st}
	if st < 1 || st > len(s.turns) {
		return
	}
	summary := s.turns[st-1]
	for i := range summary.Calls {
		if summary.Calls[i].Handle == r.Handle {
			summary.Calls[i].Outcome = r.Outcome
			s.turns[st-1] = summary
			return
		}
	}
}

// rememberMessageResult remembers what a message says about a result.
//
// A tool result's message carries the call's id and whether the result
// was an error: the session needs both to find the result in the view.
func (s *Session) rememberMessageResult(r *MessageRecord) {
	for _, block := range r.Content {
		if block.Kind != ContentToolResult {
			continue
		}
		handle := handleInText(block.Text)
		if handle == "" {
			continue
		}
		s.mu.Lock()
		if meta, ok := s.resultMeta[handle]; ok {
			meta.callID = block.ToolCallID
			s.resultMeta[handle] = meta
			result, ok := s.results[handle]
			if ok {
				result.IsError = block.IsError
				s.results[handle] = result
			}
		}
		s.mu.Unlock()
	}
}

// handleInText returns the handle a model-facing result text names.
//
// The text starts with the handle line, so the handle is the first word
// between the brackets.
func handleInText(text string) string {
	if !strings.HasPrefix(text, "[") {
		return ""
	}
	end := strings.Index(text, "]")
	if end < 0 {
		return ""
	}
	line := text[1:end]
	sep := strings.Index(line, " ·")
	if sep < 0 {
		return ""
	}
	return line[:sep]
}

// unansweredCall returns the first tool call in the rebuilt view that
// has no result, or the empty string when every call is paired.
//
// A request carries the view, so an unanswered call is a request the
// provider rejects. The restore refuses it rather than handing back a
// session whose first request fails.
func (s *Session) unansweredCall() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var calls []string
	results := map[string]bool{}
	for _, msg := range s.history {
		for _, block := range msg.Content {
			switch block.Kind {
			case ContentToolCall:
				calls = append(calls, block.ToolCallID)
			case ContentToolResult:
				results[block.ToolCallID] = true
			}
		}
	}
	for _, id := range calls {
		if !results[id] {
			return id
		}
	}
	return ""
}

// applyRecordedCompaction applies a compaction the records hold.
//
// The compaction is applied as it was recorded: the strategy that made
// it never runs again.
func (s *Session) applyRecordedCompaction(c Compaction) error {
	if err := s.validateCompaction(c); err != nil {
		return err
	}
	view, err := s.buildCompactionView(c)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = view.history
	s.spans = view.spans
	for handle, result := range view.results {
		s.results[handle] = result
	}
	s.est.rebase(view.history)
	return nil
}
