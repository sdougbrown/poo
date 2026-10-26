package poo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sdougbrown/poo/decide"
)

// ModelSwitch is the outcome of a model switch.
//
// The switch itself succeeds even when it carries a warning: a warning
// says what the caller should know about the run that follows.
type ModelSwitch struct {
	// From is the name of the model the session talked to.
	From string `json:"from"`
	// To is the name of the model the session talks to now.
	To string `json:"to"`
	// Warnings are the things the caller should know about the switch.
	Warnings []string `json:"warnings,omitempty"`
}

// SetModel switches the session to another chat model.
//
// The switch happens between the runs: a session with a run going
// refuses it, and the wire answers "busy".
//
// The checks are the plan's, in order:
//
//  1. The new model serves the chat operation. A model that does not is
//     a session_incompatible failure.
//  2. The history holds no required provider block from another API. A
//     block that is not required is left out of the requests, and the
//     omission is recorded.
//  3. The view fits the new context window. A view that does not fit is
//     a warning, not a refusal: the history is never truncated, and the
//     next run's gate settles context_exhausted.
//
// The system prompt is rendered in the switch, because the environment
// section names the model and the model info selects the variant. A prompt
// that cannot render in the new environment refuses the switch.
//
// The transport is the session's chat transport when the call names none,
// and the one given when it does. A model on another endpoint needs its
// own transport.
func (s *Session) SetModel(info ModelInfo, transport ...ChatTransport) (ModelSwitch, error) {
	if len(transport) > 1 {
		return ModelSwitch{}, errors.New("poo: a model switch takes at most one transport")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ModelSwitch{}, ErrSessionClosed
	}
	if s.broken {
		s.mu.Unlock()
		return ModelSwitch{}, ErrSessionBroken
	}
	if s.run != nil || s.reserved {
		s.mu.Unlock()
		return ModelSwitch{}, ErrBusy
	}
	// The model must serve the operation the chat role needs.
	if !info.HasOp(OpChat) {
		s.mu.Unlock()
		return ModelSwitch{}, &ProviderError{
			Kind: ProviderKindSessionIncompatible,
			Message: fmt.Sprintf("the model %q does not serve the %q operation",
				modelLabel(info), OpChat),
		}
	}
	name := modelLabel(info)
	if name == "" {
		s.mu.Unlock()
		return ModelSwitch{}, fmt.Errorf("poo: the model has no name")
	}
	// The wire API stays the one the session speaks unless the new model
	// names another one.
	api := info.API
	if api == "" {
		api = s.opts.Model.API
	}
	// The history must hold no required block from another API: the
	// request cannot be made without it.
	omitted, err := foreignBlocks(s.history, api)
	if err != nil {
		s.mu.Unlock()
		return ModelSwitch{}, err
	}
	// The view must fit the new window. It is never truncated, so a view
	// that does not fit is a warning and the next run settles
	// context_exhausted.
	limit := info.ContextLimit
	var warnings []string
	if limit <= 0 {
		limit = s.opts.Model.ContextLimit
		warnings = append(warnings, fmt.Sprintf(
			"the model %q declares no context limit, so the session keeps the %d-token window",
			name, limit))
	}
	if exhausted, reason := s.contextExhaustedLocked(limit); exhausted {
		warnings = append(warnings, fmt.Sprintf(
			"the current view does not fit the %d-token window of %q: %s; the next run settles context_exhausted",
			limit, name, reason))
	}

	from := modelLabel(s.opts.Model)
	outcome := ModelSwitch{From: from, To: name, Warnings: warnings}

	// The model info selects the prompt's variant, and the environment
	// section names the model, so the prompt renders again in the new
	// environment. A prompt that cannot render there refuses the switch,
	// and the session keeps the model and the prompt it had.
	modelID := modelIdentifier(info)
	env := s.promptBuilder.Env()
	env.Model = modelID
	env.Variant = info.PromptVariant
	promptText, promptSections, err := renderPrompt(s.promptBuilder.WithEnv(env), s.opts.SystemPrompt)
	if err != nil {
		s.mu.Unlock()
		return ModelSwitch{}, err
	}

	// The switch itself: the transport, the model's identity, the
	// window and the output limit.
	if len(transport) == 1 && transport[0] != nil {
		s.opts.Transport = transport[0]
	}
	// The model keeps the identifier the endpoint calls it by, and the
	// name is the fallback. The wire API stays the one the session
	// speaks, so the model info and the session agree.
	info.ModelID = modelID
	info.API = api
	info.ContextLimit = limit
	s.opts.Model = info
	s.promptVariant = info.PromptVariant
	s.promptBuilder = s.promptBuilder.WithEnv(env)
	s.promptText = promptText
	s.promptSections = promptSections
	s.mu.Unlock()

	rec := &ModelsChangedRecord{
		RecordMeta: NewRecordMeta("session.models_changed"),
		From:       from,
		To:         name,
		Warnings:   warnings,
		Omitted:    omitted,
	}
	if err := s.write(context.Background(), rec); err != nil {
		return outcome, err
	}
	s.events.emit(EventModelsChanged, map[string]any{
		"from":     from,
		"to":       name,
		"warnings": warnings,
	})
	return outcome, nil
}

