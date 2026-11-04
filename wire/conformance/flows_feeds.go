package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/sdougbrown/poo/wire/protocol"
)

// The feeds flow: a session's settings name a command of the session's
// own, and what the command prints arrives as input.

// The feeds the flow's manifests name.
const (
	// feedWakeName names the feed whose input may start a run.
	feedWakeName = "suite-wake"
	// feedHoldName names the feed whose input waits for one.
	feedHoldName = "suite-hold"
)

// The message the scripted feeds print.
const (
	// feedText is the text of the message a feed emits.
	feedText = "the feed's message"
	// feedSender is the sender that message names.
	feedSender = "the suite"
)

// feedWaitSteps is how many 50ms waits a scripted feed runs for before it
// ends by itself, so a binary the suite kills leaves nothing behind.
const feedWaitSteps = 200

// feeds covers the flow of a session that runs a feed: the session starts
// the command its manifest names, what the command prints is delivered as
// input, and the feed is listed and stopped like a job.
func (s *suite) feeds() {
	s.t.Helper()
	if !s.announcesFeeds() {
		return
	}
	s.feedWakesTheSession()
	s.feedHoldsItsInput()
}

// announcesFeeds reports whether the binary runs the feeds its settings
// name, and skips the flow when it does not.
//
// A binary that runs no feed refuses a settings file that names one before
// it serves, so the flow reads the capability from the handshake the
// earlier flows recorded, before it opens a session with a manifest.
func (s *suite) announcesFeeds() bool {
	s.t.Helper()
	if !s.handshakeResult.Capabilities.Feeds {
		s.skip("the binary announces no feeds capability")
		return false
	}
	return true
}

// feedWakesTheSession covers the feed that is running: an idle session
// wakes for its input, the feed is listed with the jobs, and a client
// stops it.
func (s *suite) feedWakesTheSession() {
	s.t.Helper()
	manifest := s.feedManifest(feedWakeName, map[string]any{
		"description": "the messages the conformance suite prints",
		"command":     s.feedCommand(),
		"on_idle":     "wake",
	})
	c, sessionID := s.open(s.turns, s.feedSettings(manifest))

	// The session started the feed's command, as the manifest names it.
	started := c.waitEvent("feed started", func(e event) bool {
		return e.Type == "feed.started" && e.Payload["feed"] == feedWakeName
	})
	if origin, _ := started.Payload["origin"].(string); origin != feedOrigin(feedWakeName) {
		s.t.Errorf("the feed started with the origin %q, want %q", origin, feedOrigin(feedWakeName))
	}

	// What the command printed is an input of the feed's.
	queued := c.waitEvent("the feed's input queued", func(e event) bool {
		return e.Type == "input.queued" && e.Payload["origin"] == feedOrigin(feedWakeName)
	})
	if mode, _ := queued.Payload["mode"].(string); mode != "feed" {
		s.t.Errorf("the input of the feed is queued as mode %q, want feed", mode)
	}

	// The session was idle, so the input started a run of its own.
	woken := c.waitEvent("the run the feed's input woke", func(e event) bool {
		return e.Type == "run.started" && e.Payload["trigger"] == "feed"
	})
	if names := payloadNames(woken.Payload, "feeds"); len(names) != 1 || names[0] != feedWakeName {
		s.t.Errorf("the woken run names the feeds %v, want [%s]", woken.Payload["feeds"], feedWakeName)
	}
	runID, _ := woken.Payload["run_id"].(string)
	if runID == "" {
		s.t.Fatalf("the woken run names no id: %v", woken.Payload)
	}
	s.settled(c, sessionID, runID)

	// The woken run's request carries the feed's text, framed as the
	// feed's input.
	requests := c.requests()
	if len(requests) == 0 {
		s.t.Fatal("the woken run made no request")
	}
	if want := fmt.Sprintf(`<feed name="%s" from="%s">%s</feed>`, feedWakeName, feedSender, feedText); !requestHolds(requests[0], want) {
		s.t.Errorf("the woken run's request does not carry %q: %v", want, requests[0])
	}

	// The feed is a job of the session's, under its origin and the feed
	// tool.
	state := s.sessionState(c, sessionID)
	listed := false
	for _, job := range state.Jobs {
		if job.JobID != feedOrigin(feedWakeName) {
			continue
		}
		listed = true
		if job.Tool != "feed" {
			s.t.Errorf("the feed is listed with the tool %q, want feed", job.Tool)
		}
	}
	if !listed {
		s.t.Errorf("session/get lists the jobs %v, want %q among them", state.Jobs, feedOrigin(feedWakeName))
	}

	// Cancelling the feed ends its attempt.
	c.call(protocol.MethodJobCancel, map[string]any{
		"session_id": sessionID,
		"job":        feedOrigin(feedWakeName),
	}, &struct{}{})
	ended := c.waitEvent("feed ended", func(e event) bool {
		return e.Type == "feed.ended" && e.Payload["feed"] == feedWakeName
	})
	if outcome, _ := ended.Payload["outcome"].(string); outcome != "cancelled" {
		s.t.Errorf("the feed ended %q, want cancelled", outcome)
	}
}

