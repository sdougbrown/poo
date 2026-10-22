package poo

// The permission seam is how the policy slot reaches the core.
//
// The slot decides whether a call runs, and an ask happens inside it:
// the slot blocks on the Asker chain and answers with an allowance or a
// refusal. The core publishes the permission events and keeps the
// repeat counters, so the slot never touches the session itself.

// PermissionAsk is one permission ask.
//
// The ask names the call that is waiting and why it is waiting: a
// policy rule asked about it, or the doom loop did.
type PermissionAsk struct {
	// AskID names the ask. The id is unique within the session, and a
	// client answers by it.
	AskID string
	// Tool is the name of the tool the model called.
	Tool string
	// Summary describes the call.
	Summary string
	// Class is the tool's class.
	Class ToolClass
	// Reason is why the call is being asked about: "policy" or
	// "doom_loop".
	Reason string
	// RepeatCount holds the doom loop's counts. It is set for a
	// doom-loop ask, and nil for a policy ask.
	RepeatCount *RepeatCounts
}

// PermissionAnswer is the answer that resolved a permission ask.
//
// The answer says what was decided, who decided it and how the ask was
// resolved, so a reader can tell an answer from a fallback.
type PermissionAnswer struct {
	// AskID names the ask the answer resolves.
	AskID string
	// Decision is "allow" or "deny".
	Decision string
	// AnsweredBy names who answered: the asker that answered, or
	// "fallback" when the chain had no answer to give.
	AnsweredBy string
	// Provenance is how the ask was resolved: "answered", "timeout",
	// "default" or "cancelled".
	Provenance string
	// Reason is why the call was being asked about.
	Reason string
}

// The provenances a permission answer can carry.
const (
	// ProvenanceAnswered means an asker answered the ask.
	ProvenanceAnswered = "answered"
	// ProvenanceTimeout means the chain's deadline ran out.
	ProvenanceTimeout = "timeout"
	// ProvenanceDefault means the chain had no answer to give, and the
	// fallback decided.
	ProvenanceDefault = "default"
	// ProvenanceCancelled means the run ended while the ask was
	// pending.
	ProvenanceCancelled = "cancelled"
)

// The reasons a call is being asked about.
const (
	// ReasonPolicy means a policy rule asked about the call.
	ReasonPolicy = "policy"
	// ReasonDoomLoop means the doom loop asked about the call.
	ReasonDoomLoop = "doom_loop"
)

// PermissionReporter is the policy slot's seam to the core.
//
// The core hands the slot a reporter when it calls the slot, and the
// slot reports the asks it made and the answers that resolved them. The
// core publishes the permission events from those reports, so a policy
// engine never publishes an event itself.
//
// The reporter also clears a call's repeat counters, which is how a
// doom loop the policy allowed starts over.
type PermissionReporter interface {
	// Asked reports that the run is waiting for an answer.
	Asked(PermissionAsk)
	// Answered reports the answer that resolved the ask.
	Answered(PermissionAnswer)
	// ResetRepeats clears the repeat counters of the call with the
	// given handle.
	ResetRepeats(handle string)
}

// permissionReporter is the core's side of the policy slot's seam.
//
// The reporter publishes the events and clears the counters, so the
// slot only ever decides.
type permissionReporter struct {
	// session is the session the events go to.
	session *Session
}

// Asked publishes the ask event.
func (r *permissionReporter) Asked(ask PermissionAsk) {
	payload := map[string]any{
		"ask_id":  ask.AskID,
		"tool":    ask.Tool,
		"summary": ask.Summary,
		"class":   string(ask.Class),
		"reason":  ask.Reason,
	}
	if ask.RepeatCount != nil {
		payload["repeat_count"] = map[string]any{
			"exact": ask.RepeatCount.Exact,
			"near":  ask.RepeatCount.Near,
		}
	}
	r.session.events.emit(EventPermissionAsked, payload)
}

// Answered publishes the answer event.
func (r *permissionReporter) Answered(answer PermissionAnswer) {
	r.session.events.emit(EventPermissionAnswered, map[string]any{
		"ask_id":      answer.AskID,
		"decision":    answer.Decision,
		"answered_by": answer.AnsweredBy,
		"provenance":  answer.Provenance,
		"reason":      answer.Reason,
	})
}

// ResetRepeats clears the repeat counters of a call.
func (r *permissionReporter) ResetRepeats(handle string) {
	r.session.resetRepeats(handle)
}
