package config_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/feed"
	"github.com/sdougbrown/poo/pootest"
)

// The filter fan-out: one record in may become any number of records out,
// so a filter adapts a source whose shape the session does not know.

// queuedRecords returns the feed inputs the session queued that are a
// filter's records, leaving out the report a fault wakes the session with.
func queuedRecords(sink *pootest.Sink) []*poo.InputQueuedRecord {
	var out []*poo.InputQueuedRecord
	for _, rec := range queuedFeedInputs(sink) {
		if !strings.Contains(rec.Text, ` fault="`) {
			out = append(out, rec)
		}
	}
	return out
}

// TestFilterFanOutCompact covers the fan-out itself: a filter that writes
// three one-line records has all three delivered, in the order it wrote
// them.
func TestFilterFanOutCompact(t *testing.T) {
	_, _, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "filterless-data")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-fanout")}},
	})
	waitUntil(t, "the filter's three records to be queued", func() bool {
		return len(queuedRecords(sink)) >= 3
	})
	queued := queuedRecords(sink)
	want := []string{
		`<feed name="chat" from="alice">the first message</feed>`,
		`<feed name="chat" from="alice">the second message</feed>`,
		`<feed name="chat" from="alice">the third message</feed>`,
	}
	if len(queued) != len(want) {
		t.Fatalf("the session queued %d records, want the filter's three", len(queued))
	}
	for i, text := range want {
		if queued[i].Text != text {
			t.Errorf("the record %d is %q, want %q", i, queued[i].Text, text)
		}
	}
}

// TestFilterFanOutPretty covers the decoder's stream: jq's default output
// spreads one record over several lines, and the record is still one.
func TestFilterFanOutPretty(t *testing.T) {
	_, _, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "filterless-data")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-fanout-pretty")}},
	})
	waitUntil(t, "the pretty-printed record to be queued", func() bool {
		return len(queuedRecords(sink)) >= 1
	})
	queued := queuedRecords(sink)
	if len(queued) != 1 {
		t.Fatalf("the session queued %d records, want the one", len(queued))
	}
	want := `<feed name="chat" from="alice">the pretty message</feed>`
	if queued[0].Text != want {
		t.Errorf("the record is %q, want %q", queued[0].Text, want)
	}
}

// TestFilterFanOutAtomicOnBadRecord covers the all-or-nothing rule: a
// record, a value that is not one, and a record again deliver nothing, and
// the attempt ends with the output fault.
func TestFilterFanOutAtomicOnBadRecord(t *testing.T) {
	_, _, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "filterless-data")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-fanout-bad")}},
	})
	waitUntil(t, "the fault report to reach the chat model", func() bool {
		return deliveredFeedMessage(sink) != nil
	})
	text := deliveredFeedMessage(sink).Content[0].Text
	if !strings.Contains(text, `fault="`+poo.FeedFaultOutput+`"`) {
		t.Errorf("the fault report %q does not name the output fault", text)
	}
	if queued := queuedRecords(sink); len(queued) != 0 {
		t.Fatalf("the session queued %d records, want none: one value was not a record", len(queued))
	}
	if ends := feedEndsOf(sink); len(ends) != 1 || ends[0].Outcome != poo.FeedSuspended {
		t.Fatalf("the attempts ended %+v, want one suspended attempt", ends)
	}
}

// TestFilterFanOutCap covers the caps one run's output runs under: more
// records than a run may write, and one record past the bytes a record may
// be. Each is a fault, and each delivers none of the records before it.
func TestFilterFanOutCap(t *testing.T) {
	cases := []struct {
		// name names the case.
		name string
		// filter is the fixture filter that trips the cap.
		filter string
		// want is the fault's code.
		want string
	}{
		{name: "more records than one run may write", filter: "filter-fanout-cap", want: poo.FeedFaultOutput},
		{name: "a record past the bytes one may be", filter: "filter-fanout-wide", want: poo.FeedFaultRecord},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
				"command": []string{feedScript(t, "filterless-data")},
				"on_idle": "wake",
				"filter":  map[string]any{"command": []string{filterScript(t, c.filter)}},
			})
			waitUntil(t, "the fault report to reach the chat model", func() bool {
				return deliveredFeedMessage(sink) != nil
			})
			text := deliveredFeedMessage(sink).Content[0].Text
			if !strings.Contains(text, `fault="`+c.want+`"`) {
				t.Errorf("the fault report %q does not name the %s fault", text, c.want)
			}
			if queued := queuedRecords(sink); len(queued) != 0 {
				t.Fatalf("the session queued %d records, want none: the run wrote past a cap", len(queued))
			}
		})
	}
}

// TestFilterFanOutOverOneMiBTotal covers the total an output may reach
// below the run cap: three records that fit individually are all delivered,
// though their sum is past the 1 MiB a single record may be.
func TestFilterFanOutOverOneMiBTotal(t *testing.T) {
	_, _, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "filterless-data")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-fanout-large")}},
	})
	waitUntil(t, "the filter's three large records to be queued", func() bool {
		return len(queuedRecords(sink)) >= 3
	})
	queued := queuedRecords(sink)
	if len(queued) != 3 {
		t.Fatalf("the session queued %d records, want the filter's three", len(queued))
	}
	total := 0
	for _, rec := range queued {
		total += len(rec.Text)
	}
	if total <= feed.MaxLineBytes {
		t.Fatalf("the records total %d bytes, want more than the %d one record may be", total, feed.MaxLineBytes)
	}
}

// TestFilterOutputTotalCap covers one run's total: five records that fit
// individually are past the bytes one run may write, so none of them is
// delivered and the output fault is the one that suspends the feed.
func TestFilterOutputTotalCap(t *testing.T) {
	_, _, sink, _, _ := filterFeedSession(t, "chat", map[string]any{
		"command": []string{feedScript(t, "filterless-data")},
		"on_idle": "wake",
		"filter":  map[string]any{"command": []string{filterScript(t, "filter-fanout-total")}},
	})
	waitUntil(t, "the fault report to reach the chat model", func() bool {
		return deliveredFeedMessage(sink) != nil
	})
	text := deliveredFeedMessage(sink).Content[0].Text
	if !strings.Contains(text, `fault="`+poo.FeedFaultOutput+`"`) {
		t.Errorf("the fault report %q does not name the output fault", text)
	}
	if queued := queuedRecords(sink); len(queued) != 0 {
		t.Fatalf("the session queued %d records, want none: the run wrote past its cap", len(queued))
	}
}
