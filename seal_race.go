package poo

// SealRaceHooks are the seams the seal protocol is tested at.
//
// A hook is called at a named boundary, and it blocks the caller until it
// returns. A test can therefore pin the order the two claims a call's
// records are made against are made in: the batch's skip path and the
// finalization's seal.
type SealRaceHooks struct {
	// AfterSeal is called once the run is sealed and the finalization
	// owns the calls and the inputs the seal took, before it settles
	// them.
	AfterSeal func()
	// BeforeSkipCalls is called at the skip path, before it claims any
	// call of the batch.
	BeforeSkipCalls func()
	// BeforeAcceptanceWait is called by a natural finalization once it
	// finds an input whose acceptance record is still being written,
	// before it waits for that record.
	BeforeAcceptanceWait func()
}

// SetSealRaceHooks installs the seal hooks and returns a function that
// removes them.
//
// A session without them never calls anything, so the core's own path
// does not change.
func (s *Session) SetSealRaceHooks(hooks *SealRaceHooks) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.sealHooks
	s.sealHooks = hooks
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.sealHooks = prev
	}
}

// sealRaceHook runs the named seal hook, if the session has one.
//
// The caller must not hold the session's lock.
func (s *Session) sealRaceHook(name string) {
	s.mu.Lock()
	hooks := s.sealHooks
	s.mu.Unlock()
	if hooks == nil {
		return
	}
	switch name {
	case "seal":
		if hooks.AfterSeal != nil {
			hooks.AfterSeal()
		}
	case "skip":
		if hooks.BeforeSkipCalls != nil {
			hooks.BeforeSkipCalls()
		}
	case "acceptance":
		if hooks.BeforeAcceptanceWait != nil {
			hooks.BeforeAcceptanceWait()
		}
	}
}