// SetDecider switches the session to another decision model.
//
// The decider is optional: a session without one has no decision model,
// and a nil decider takes the one it has away.
//
// The model must serve the operation the decider's role needs: the
// systemone one or the decisions one. A model that serves neither
// cannot answer a question, and assigning it to the role is refused.
//
// The switch happens between the runs, like the chat model's.
func (s *Session) SetDecider(info ModelInfo, decider decide.Decider) error {
	if decider != nil {
		if err := checkDeciderModel(info); err != nil {
			return err
		}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	if s.broken {
		s.mu.Unlock()
		return ErrSessionBroken
	}
	if s.run != nil || s.reserved {
		s.mu.Unlock()
		return ErrBusy
	}
	s.deciderModel = info
	s.decider = s.wrapDecider(decider)
	s.mu.Unlock()
	return nil
}

// contextExhaustedLocked reports that the next request could not be
// made: the view does not fit the window, or there is no room to
// answer.
//
// The gate settles the run as context_exhausted in both cases, so a
// switch warns exactly when the gate would settle.
//
// The caller holds the session's lock.
func (s *Session) contextExhaustedLocked(limit int) (bool, string) {
	estimated := s.est.estimate(s.modelMessagesLocked())
	if float64(estimated) >= s.opts.HardRatio*float64(limit) {
		return true, fmt.Sprintf("the estimated input of %d tokens is at or above %d%% of the %d-token window",
			estimated, int(s.opts.HardRatio*100), limit)
	}
	room := limit - estimated - OutputReserve
	if room < MinOutputTokens {
		return true, fmt.Sprintf("the computed max_tokens %d is under the %d-token minimum", room, MinOutputTokens)
	}
	return false, ""
}

// foreignBlocks reports the provider blocks in the history that belong
// to another API.
//
// A required block is a failure: the request cannot be made without it,
// and the session is incompatible with the new model. A block that is
// not required is left out of the requests, and the caller records the
// omission.
func foreignBlocks(history []Message, api API) ([]OmittedBlock, error) {
	if api == "" {
		return nil, nil
	}
	counts := map[string]int{}
	var order []string
	for _, msg := range history {
		for _, block := range msg.Content {
			p := block.Provider
			if p == nil || p.API == api {
				continue
			}
			if p.Required {
				return nil, &ProviderError{
					Kind: ProviderKindSessionIncompatible,
					Message: fmt.Sprintf("the history holds a required %q block (%s) that the %q API cannot carry",
						p.Type, p.API, api),
				}
			}
			key := string(p.API) + "\x00" + p.Type
			if counts[key] == 0 {
				order = append(order, key)
			}
			counts[key]++
		}
	}
	out := make([]OmittedBlock, 0, len(order))
	for _, key := range order {
		api, typ, _ := strings.Cut(key, "\x00")
		out = append(out, OmittedBlock{API: API(api), Type: typ, Count: counts[key]})
	}
	return out, nil
}

// stripForeignBlocks drops the provider blocks that belong to another
// API.
//
// A block the current model cannot read is left out of the request: the
// history keeps it, and the switch that brought the new model recorded
// the omission. A block that rides beside a visible one loses only the
// payload, and the visible text stays.
func stripForeignBlocks(messages []Message, api API) []Message {
	if api == "" {
		return messages
	}
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		changed := false
		blocks := make([]ContentBlock, 0, len(msg.Content))
		for _, block := range msg.Content {
			p := block.Provider
			if p == nil || p.API == api {
				blocks = append(blocks, block)
				continue
			}
			changed = true
			if block.Kind == ContentProvider {
				// The block is nothing but the payload, so it goes
				// away whole.
				continue
			}
			// The visible block stays; only its companion goes.
			block.Provider = nil
			blocks = append(blocks, block)
		}
		if !changed {
			out = append(out, msg)
			continue
		}
		msg.Content = blocks
		out = append(out, msg)
	}
	return out
}

// modelIdentifier returns what the endpoint calls the model.
func modelIdentifier(m ModelInfo) string {
	if m.ModelID != "" {
		return m.ModelID
	}
	return m.Name
}
