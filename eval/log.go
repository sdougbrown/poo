package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// LogRecord is one record of a session log, decoded into the fields the
// harness measures.
//
// The harness decodes into a loose shape: a session log grows a field
// every stage, and a consumer that fails on an unknown field breaks on a
// newer log. Only the fields the metrics need are typed.
type LogRecord struct {
	// Record is the record's name, e.g. "tool.call".
	Record string `json:"record"`
	// Seq is the record's sequence number.
	Seq int64 `json:"seq"`
	// TS is the record's timestamp.
	TS string `json:"ts"`

	// The session's own fields.
	SessionID    string `json:"session_id"`
	Format       string `json:"format"`
	WorkingDir   string `json:"working_dir"`
	ContextLimit int    `json:"context_limit"`
	ToolsMode    string `json:"tools_mode"`

	// The run's fields.
	RunID   string `json:"run_id"`
	Turn    int    `json:"turn"`
	Reason  string `json:"reason"`
	Turns   int    `json:"turns"`
	Details struct {
		Source string `json:"source"`
		Text   string `json:"text"`
	} `json:"detail"`

	// The message's fields.
	Origin  string `json:"origin"`
	Role    string `json:"role"`
	Content []struct {
		Kind       string          `json:"kind"`
		Text       string          `json:"text"`
		ToolCallID string          `json:"tool_call_id"`
		Name       string          `json:"name"`
		Arguments  json.RawMessage `json:"arguments"`
		IsError    bool            `json:"is_error"`
	} `json:"content"`

	// The tool call's and tool result's fields.
	Handle    string          `json:"handle"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Outcome   string          `json:"outcome"`
	Text      string          `json:"text"`

	// The usage record's fields.
	Request         int     `json:"request"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	CacheReadTokens *int    `json:"cache_read_tokens"`
	TTFTMS          float64 `json:"ttft_ms"`
}

// Log is a decoded session log.
type Log struct {
	// Path is where the log was read from.
	Path string
	// Records are the records, in the order they were written.
	Records []LogRecord
	// Torn reports that the log's last line did not decode, which
	// happens when a run is killed mid-write.
	Torn bool
}

// ParseLog reads a session log.
//
// A record that does not decode is an error: a metric computed from a
// half-read log is worse than no metric. The one exception is the last
// line: a run that was killed mid-write leaves a partial record there,
// and the records before it are good. The tail is not kept as a record
// — it cannot be trusted — but the log is marked Torn, so a reader can
// see that the run did not finish.
func ParseLog(path string) (*Log, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("eval: opening %s: %w", path, err)
	}
	defer f.Close()

	log := &Log{Path: path}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	line := 0
	// A line is held back until the next one arrives, so a malformed
	// line can be told from the last one: a run that was killed
	// mid-write leaves a partial record there, and the records before it
	// are good.
	var pending string
	pendingLine := 0
	flush := func(text string, at int, last bool) error {
		if text == "" {
			return nil
		}
		rec, err := decodeRecord(text)
		if err != nil {
			if last {
				// The last line did not decode. A run that was
				// killed mid-write leaves one there, and the records
				// before it are good, so the tail is kept as it was
				// found.
				log.Torn = true
				return nil
			}
			return fmt.Errorf("eval: %s line %d: %w", path, at, err)
		}
		log.Records = append(log.Records, rec)
		return nil
	}
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		if err := flush(pending, pendingLine, false); err != nil {
			return nil, err
		}
		pending = text
		pendingLine = line + 1
		line++
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("eval: reading %s: %w", path, err)
	}
	if err := flush(pending, pendingLine, true); err != nil {
		return nil, err
	}
	if len(log.Records) == 0 {
		return nil, fmt.Errorf("eval: %s holds no records", path)
	}
	return log, nil
}

// decodeRecord decodes one line of a session log.
func decodeRecord(text string) (LogRecord, error) {
	var rec LogRecord
	if err := json.Unmarshal([]byte(text), &rec); err != nil {
		return LogRecord{}, err
	}
	return rec, nil
}

// ToolCalls returns the tool calls the run made, in order.
func (l *Log) ToolCalls() []LogRecord {
	var out []LogRecord
	for _, r := range l.Records {
		if r.Record == "tool.call" {
			out = append(out, r)
		}
	}
	return out
}

// ToolResults returns the tool results the run produced, in order.
func (l *Log) ToolResults() []LogRecord {
	var out []LogRecord
	for _, r := range l.Records {
		if r.Record == "tool.result" {
			out = append(out, r)
		}
	}
	return out
}

// Usages returns the usage records, in order.
func (l *Log) Usages() []LogRecord {
	var out []LogRecord
	for _, r := range l.Records {
		if r.Record == "usage" {
			out = append(out, r)
		}
	}
	return out
}

