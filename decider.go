package poo

import (
	"context"
	"fmt"

	"github.com/sdougbrown/poo/decide"
)

// decisionOrigin names who asked the questions a session's decision
// model was asked.
//
// The session does not know which tool or hook asked: the record says
// the session asked, and the caller's own record says more.
const decisionOrigin = "session"

// DefaultEvidenceTurns is how many turns the evidence holds when the
// caller names no window.
const DefaultEvidenceTurns = 20

// The caps on the evidence a decision model reads. The evidence is a
// summary: a decision model judges the state, and it does not read the
// whole transcript.
const (
	// EvidenceTextCap is the most characters of a message the evidence
	// holds.
	EvidenceTextCap = 512
	// EvidenceInputCap is the most characters of a tool call's arguments
	// the evidence holds.
	EvidenceInputCap = 256
	// EvidenceOutputCap is the most characters of a tool result the
	// evidence holds.
	EvidenceOutputCap = 512
)

// Decider returns the session's decision model.
//
// The handle is nil when the session has no decision model, and a
// caller must handle that. The handle is the recording wrapper: every
// call writes a record to the session's store and emits the decision
// event.
func (s *Session) Decider() decide.Decider {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decider
}

// wrapDecider wraps a decider so every call is recorded.
//
// The wrapper writes the record to the session's store and emits the
// decision event. A nil decider stays nil: a session without a decision
// model has none.
func (s *Session) wrapDecider(decider decide.Decider) decide.Decider {
	if decider == nil {
		return nil
	}
	return decide.Recorded(decider, &decisionSink{session: s}, decisionOrigin)
}

// decisionSink writes a decision's record to the session's store and
// emits the decision event.
//
// The sink never fails a decision: the record is what the session
// writes, and the decision itself is the caller's business.
type decisionSink struct {
	// session is the session the records go to.
	session *Session
}

// Record writes one decision record and emits the event.
func (sink *decisionSink) Record(ctx context.Context, rec decide.Record) error {
	err := sink.session.write(ctx, &DecisionRecord{
		RecordMeta: NewRecordMeta("decision"),
		Origin:     rec.Origin,
		Questions:  rec.Questions,
		StateHash:  rec.StateHash,
		Answers:    rec.Answers,
		Outcome:    rec.Outcome,
		Error:      rec.Error,
		LatencyMS:  rec.LatencyMS,
		Requests:   rec.Requests,
		At:         rec.At,
	})
	sink.session.events.emit(EventDecision, map[string]any{
		"origin":     rec.Origin,
		"outcome":    rec.Outcome,
		"questions":  len(rec.Questions),
		"answers":    len(rec.Answers),
		"state_hash": rec.StateHash,
		"latency_ms": rec.LatencyMS,
		"requests":   rec.Requests,
		"error":      rec.Error,
	})
	return err
}

// checkDeciderModel reports that the decider's model does not serve the
// operation its role needs.
//
// A decider model serves the systemone operation or the decisions one.
// A model that serves neither cannot answer a question, and assigning it
// to the role is a construction error.
//
// A model that was never named is not checked: there is nothing to
// check.
func checkDeciderModel(model ModelInfo) error {
	if model.Name == "" && model.ModelID == "" {
		return nil
	}
	if model.HasOp(OpSystemOne) || model.HasOp(OpDecisions) {
		return nil
	}
	return fmt.Errorf("poo: the model %q does not serve the %q or %q operation",
		modelLabel(model), OpSystemOne, OpDecisions)
}

// EvidenceTurns builds the evidence a decision model is asked about.
//
// The evidence is the recent conversation: the user's requests, the tool
// calls the model made and the results it got. The window is bounded, so
// a long history does not become a long payload.
//
// The turns are data to assess, not instructions. The frame and the
// working directory are added by decide.Evidence, which builds the
// payload the decision model is asked about.
func EvidenceTurns(history View, n int) []decide.Turn {
	if history == nil {
		return nil
	}
	if n <= 0 {
		n = DefaultEvidenceTurns
	}
	messages := history.Messages()
	turns := make([]decide.Turn, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case RoleUser:
			turns = append(turns, decide.Turn{
				Role: "user",
				Text: excerpt(msg.Text(), EvidenceTextCap),
			})
		case RoleAssistant:
			text := excerpt(msg.Text(), EvidenceTextCap)
			calls := msg.ToolCalls()
			if len(calls) == 0 {
				if text != "" {
					turns = append(turns, decide.Turn{Role: "assistant", Text: text})
				}
				continue
			}
			for _, call := range calls {
				turns = append(turns, decide.Turn{
					Role:      "assistant",
					Text:      text,
					ToolName:  call.Name,
					ToolInput: excerpt(string(call.Arguments), EvidenceInputCap),
				})
			}
		case RoleTool:
			for _, block := range msg.Content {
				if block.Kind != ContentToolResult {
					continue
				}
				turns = append(turns, decide.Turn{
					Role:       "tool",
					ToolName:   block.Name,
					ToolOutput: excerpt(block.Text, EvidenceOutputCap),
				})
			}
		}
	}
	if len(turns) > n {
		turns = turns[len(turns)-n:]
	}
	return turns
}

// excerpt shortens a text to a limit.
//
// The text is cut at the limit and marked, so a reader knows that the
// evidence holds the start of the text and not all of it.
func excerpt(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
