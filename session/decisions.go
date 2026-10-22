package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sdougbrown/poo/decide"
)

// The sink is a decide.Sink.
var _ decide.Sink = (*JSONLDecisionSink)(nil)

// JSONLDecisionSink writes decision records to a JSONL file.
//
// The sink is for a decision-only caller: a build that runs no session
// has no session log, but it keeps the decisions it made. Every record
// is one line, and every line names the record it holds, so a reader
// knows what it is reading.
type JSONLDecisionSink struct {
	mu sync.Mutex
	f  *os.File
	// path is the sink's file.
	path string
}

// NewJSONLDecisionSink returns a sink that writes to the path.
//
// The file is created when it is missing, and the directory it lives in
// is created with it.
func NewJSONLDecisionSink(path string) (*JSONLDecisionSink, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, &Error{Code: CodeStoreError, Message: "creating " + dir, Err: err}
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, &Error{Code: CodeStoreError, Message: "opening " + path, Err: err}
	}
	return &JSONLDecisionSink{f: f, path: path}, nil
}

// Path returns the sink's file.
func (s *JSONLDecisionSink) Path() string { return s.path }

// Record writes one decision record.
//
// The write is serialized, so a concurrent record never interleaves two
// records' bytes.
func (s *JSONLDecisionSink) Record(ctx context.Context, rec decide.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return &Error{Code: CodeStoreError, Message: "the decision sink is closed"}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	line, err := json.Marshal(decisionLine{Record: rec, Name: "decision"})
	if err != nil {
		return &Error{Code: CodeStoreError, Message: "encoding the decision", Err: err}
	}
	line = append(line, '\n')
	if _, err := s.f.Write(line); err != nil {
		return &Error{Code: CodeStoreError, Message: "writing the decision", Err: err}
	}
	return nil
}

// Close closes the sink.
func (s *JSONLDecisionSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Sync()
	cerr := s.f.Close()
	s.f = nil
	if err != nil {
		return &Error{Code: CodeStoreError, Message: "syncing " + s.path, Err: err}
	}
	if cerr != nil {
		return &Error{Code: CodeStoreError, Message: "closing " + s.path, Err: cerr}
	}
	return nil
}

// decisionLine is a decision record as it is written.
//
// The line carries the record's name, so a file of decisions is
// self-describing the way a session log is.
type decisionLine struct {
	// Record is the decision the line holds.
	decide.Record
	// Name is the record's name.
	Name string `json:"record"`
}

// String returns the sink's path.
func (s *JSONLDecisionSink) String() string { return fmt.Sprintf("JSONLDecisionSink(%s)", s.path) }