// Settle returns the run's settle record, or nil when the run did not
// settle.
func (l *Log) Settle() *LogRecord {
	for i := len(l.Records) - 1; i >= 0; i-- {
		if l.Records[i].Record == "run.settled" {
			return &l.Records[i]
		}
	}
	return nil
}

// ContextLimit returns the context window the session ran under.
//
// The session's own record holds it. A log that does not name one has
// no limit to report, and the answer is zero.
func (l *Log) ContextLimit() int {
	for _, r := range l.Records {
		if r.Record == "session.created" && r.ContextLimit > 0 {
			return r.ContextLimit
		}
	}
	return 0
}

// SettleTurns returns the number of turns the run made, as the settle
// record says.
func (l *Log) SettleTurns() int {
	if s := l.Settle(); s != nil {
		return s.Turns
	}
	return 0
}

// FinalMessage returns the last text the model produced, which is the
// answer a read-only task is judged on.
func (l *Log) FinalMessage() string {
	var b strings.Builder
	found := false
	for _, r := range l.Records {
		if r.Record != "message" || r.Origin != "model" {
			continue
		}
		if found {
			b.Reset()
		}
		found = true
		for _, c := range r.Content {
			if c.Kind == "text" {
				b.WriteString(c.Text)
			}
		}
	}
	return b.String()
}

// TranscriptSegment is one turn of a transcript.
//
// The segments are the unit the judge elides: a transcript too large for
// the judge keeps its first and last segments and drops the middle.
type TranscriptSegment struct {
	// Turn is the turn the segment covers.
	Turn int `json:"turn"`
	// Text is the segment's text.
	Text string `json:"text"`
}

// TranscriptSegments renders the log as a transcript, one segment per
// turn.
//
// The transcript is the conversation the model saw: the messages, the
// tool calls with their arguments, and the tool results. It is plain
// text, so a judge reads it the same way a person would.
func (l *Log) TranscriptSegments() []TranscriptSegment {
	// The segments are keyed by turn, so the events of one turn stay
	// together and the order of the turns is the order of the run.
	order := []int{}
	byTurn := map[int]*strings.Builder{}
	appendTo := func(turn int, text string) {
		b, ok := byTurn[turn]
		if !ok {
			b = &strings.Builder{}
			byTurn[turn] = b
			order = append(order, turn)
		}
		b.WriteString(text)
	}

	for _, r := range l.Records {
		switch r.Record {
		case "message":
			switch r.Origin {
			case "system":
				appendTo(r.Turn, "[system prompt]\n")
			case "tool":
				// The tool result is recorded as a message too, and
				// the tool.result record already carries it.
				continue
			}
			label := "user"
			if r.Origin == "model" {
				label = "assistant"
			}
			for _, c := range r.Content {
				switch c.Kind {
				case "text":
					if c.Text == "" {
						continue
					}
					appendTo(r.Turn, fmt.Sprintf("turn %d %s: %s\n", r.Turn, label, c.Text))
				case "tool_call":
					appendTo(r.Turn, fmt.Sprintf("turn %d %s calls %s %s\n", r.Turn, label, c.Name, string(c.Arguments)))
				}
			}
		case "tool.result":
			text := r.Text
			// The cut ends on a character boundary: a byte split would
			// put invalid UTF-8 into the judge's prompt.
			text = prefix(text, 2000) + " …"
			appendTo(r.Turn, fmt.Sprintf("turn %d tool %s (%s): %s\n", r.Turn, r.Tool, r.Outcome, text))
		}
	}

	sort.Ints(order)
	out := make([]TranscriptSegment, 0, len(order))
	for _, turn := range order {
		out = append(out, TranscriptSegment{Turn: turn, Text: byTurn[turn].String()})
	}
	return out
}

// Turns counts the provider requests the run made.
//
// The settle record counts them, and the usage records count them
// independently: the settle record is missing when the process died
// mid-run, and the usage records are what survived.
func (l *Log) Turns() int {
	if s := l.Settle(); s != nil && s.Turns > 0 {
		return s.Turns
	}
	return len(l.Usages())
}

// WallMS returns the run's wall time in milliseconds.
//
// The run's own clock is the difference between the first and the last
// record's timestamp.
func (l *Log) WallMS() float64 {
	if len(l.Records) < 2 {
		return 0
	}
	first, err := parseTS(l.Records[0].TS)
	if err != nil {
		return 0
	}
	last, err := parseTS(l.Records[len(l.Records)-1].TS)
	if err != nil {
		return 0
	}
	return float64(last.Sub(first).Milliseconds())
}

// sortedKeys returns a map's keys, sorted, so a report's output is
// stable.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedInts returns a map's integer keys, sorted, so a report's output
// is stable.
func sortedInts[V any](m map[int]V) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
