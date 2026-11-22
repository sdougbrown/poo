package poo_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/session"
)

// A compaction's own asks can take minutes over a slow endpoint, and
// the claim that protects the compaction's record is held across them.
// A claim bounded by one record-write budget would be expired before
// the record it protects is written, and the run would settle with a
// store error no store made.

// slowCompactor is a compactor whose ask takes longer than a shrunken
// record-write budget.
type slowCompactor struct {
	delay time.Duration
	fired bool
}

func (c *slowCompactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	return !c.fired && budget.Turns >= 1
}

func (c *slowCompactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	c.fired = true
	select {
	case <-ctx.Done():
		return poo.Compaction{}, ctx.Err()
	case <-time.After(c.delay):
	}
	// The covered range is the first turn alone: the view holds one
	// completed turn when the compaction runs.
	marker := "[Context compacted: turns 1-1 reconstructed below]"
	return poo.Compaction{
		Strategy: "slow",
		Replace: &poo.RangeReplacement{
			Cut:      poo.TurnRange{First: 1, Last: 1},
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, marker+"\n\nthe work continues")},
		},
	}, nil
}

// TestACompactionSlowerThanTheRecordBudgetWritesItsRecord covers the
// claim: the compactor runs longer than the budget, the record lands
// whole, and the run carries on instead of settling with a store error.
func TestACompactionSlowerThanTheRecordBudgetWritesItsRecord(t *testing.T) {
	big := strings.Repeat("x", 40000)
	tr := scripted([]fakeResponse{
		{text: big, calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		{text: "the final answer"},
	})
	// The sink is a real session log: it checks the write's context,
	// the way the file store does, so an expired claim is a failed
	// write and not a silent one.
	logPath := filepath.Join(t.TempDir(), "session.jsonl")
	log, err := session.Open(logPath)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	t.Cleanup(func() { log.Close() })
	opts := testOptions(t, tr, &echoTool{}, log)
	compactor := &slowCompactor{delay: 150 * time.Millisecond}
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	poo.SetRecordWriteBudget(s, 50*time.Millisecond)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	records, err := log.ReadRecords(context.Background())
	if err != nil {
		t.Fatalf("reading the log back: %v", err)
	}
	var compaction *poo.CompactionRecord
	for _, rec := range records {
		if cr, ok := rec.(*poo.CompactionRecord); ok {
			compaction = cr
		}
	}
	if compaction == nil {
		t.Fatal("the run holds no compaction record: the write outlived its claim's budget and was lost")
	}
	if compaction.Strategy != "slow" {
		t.Errorf("the compaction's strategy is %q, want slow", compaction.Strategy)
	}
	// The view the next request carried is the reconstruction, not the
	// transcript the compaction cut.
	last := tr.Requests()[len(tr.Requests())-1]
	text := ""
	for _, m := range last.Messages {
		text += m.Text()
	}
	if !strings.Contains(text, "the work continues") {
		t.Errorf("the last request does not carry the reconstruction")
	}
	if strings.Contains(text, big) {
		t.Errorf("the last request still carries the transcript the compaction cut")
	}
}
