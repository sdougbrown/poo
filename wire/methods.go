package wire

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/wire/protocol"
)

// DefaultInspectTimeout is how long an inspect waits when the client
// names no timeout.
const DefaultInspectTimeout = 30 * time.Second

// dispatch runs the method a request names.
//
// Every method decodes its params strictly: a params object that holds
// a field the method does not know is refused with unknown_field, and
// a params object that is null is refused with invalid_params.
func (s *server) dispatch(req protocol.Request) (any, *protocol.Error) {
	switch req.Method {
	case protocol.MethodSessionCreate:
		return s.sessionCreate(req.Params)
	case protocol.MethodSessionLoad:
		return s.sessionLoad(req.Params)
	case protocol.MethodSessionGet:
		return s.sessionGet(req.Params)
	case protocol.MethodSessionConfigure:
		return s.sessionConfigure(req.Params)
	case protocol.MethodSessionClose:
		return s.sessionClose(req.Params)
	case protocol.MethodSessionHistory:
		return s.sessionHistory(req.Params)
	case protocol.MethodModelsList:
		return s.modelsList(req.Params)
	case protocol.MethodAgentPrompt:
		return s.agentPrompt(req.Params)
	case protocol.MethodAgentMessage:
		return s.agentMessage(req.Params)
	case protocol.MethodAgentInspect:
		return s.agentInspect(req.Params)
	case protocol.MethodAgentCancel:
		return s.agentCancel(req.Params)
	case protocol.MethodJobCancel:
		return s.jobCancel(req.Params)
	case protocol.MethodConfigGet:
		return s.configGet(req.Params)
	}
	return nil, protocol.Errorf(protocol.CodeMethodNotFound, protocol.DataMethodNotFound,
		"there is no method %q", req.Method)
}

// initialize is the handshake.
//
// The result tells the client what this binary can do, which matters
// because custom binaries differ.
func (s *server) initialize(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.InitializeParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	version, ok := negotiate(params.ProtocolVersions)
	if !ok {
		return nil, protocol.Errorf(protocol.CodeVersionUnsupported, protocol.DataProtocolVersionUnsupported,
			"the client speaks %v and this server speaks %d", params.ProtocolVersions, protocol.Version)
	}
	s.mu.Lock()
	s.initialized = true
	s.clientInfo = params.Client
	s.mu.Unlock()
	return s.initializeResult(version), nil
}

// negotiate picks the highest version the client and the server share.
//
// The server speaks one version in v1, and the client may name any
// number of them.
func negotiate(versions []int) (int, bool) {
	best := 0
	for _, version := range versions {
		if version == protocol.Version && version > best {
			best = version
		}
	}
	return best, best > 0
}

// sessionCreate starts a session.
func (s *server) sessionCreate(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.CreateParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	if params.WorkingDir == "" {
		return nil, invalidParams("the working directory is required")
	}
	// The modes this process can serve are the ones the handshake
	// announced. A mode outside them is refused.
	modes := s.factory.Capabilities().Persistence
	if params.Mode != "" && !contains(modes, params.Mode) {
		return nil, protocol.Errorf(protocol.CodeServerError, protocol.DataUnsupportedMode,
			"this process does not serve the %q mode", params.Mode)
	}
	obs := &observer{srv: s}
	sess, info, err := s.factory.Create(context.Background(), params, Env{Observer: obs, Client: s.client})
	if err != nil {
		return nil, failure(err)
	}
	if sess == nil || info == nil {
		return nil, protocol.Errorf(protocol.CodeInternalError, protocol.DataInternalError,
			"the factory built no session")
	}
	entry := newSession(sess, info)
	obs.attach(entry)
	s.register(entry)
	s.sessionCreated(entry)
	return sessionInfoOf(entry), nil
}

// sessionLoad opens a recorded session.
func (s *server) sessionLoad(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.LoadParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	if params.SessionID == "" {
		return nil, invalidParams("the session id is required")
	}
	obs := &observer{srv: s}
	sess, info, err := s.factory.Load(context.Background(), params, Env{Observer: obs, Client: s.client})
	if err != nil {
		return nil, failure(err)
	}
	if sess == nil || info == nil {
		return nil, protocol.Errorf(protocol.CodeInternalError, protocol.DataInternalError,
			"the factory loaded no session")
	}
	entry := newSession(sess, info)
	obs.attach(entry)
	s.register(entry)
	return sessionInfoOf(entry), nil
}

// sessionInfoOf returns what the client learns about a session.
func sessionInfoOf(entry *session) protocol.SessionInfo {
	info := protocol.SessionInfo{
		SessionID: entry.id,
		Mode:      entry.info.Mode,
		Tools:     entry.info.Tools,
		Models:    entry.info.Models,
	}
	if entry.info.SessionDir != "" {
		info.SessionDir = entry.info.SessionDir
	}
	if entry.info.RecoveredTail {
		info.RecoveredTail = true
	}
	if len(entry.info.InterruptedRuns) > 0 {
		info.InterruptedRuns = entry.info.InterruptedRuns
	}
	if len(entry.info.Warnings) > 0 {
		info.Warnings = entry.info.Warnings
	}
	return info
}

