package session

import (
	"encoding/json"
	"time"

	"github.com/sdougbrown/poo"
)

// Metadata is a session's header: what its session.created record
// carries.
//
// The header is everything a loaded session needs to be the session it
// was: the profile it was created with, the models it talked to, the
// tools it was given and the binary that created it.
type Metadata struct {
	// Format is the record vocabulary the session was written in.
	Format string
	// SessionID identifies the session.
	SessionID string
	// Mode is the session's persistence mode.
	Mode Mode
	// CreatedAt is when the session was created.
	CreatedAt time.Time
	// WorkingDir is the absolute directory the session works in.
	WorkingDir string
	// Profile is the profile the session was created with.
	Profile string
	// Roles are the effective model roles, when the session names
	// them: the role and the model that filled it.
	Roles map[string]string
	// SettingsHash is the hash of the settings the session was created
	// with.
	SettingsHash string
	// Tools are the tools linked into the session.
	Tools []poo.SessionTool
	// Model is the model the session talks to.
	Model poo.SessionModel
	// ContextLimit is the model's context window, in tokens.
	ContextLimit int
	// ToolsMode is how the session asks for tool calls.
	ToolsMode string
	// Build names the binary that created the session.
	Build poo.BuildInfo
}

// metadataLine is a session.created record as it is stored.
//
// The line is read on its own, so a header field the core's record type
// does not name yet — the profile, the roles, the settings hash — is
// still read.
type metadataLine struct {
	Format       string            `json:"format"`
	SessionID    string            `json:"session_id"`
	Mode         string            `json:"mode"`
	CreatedAt    string            `json:"created_at"`
	WorkingDir   string            `json:"working_dir"`
	Profile      string            `json:"profile"`
	Roles        map[string]string `json:"roles"`
	SettingsHash string            `json:"settings_hash"`
	Tools        []poo.SessionTool `json:"tools"`
	Model        poo.SessionModel  `json:"model"`
	ContextLimit int               `json:"context_limit"`
	ToolsMode    string            `json:"tools_mode"`
	Build        poo.BuildInfo     `json:"build"`
}

// metadata returns the header a session.created line carries.
func (l metadataLine) metadata() Metadata {
	return Metadata{
		Format:       l.Format,
		SessionID:    l.SessionID,
		Mode:         Mode(l.Mode),
		CreatedAt:    parseTime(l.CreatedAt),
		WorkingDir:   l.WorkingDir,
		Profile:      l.Profile,
		Roles:        l.Roles,
		SettingsHash: l.SettingsHash,
		Tools:        l.Tools,
		Model:        l.Model,
		ContextLimit: l.ContextLimit,
		ToolsMode:    l.ToolsMode,
		Build:        l.Build,
	}
}

// metadataOf reads a session's header from a log's first line.
func metadataOf(line []byte) (Metadata, error) {
	var raw metadataLine
	if err := json.Unmarshal(line, &raw); err != nil {
		return Metadata{}, &Error{Code: CodeCorruptLog, Message: "the log does not start with a session", Err: err}
	}
	return raw.metadata(), nil
}

// Summary is what a store shows about one session.
//
// It is a projection: a summary is computed from the session's records,
// and it is not the session itself.
type Summary struct {
	// ID identifies the session.
	ID string
	// CreatedAt is when the session was created.
	CreatedAt time.Time
	// Mode is the session's persistence mode.
	Mode Mode
	// Profile is the profile the session was created with.
	Profile string
	// Build names the binary that created the session.
	Build poo.BuildInfo
	// Runs is the number of runs the session had.
	Runs int
	// LastReason is the settle reason of the session's last run.
	LastReason string
	// Size is the size of the session's file, in bytes.
	Size int64
	// Locked reports that another process holds the session's lock.
	Locked bool
	// LastActivity is when the session's last record was written.
	LastActivity time.Time
}

// Projection computes the summaries of sessions.
//
// The projection is general: it computes summaries from records, and it
// is not the history projection, which pages a session's items.
type Projection struct{}

// Summarize computes the summary of a session from its header and its
// records.
func (Projection) Summarize(meta Metadata, recs []poo.Record) Summary {
	scan := &scanned{}
	for _, rec := range recs {
		line := summaryLine{Record: rec.Meta().Record, TS: rec.Meta().TS}
		switch r := rec.(type) {
		case *poo.RunSettled:
			line.Reason = string(r.Reason)
		case *poo.SessionCreated:
			line.CreatedAt = r.CreatedAt
			line.Mode = r.Mode
			line.Build = r.Build
		}
		scan.add(line)
	}
	sum := Summary{
		ID:           meta.SessionID,
		CreatedAt:    meta.CreatedAt,
		Mode:         meta.Mode,
		Profile:      meta.Profile,
		Build:        meta.Build,
		Runs:         scan.runs,
		LastReason:   scan.lastReason,
		LastActivity: scan.lastActivity,
	}
	return sum
}

// summaryLine is what a summary reads from one record.
type summaryLine struct {
	// Record is the record's name.
	Record string `json:"record"`
	// TS is the record's timestamp.
	TS string `json:"ts"`
	// Reason is the settle reason of a run.settled record.
	Reason string `json:"reason"`
	// CreatedAt is the creation time of a session.created record.
	CreatedAt string `json:"created_at"`
	// Mode is the persistence mode of a session.created record.
	Mode string `json:"mode"`
	// Profile is the profile of a session.created record.
	Profile string `json:"profile"`
	// Build names the binary of a session.created record.
	Build poo.BuildInfo `json:"build"`
}

// scanned is what a summary is built from.
type scanned struct {
	// created is when the session was created.
	created time.Time
	// mode is the session's persistence mode.
	mode Mode
	// profile is the profile the session was created with.
	profile string
	// build names the binary that created the session.
	build poo.BuildInfo
	// runs is the number of runs the session had.
	runs int
	// lastReason is the settle reason of the session's last run.
	lastReason string
	// lastActivity is when the session's last record was written.
	lastActivity time.Time
}

// add reads one record into the scan.
func (s *scanned) add(line summaryLine) {
	switch line.Record {
	case "session.created":
		s.created = parseTime(line.CreatedAt)
		s.mode = Mode(line.Mode)
		s.profile = line.Profile
		s.build = line.Build
	case "run.started":
		s.runs++
	case "run.settled":
		s.lastReason = line.Reason
	}
	if ts := parseTime(line.TS); ts.After(s.lastActivity) {
		s.lastActivity = ts
	}
}

// parseTime reads a record's timestamp.
//
// A record with no timestamp is at the start of time: a summary says
// what it knows, and it does not invent a time.
func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}
