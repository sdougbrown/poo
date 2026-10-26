package session

import (
	"encoding/json"
	"fmt"

	"github.com/sdougbrown/poo"
)

// Codec reads and writes the records of a session log.
//
// The codec is versioned: it names the format it writes, and it can
// migrate a log that was written in an older format. A log in a format
// the codec does not know is a typed failure, not a guess: a reader that
// guesses loses the records it does not understand.
type Codec interface {
	// Version is the format the codec writes, e.g. "poo-session-v0".
	Version() string
	// Encode returns a record's line, without the newline.
	Encode(rec poo.Record) ([]byte, error)
	// Decode returns the record a line holds.
	Decode(line []byte) (poo.Record, error)
	// Migrate returns the lines of a log written in another format, in
	// the codec's own format. It returns ErrCodecMismatch when the
	// codec cannot read that format.
	//
	// The migration works on the log's lines, because a log in an
	// older format may hold records this codec does not know.
	Migrate(from string, lines [][]byte) ([][]byte, error)
}

// JSONLCodec is the codec of the append-only JSONL session log.
//
// The codec writes one record per line and reads the record names it
// knows. The names are the log's vocabulary, and the vocabulary grows:
// a log that carries a record the codec does not know is a failure, so
// a reader stops instead of dropping what it does not understand.
type JSONLCodec struct{}

// NewCodec returns the codec of the current session log format.
func NewCodec() Codec { return JSONLCodec{} }

// Version returns the format the codec writes.
func (JSONLCodec) Version() string { return poo.FormatVersion }

// Encode returns a record's line, without the newline.
func (JSONLCodec) Encode(rec poo.Record) ([]byte, error) {
	b, err := json.Marshal(rec)
	if err != nil {
		return nil, &Error{Code: CodeStoreError, Message: "encoding " + rec.Meta().Record, Err: err}
	}
	return b, nil
}

// Decode returns the record a line holds.
func (JSONLCodec) Decode(line []byte) (poo.Record, error) {
	var envelope struct {
		Record string `json:"record"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil, &Error{Code: CodeCorruptLog, Message: "the line is not a record", Err: err}
	}
	newRecord, ok := recordTypes[envelope.Record]
	if !ok {
		return nil, &Error{Code: CodeCodecMismatch, Message: "the log holds an unknown record: " + envelope.Record}
	}
	rec := newRecord()
	if err := json.Unmarshal(line, rec); err != nil {
		return nil, &Error{Code: CodeCorruptLog, Message: "the line is not a " + envelope.Record, Err: err}
	}
	return rec, nil
}

// Migrate returns the records of a log written in another format.
//
// The current format is the first one, so there is nothing to migrate
// from: a log in another format is a mismatch.
func (JSONLCodec) Migrate(from string, lines [][]byte) ([][]byte, error) {
	return nil, &Error{Code: CodeCodecMismatch, Message: "the codec cannot read " + from}
}

// recordTypes are the records a session log holds, by name.
//
// The names are the log's vocabulary. A record type that is added to the
// core is added here too, so a log that carries it can be read.
var recordTypes = map[string]func() poo.Record{
	"session.created":        func() poo.Record { return &poo.SessionCreatedRecord{} },
	"run.started":            func() poo.Record { return &poo.RunStartedRecord{} },
	"run.settled":            func() poo.Record { return &poo.RunSettledRecord{} },
	"system_prompt":          func() poo.Record { return &poo.SystemPromptRecord{} },
	"message":                func() poo.Record { return &poo.MessageRecord{} },
	"tool.call":              func() poo.Record { return &poo.ToolCallRecord{} },
	"tool.result":            func() poo.Record { return &poo.ToolResultRecord{} },
	"permission.asked":       func() poo.Record { return &poo.PermissionAskedRecord{} },
	"permission.answered":    func() poo.Record { return &poo.PermissionAnsweredRecord{} },
	"observer.dropped":       func() poo.Record { return &poo.ObserverDroppedRecord{} },
	"usage":                  func() poo.Record { return &poo.UsageRecord{} },
	"provider.attempt":       func() poo.Record { return &poo.ProviderAttemptRecord{} },
	"compaction":             func() poo.Record { return &poo.CompactionRecord{} },
	"handoff":                func() poo.Record { return &poo.HandoffRecord{} },
	"session.models_changed": func() poo.Record { return &poo.ModelsChangedRecord{} },
	"decision":               func() poo.Record { return &poo.DecisionRecord{} },
	"input.queued":           func() poo.Record { return &poo.InputQueuedRecord{} },
	"input.delivered":        func() poo.Record { return &poo.InputDeliveredRecord{} },
	"input.dropped":          func() poo.Record { return &poo.InputDroppedRecord{} },
	"input.held":             func() poo.Record { return &poo.InputHeldRecord{} },
	"turn.interrupted":       func() poo.Record { return &poo.TurnInterruptedRecord{} },
	"job.started":            func() poo.Record { return &poo.JobStartedRecord{} },
	"job.ended":              func() poo.Record { return &poo.JobEndedRecord{} },
}

// String returns the codec's format.
func (c JSONLCodec) String() string { return fmt.Sprintf("JSONLCodec(%s)", c.Version()) }
