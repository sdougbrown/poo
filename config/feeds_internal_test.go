package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/feed"
)

// TestTheFeedIdlePoliciesMapEveryValue covers the mapping the manifest's
// on_idle goes through: every value the loader accepts has a policy in
// the core, and a value neither side names is a failure.
func TestTheFeedIdlePoliciesMapEveryValue(t *testing.T) {
	cases := []struct {
		// value is what the manifest says.
		value string
		// want is the policy it maps to.
		want poo.OnIdle
		// wantErr reports that the value maps to nothing.
		wantErr bool
	}{
		{value: "", want: ""},
		{value: feed.OnIdleWake, want: poo.OnIdleWake},
		{value: feed.OnIdleHold, want: poo.OnIdleHold},
		{value: "sometimes", wantErr: true},
	}
	for _, c := range cases {
		got, err := onIdleOf(c.value)
		if c.wantErr {
			if err == nil {
				t.Errorf("the on_idle %q mapped to %q, want a failure", c.value, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("the on_idle %q failed: %v", c.value, err)
			continue
		}
		if got != c.want {
			t.Errorf("the on_idle %q maps to %q, want %q", c.value, got, c.want)
		}
	}
}

// TestTheLimitedBufferKeepsWhatFits covers the buffer's contract: it
// keeps the bytes that fit, records that the rest were dropped, and
// never fails a write. The truncation at the limit is the cap the
// filter's stderr and the filter's output run under.
func TestTheLimitedBufferKeepsWhatFits(t *testing.T) {
	cases := []struct {
		// name names the case.
		name string
		// limit is how many bytes the buffer keeps. Zero means the whole
		// output.
		limit int
		// writes are the bytes the process writes, in order.
		writes [][]byte
		// wantLen is how many bytes the buffer keeps.
		wantLen int
		// wantOver reports that the output went past the limit.
		wantOver bool
	}{
		{name: "fit", limit: 8, writes: [][]byte{[]byte("abc")}, wantLen: 3, wantOver: false},
		{name: "exact boundary", limit: 8, writes: [][]byte{[]byte("12345678")}, wantLen: 8, wantOver: false},
		{name: "truncation", limit: 8, writes: [][]byte{[]byte("1234567890")}, wantLen: 8, wantOver: true},
		{name: "truncation across writes", limit: 8, writes: [][]byte{[]byte("1234"), []byte("567890")}, wantLen: 8, wantOver: true},
		{name: "zero limit keeps everything", limit: 0, writes: [][]byte{[]byte("abc")}, wantLen: 3, wantOver: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := &limitedBuffer{limit: c.limit}
			for _, w := range c.writes {
				n, err := buf.Write(w)
				if err != nil {
					t.Fatalf("the write failed: %v", err)
				}
				if n != len(w) {
					t.Errorf("the write reported %d bytes, want %d", n, len(w))
				}
			}
			if got := len(buf.bytes()); got != c.wantLen {
				t.Errorf("the buffer holds %d bytes, want %d", got, c.wantLen)
			}
			if buf.overflowed() != c.wantOver {
				t.Errorf("the buffer overflowed %v, want %v", buf.overflowed(), c.wantOver)
			}
		})
	}
}

// TestTheFilterOutputRefusesWhatIsNotARecord covers the output's
// refusals: a record with no text, an image with no path, and a data
// that is not an object are all the fault that suspends the feed.
func TestTheFilterOutputRefusesWhatIsNotARecord(t *testing.T) {
	cases := []struct {
		// name names the case.
		name string
		// out is what the filter wrote.
		out string
		// wantErr reports that the output is refused.
		wantErr bool
		// wantErrText is the error's text, when it is refused.
		wantErrText string
	}{
		{name: "a record with no text", out: `{"from":"alice"}`, wantErr: true, wantErrText: "the filter's record has no text"},
		{name: "an image with no path", out: `{"image":{}}`, wantErr: true, wantErrText: "the filter's record names an image with no path"},
		{name: "a data that is not an object", out: `{"text":"the message","data":[1,2]}`, wantErr: true, wantErrText: "the filter's record has a data that is not an object"},
		{name: "a good record", out: `{"text":"the message","from":"alice"}`, wantErr: false},
		{name: "a record with a data object", out: `{"text":"the message","data":{"kind":"note"}}`, wantErr: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, delivered, err := filterOutput([]byte(c.out))
			if c.wantErr {
				if err == nil {
					t.Fatalf("the output %q was not refused", c.out)
				}
				if !strings.Contains(err.Error(), c.wantErrText) {
					t.Errorf("the error is %q, want it to say %q", err.Error(), c.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("the output %q was refused: %v", c.out, err)
			}
			if !delivered {
				t.Errorf("the output %q was not delivered", c.out)
			}
		})
	}
}

// TestAFilterNullDataIsAbsentData covers the shape jq writes for a record
// with no data: `{text, from, data}` emits a null data, and the record is
// delivered without data rather than faulting the feed.
func TestAFilterNullDataIsAbsentData(t *testing.T) {
	message, delivered, err := filterOutput([]byte(`{"text":"the message","from":"alice","data":null}`))
	if err != nil {
		t.Fatalf("the output was refused: %v", err)
	}
	if !delivered {
		t.Fatal("the output was dropped")
	}
	if message.Data != nil {
		t.Errorf("the record's data is %q, and a null data is no data", message.Data)
	}
}

// TestTheFilterRecordDoesNotEscapeItsWayPastTheLineCap covers the record a
// filter reads: characters that JSON may escape as HTML stay as they are,
// so an accepted record does not grow past the cap on its way to the
// filter.
func TestTheFilterRecordDoesNotEscapeItsWayPastTheLineCap(t *testing.T) {
	// 180 KiB of ampersands fits the feed's 1 MiB cap and does not fit it
	// once each one is escaped to \u0026.
	text := strings.Repeat("&", 180<<10)
	line, err := filterRecord("alice", text, nil)
	if err != nil {
		t.Fatalf("the record was refused: %v", err)
	}
	if len(line) > feed.MaxLineBytes {
		t.Fatalf("the record is %d bytes, and a record is at most %d", len(line), feed.MaxLineBytes)
	}
	var record filterMessage
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("the record is not JSON: %v", err)
	}
	if record.Text != text || record.From != "alice" {
		t.Errorf("the record round trip lost the message: %d bytes of text, from %q", len(record.Text), record.From)
	}
}