// sessionGet reports a session's state.
func (s *server) sessionGet(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.GetParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	entry, rerr := s.lookup(params.SessionID)
	if rerr != nil {
		return nil, rerr
	}
	result := protocol.GetResult{
		State:  string(entry.sess.State()),
		Jobs:   jobsOf(entry.sess),
		Prompt: promptOf(entry.sess),
		Broken: entry.sess.Broken(),
	}
	if runID := entry.sess.ActiveRunID(); runID != "" {
		result.RunID = runID
	}
	return s.getResult(result), nil
}

// sessionConfigure changes a session between its runs.
func (s *server) sessionConfigure(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.ConfigureParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	entry, rerr := s.lookup(params.SessionID)
	if rerr != nil {
		return nil, rerr
	}
	// The change takes effect at the next run: a session that is
	// running refuses it.
	if entry.sess.Running() {
		return nil, protocol.Errorf(protocol.CodeServerError, protocol.DataBusy,
			"the session %q is running", entry.id)
	}
	models, err := s.factory.Configure(context.Background(), entry.sess, params)
	if err != nil {
		return nil, failure(err)
	}
	return protocol.ConfigureResult{Models: models}, nil
}

// sessionClose closes a session.
func (s *server) sessionClose(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.CloseParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	entry, rerr := s.lookup(params.SessionID)
	if rerr != nil {
		return nil, rerr
	}
	if err := s.closeSession(entry, sourceCancelClose); err != nil {
		return nil, protocol.Errorf(protocol.CodeServerError, protocol.DataStoreError,
			"the close failed: %v", err)
	}
	return map[string]any{}, nil
}

// sessionHistory pages a session's scrollback.
//
// The page comes back oldest first, and the paging goes backwards:
// has_more says that older items exist before the first returned one.
func (s *server) sessionHistory(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.HistoryParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	entry, rerr := s.lookup(params.SessionID)
	if rerr != nil {
		return nil, rerr
	}
	if params.Limit < 0 {
		return nil, invalidParams("the limit must not be negative")
	}
	if params.Limit > poo.MaxHistoryLimit {
		params.Limit = poo.MaxHistoryLimit
	}
	var records []poo.Record
	if entry.info != nil && entry.info.Records != nil {
		records = entry.info.Records()
	}
	page := poo.ProjectHistory(records, params.BeforeSeq, params.Limit)
	items := make([]protocol.HistoryItem, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, protocol.HistoryItem{
			Seq:      item.Seq,
			Type:     item.Type,
			RunID:    item.RunID,
			Turn:     item.Turn,
			Role:     string(item.Role),
			Text:     item.Text,
			Handle:   item.Handle,
			Tool:     item.Tool,
			Outcome:  string(item.Outcome),
			Decision: item.Decision,
			Reason:   string(item.Reason),
			At:       item.At,
		})
	}
	return protocol.HistoryResult{Items: items, HasMore: page.HasMore}, nil
}

// modelsList returns the effective model catalog.
func (s *server) modelsList(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.ModelsParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	models, err := s.factory.Models(context.Background(), params.Refresh)
	if err != nil {
		return nil, failure(err)
	}
	if models == nil {
		models = []protocol.ModelInfo{}
	}
	return protocol.ModelsResult{Models: models}, nil
}

