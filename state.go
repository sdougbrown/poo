package poo

// State is where a session is in the agent loop.
//
// The set is closed. The core moves between these states and nothing
// else, and a consumer that wants a new step uses a hook rather than a
// state.
type State string

const (
	// StateIdle means the session has no run.
	StateIdle State = "idle"
	// StateRequesting means the run is asking the provider for a
	// response.
	StateRequesting State = "requesting"
	// StateStreaming means the provider's answer is arriving.
	StateStreaming State = "streaming"
	// StateExecuting means the run is running the tool calls the model
	// asked for.
	StateExecuting State = "executing"
	// StateCompacting means the run is asking the compactor to compact
	// the view before the next request.
	StateCompacting State = "compacting"
	// StateWaiting means the run is open while a job it started runs.
	//
	// The state belongs to the jobs stage: nothing enters it yet, and
	// the wake path that would leave it is the same seam.
	StateWaiting State = "waiting"
	// StateSettling means the run is finalizing.
	StateSettling State = "settling"
	// StateSettled means the run has settled.
	StateSettled State = "settled"
)

// legalStates lists, for each state, the states the session may move to.
//
// The table is the FSM: a transition that is not in it is a bug in the
// core, and the transition function is the only place that moves.
//
// The waiting state is in the set for the jobs stage, and nothing
// enters it yet.
var legalStates = map[State][]State{
	StateIdle:       {StateRequesting},
	StateRequesting: {StateStreaming, StateExecuting, StateCompacting, StateRequesting, StateSettling},
	StateStreaming:  {StateExecuting, StateRequesting, StateCompacting, StateSettling},
	StateExecuting:  {StateExecuting, StateRequesting, StateCompacting, StateSettling},
	StateCompacting: {StateRequesting, StateSettling},
	StateWaiting:    {StateRequesting, StateCompacting, StateSettling},
	StateSettling:   {StateSettled},
	StateSettled:    {},
}

// IsLegal reports whether a session may move from one state to another.
//
// The table is the FSM's own: a transition that is not in it is a bug in
// the core.
func IsLegal(from, to State) bool {
	for _, s := range legalStates[from] {
		if s == to {
			return true
		}
	}
	return false
}

// transition moves the session to a new state.
//
// The transition function is the only place that changes the state, and
// it records the move so a reader can see the path the run took.
func (s *Session) transition(to State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transitionLocked(to)
}

// transitionLocked moves the session to a new state.
//
// The caller holds the session's lock.
func (s *Session) transitionLocked(to State) {
	if s.state == to {
		return
	}
	s.state = to
	s.transitions = append(s.transitions, to)
}
