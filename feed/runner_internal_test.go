package feed

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// records collects the log records a test reads back.
type records struct {
	mu   sync.Mutex
	held []slog.Record
}

// Enabled reports that every level is on.
func (r *records) Enabled(context.Context, slog.Level) bool { return true }

// Handle keeps one record.
func (r *records) Handle(_ context.Context, record slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.held = append(r.held, record.Clone())
	return nil
}

// WithAttrs returns the same sink, which keeps the attributes it is
// given with each record.
func (r *records) WithAttrs([]slog.Attr) slog.Handler { return r }

// WithGroup returns the same sink.
func (r *records) WithGroup(string) slog.Handler { return r }

// logged returns the bytes of stderr the sink holds, and how many
// notices it holds.
func (r *records) logged() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	bytes, notices := 0, 0
	for _, record := range r.held {
		line := ""
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "line" {
				line = attr.Value.String()
			}
			return true
		})
		if line == "" {
			notices++
			continue
		}
		bytes += len(line)
	}
	return bytes, notices
}

// TestTheStderrStopsAtItsBudget covers the cap: one attempt logs at most
// its budget of a command's stderr, then says once that the rest is
// dropped.
func TestTheStderrStopsAtItsBudget(t *testing.T) {
	sink := &records{}
	const budget = 40
	writer := newStderrLog(slog.New(sink), budget)
	for i := 0; i < 20; i++ {
		if _, err := writer.Write([]byte("0123456789\n")); err != nil {
			t.Fatalf("the writer failed: %v", err)
		}
	}
	if _, err := writer.Write([]byte("after the budget\n")); err != nil {
		t.Fatalf("the writer failed after its budget: %v", err)
	}
	logged, notices := sink.logged()
	if logged > budget {
		t.Errorf("the log holds %d bytes of stderr, want at most %d", logged, budget)
	}
	if logged == 0 {
		t.Error("the budget logged nothing")
	}
	if notices != 1 {
		t.Errorf("the log holds %d notices, want 1", notices)
	}
}

// TestALinePastTheBudgetIsLoggedInPart covers the line a command never
// ends: what the budget holds is logged, and the rest is dropped
// without waiting for a newline that may never come.
func TestALinePastTheBudgetIsLoggedInPart(t *testing.T) {
	sink := &records{}
	const budget = 16
	writer := newStderrLog(slog.New(sink), budget)
	if _, err := writer.Write([]byte(strings.Repeat("x", 1000))); err != nil {
		t.Fatalf("the writer failed: %v", err)
	}
	if _, err := writer.Write([]byte("more\n")); err != nil {
		t.Fatalf("the writer failed after its budget: %v", err)
	}
	logged, notices := sink.logged()
	if logged != budget {
		t.Errorf("the log holds %d bytes, want the whole budget of %d", logged, budget)
	}
	if notices != 1 {
		t.Errorf("the log holds %d notices, want 1", notices)
	}
}