// feedHoldsItsInput covers the feed whose input is held: the manifest says
// the input may not start a run, so it waits for one a prompt starts, and
// that run takes it.
func (s *suite) feedHoldsItsInput() {
	s.t.Helper()
	manifest := s.feedManifest(feedHoldName, map[string]any{
		"description": "the messages the conformance suite prints",
		"command":     s.feedCommand(),
		"on_idle":     "hold",
	})
	c, sessionID := s.open(s.turns, s.feedSettings(manifest))

	held := c.waitEvent("the feed's input held", func(e event) bool {
		return e.Type == "input.held" && slices.Contains(payloadNames(e.Payload, "jobs"), feedOrigin(feedHoldName))
	})
	if reason, _ := held.Payload["reason"].(string); reason != "on_idle" {
		s.t.Errorf("the input of the feed is held for %q, want on_idle", reason)
	}
	// The held input started no run: it waits for the run something else
	// starts.
	s.noRun(c, sessionID, holdWindow)

	// The run a prompt starts takes the held input, framed as the feed's.
	runID := s.prompt(c, sessionID, "say hello")
	s.settled(c, sessionID, runID)
	want := fmt.Sprintf(`<feed name="%s" from="%s">%s</feed>`, feedHoldName, feedSender, feedText)
	delivered := false
	for _, request := range c.requests() {
		if requestHolds(request, want) {
			delivered = true
		}
	}
	if !delivered {
		s.t.Errorf("no request of the prompted run carries the held input %q", want)
	}

	// The close stops the feed, so nothing the manifest named outlives the
	// session.
	c.call(protocol.MethodSessionClose, map[string]any{
		"session_id": sessionID,
	}, &struct{}{})
	ended := c.waitEvent("feed ended", func(e event) bool {
		return e.Type == "feed.ended" && e.Payload["feed"] == feedHoldName
	})
	if outcome, _ := ended.Payload["outcome"].(string); outcome != "session_closed" {
		s.t.Errorf("the feed ended %q with the session, want session_closed", outcome)
	}
}

// holdWindow is how long the flow watches for a run a held input must not
// start.
const holdWindow = 250 * time.Millisecond

// feedCommand returns the command a scripted feed runs: one message on
// stdout, then a bounded wait, so the process ends by itself when no one
// stops it.
func (s *suite) feedCommand() []string {
	s.t.Helper()
	line, err := json.Marshal(map[string]string{"text": feedText, "from": feedSender})
	if err != nil {
		s.t.Fatalf("encoding the feed's message: %v", err)
	}
	return []string{"sh", "-c", fmt.Sprintf(
		`printf '%%s\n' %s; i=0; while [ "$i" -lt %d ]; do sleep 0.05; i=$((i+1)); done`,
		shellQuote(string(line)), feedWaitSteps)}
}

// feedManifest writes a manifest naming one feed and returns its path.
//
// The manifest is the flow's, written into the suite's directory; the
// settings name it, so the binary reads it the way it reads any other.
func (s *suite) feedManifest(name string, spec map[string]any) string {
	s.t.Helper()
	document := map[string]any{
		"version": 1,
		"feeds":   map[string]any{name: spec},
	}
	data, err := json.Marshal(document)
	if err != nil {
		s.t.Fatalf("encoding the feed manifest: %v", err)
	}
	path := filepath.Join(s.dir, "feeds-"+name+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		s.t.Fatalf("writing the feed manifest: %v", err)
	}
	return path
}

// feedSettings returns the settings a flow adds to name a manifest.
func (s *suite) feedSettings(manifest string) map[string]any {
	return map[string]any{
		"feeds": map[string]any{"paths": []string{manifest}},
	}
}

// feedOrigin returns the origin a feed's records and inputs carry.
func feedOrigin(name string) string { return "feed:" + name }

// payloadNames returns a payload's field as a list of names.
//
// The payload is the event's own data, so the list arrives as the JSON
// array it was written as.
func payloadNames(payload map[string]any, key string) []string {
	items, _ := payload[key].([]any)
	names := make([]string, 0, len(items))
	for _, item := range items {
		if name, ok := item.(string); ok {
			names = append(names, name)
		}
	}
	return names
}

// noRun checks that the session started no run within the window.
//
// A held input never starts one, so a run is the flow's failure; the wait
// gives a run that would have started the time to say so.
func (s *suite) noRun(c *client, sessionID string, window time.Duration) {
	s.t.Helper()
	deadline := time.Now().Add(window)
	for {
		for _, evt := range c.eventList() {
			if evt.Type == "run.started" && evt.SessionID == sessionID {
				s.t.Fatalf("the session started the run %v for a held feed input", evt.Payload["run_id"])
			}
		}
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