// configGet returns the effective settings.
func (s *server) configGet(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.ConfigParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	settings, err := s.factory.Config(context.Background())
	if err != nil {
		return nil, failure(err)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, nil
}

// agentPrompt starts a run.
//
// The call returns at once: the run goes on in the background, and the
// client watches it through the events and agent/inspect.
func (s *server) agentPrompt(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.PromptParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	entry, rerr := s.lookup(params.SessionID)
	if rerr != nil {
		return nil, rerr
	}
	if params.Text == "" {
		return nil, invalidParams("the prompt text is required")
	}
	opts, rerr := inputOptions(params.Delivered)
	if rerr != nil {
		return nil, rerr
	}
	runID, err := entry.sess.Prompt(context.Background(), params.Text, opts...)
	if err != nil {
		return nil, failure(err)
	}
	return protocol.PromptResult{RunID: runID}, nil
}

// agentMessage sends a message to the active run.
func (s *server) agentMessage(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.MessageParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	entry, rerr := s.lookup(params.SessionID)
	if rerr != nil {
		return nil, rerr
	}
	if params.Text == "" {
		return nil, invalidParams("the message text is required")
	}
	mode, rerr := inputMode(params.Mode)
	if rerr != nil {
		return nil, rerr
	}
	opts, rerr := inputOptions(params.Delivered)
	if rerr != nil {
		return nil, rerr
	}
	inputID, err := entry.sess.Send(params.Text, mode, opts...)
	if err != nil {
		return nil, failure(err)
	}
	return protocol.MessageResult{InputID: inputID}, nil
}

// inputMode returns the mode a message is delivered in.
func inputMode(name string) (poo.InputMode, *protocol.Error) {
	switch name {
	case "", "steer":
		return poo.Steer, nil
	case "interrupt":
		return poo.Interrupt, nil
	case "follow_up":
		return poo.FollowUp, nil
	}
	return "", invalidParams("the mode must be steer, interrupt or follow_up, not %q", name)
}

// inputOptions returns the options a message carries.
func inputOptions(delivered *protocol.Delivered) ([]poo.InputOption, *protocol.Error) {
	if delivered == nil {
		return nil, nil
	}
	if delivered.UpToChars < 0 {
		return nil, invalidParams("upto_chars must not be negative")
	}
	if delivered.MessageID == "" {
		return nil, invalidParams("the message id is required")
	}
	return []poo.InputOption{poo.WithDelivered(delivered.MessageID, delivered.UpToChars)}, nil
}

// agentInspect reports a session's state, its events, and the outcome
// of a run it waits for.
//
// The wait is not a failure: a wait that runs out answers with
// timed_out, and the client asks again.
func (s *server) agentInspect(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.InspectParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	entry, rerr := s.lookup(params.SessionID)
	if rerr != nil {
		return nil, rerr
	}
	events, next := entry.eventsAfter(params.AfterSeq)
	result := protocol.InspectResult{
		Events:  events,
		NextSeq: next,
		State:   string(entry.sess.State()),
	}
	if result.Events == nil {
		result.Events = []protocol.Event{}
	}
	if params.Wait == nil {
		return result, nil
	}
	if params.Wait.Until != "settled" {
		return nil, invalidParams("the wait must be for a settled run, not %q", params.Wait.Until)
	}
	if params.Wait.RunID == "" {
		return nil, invalidParams("the wait needs a run id")
	}
	timeout := time.Duration(params.Wait.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = DefaultInspectTimeout
	}
	settled, timedOut := entry.waitForSettle(params.Wait.RunID, timeout)
	if timedOut {
		result.TimedOut = true
		return result, nil
	}
	if settled.RunID == "" {
		// The session closed while the client waited, and the run did
		// not settle: the waiter is told the session is closed.
		return nil, protocol.Errorf(protocol.CodeServerError, protocol.DataSessionClosed,
			"the session %q is closed", entry.id)
	}
	result.Settled = &settled
	return result, nil
}

// agentCancel ends the active run.
//
// The cancel is idempotent: a run that has already settled is not an
// error, and a run the session does not have is.
func (s *server) agentCancel(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.CancelParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	entry, rerr := s.lookup(params.SessionID)
	if rerr != nil {
		return nil, rerr
	}
	if params.RunID == "" {
		return nil, invalidParams("the run id is required")
	}
	if entry.sess.ActiveRunID() == params.RunID {
		entry.sess.Cancel(sourceCancelClient)
		return map[string]any{}, nil
	}
	if _, settled := entry.sess.RunResult(params.RunID); settled {
		// The run has settled already. The cancel is idempotent.
		return map[string]any{}, nil
	}
	return nil, protocol.Errorf(protocol.CodeServerError, protocol.DataRunNotFound,
		"the session %q has no run %q", entry.id, params.RunID)
}

// jobCancel stops one background job.
//
// The cancel is idempotent: an unknown job and a job that has finished
// are both answered with an empty result.
func (s *server) jobCancel(raw json.RawMessage) (any, *protocol.Error) {
	var params protocol.JobCancelParams
	if err := protocol.DecodeParams(raw, &params); err != nil {
		return nil, paramsFailure(err)
	}
	entry, rerr := s.lookup(params.SessionID)
	if rerr != nil {
		return nil, rerr
	}
	if params.Job == "" {
		return nil, invalidParams("the job id is required")
	}
	err := entry.sess.CancelJob(params.Job)
	if err != nil && !errors.Is(err, poo.ErrUnknownJob) {
		return nil, failure(err)
	}
	return map[string]any{}, nil
}

// jobsOf returns the session's jobs.
func jobsOf(sess *poo.Session) []protocol.JobInfo {
	jobs := sess.Jobs()
	out := make([]protocol.JobInfo, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, protocol.JobInfo{
			Job:       job.Job,
			Tool:      job.Tool,
			Handle:    job.Handle,
			RunID:     job.RunID,
			StartedAt: job.StartedAt,
			Status:    job.Status,
		})
	}
	return out
}

// promptOf returns the session's system prompt.
func promptOf(sess *poo.Session) protocol.PromptState {
	state := sess.PromptState()
	return protocol.PromptState{
		Text:     state.Text,
		Hash:     state.Hash,
		Sections: state.Sections,
		Preview:  state.Preview,
	}
}

// contains reports that a list holds a value.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
