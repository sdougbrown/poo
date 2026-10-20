package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Metrics is everything the harness measures about one run.
//
// Every field comes from the session log. The harness never looks at the
// model's own claims about what it did; it reads what actually happened.
type Metrics struct {
	// Turns is the number of provider requests the run made.
	Turns int `json:"turns"`
	// WallMS is the run's wall time in milliseconds.
	WallMS float64 `json:"wall_ms"`
	// InputTokens and OutputTokens are the endpoint's own counts, summed
	// over the run's requests.
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// CacheReadTokens is the number of prompt tokens the endpoint served
	// from its cache. It is nil when the endpoint reported no cache
	// counts at all.
	CacheReadTokens *int `json:"cache_read_tokens,omitempty"`
	// CacheHitRatio is the cached share of the prompt tokens, in [0,1].
	// It is nil when the endpoint reported no cache counts.
	CacheHitRatio *float64 `json:"cache_hit_ratio,omitempty"`

	// TTFTMS is the mean time to the first token over the run's
	// requests.
	TTFTMS float64 `json:"ttft_ms"`
	// TTFTTrend is the slope of the time to the first token against the
	// turn number, in milliseconds per turn. A positive slope means the
	// endpoint slows down as the run goes on.
	TTFTTrend float64 `json:"ttft_trend_ms_per_turn"`
	// TTFTGrowsWithPrefix reports whether the trend is both positive and
	// large enough to matter: a positive slope of at least a tenth of
	// the mean TTFT.
	TTFTGrowsWithPrefix bool `json:"ttft_grows_with_prefix"`

	// ToolCalls is the number of tool calls the model made.
	ToolCalls int `json:"tool_calls"`
	// ToolErrors is the number of tool calls that failed.
	ToolErrors int `json:"tool_errors"`
	// ToolErrorRate is the failed share of the tool calls, in [0,1].
	ToolErrorRate float64 `json:"tool_error_rate"`
	// TextParseFailures is the number of tool calls whose arguments were
	// not valid JSON, which is where a text-mode call that did not parse
	// shows up.
	TextParseFailures int `json:"text_parse_failures"`
	// TextParseRate is the unparsable share of the tool calls, in [0,1].
	TextParseRate float64 `json:"text_parse_rate"`

	// Rereads is the number of file ranges the run read more than once.
	Rereads int `json:"rereads"`
	// RereadRate is the re-read share of the reads, in [0,1].
	RereadRate float64 `json:"reread_rate"`
	// Reads is the number of reads the run made, through the read tool
	// or an obvious shell read.
	Reads int `json:"reads"`

	// SettleReason is why the run settled.
	SettleReason string `json:"settle_reason"`
	// SettleSource names the condition that settled the run.
	SettleSource string `json:"settle_source"`

	// ChangedFiles are the paths the run changed, relative to the
	// workdir.
	ChangedFiles []string `json:"changed_files,omitempty"`
	// UnexpectedFiles are the changed paths that are outside the task's
	// expected_paths.
	UnexpectedFiles []string `json:"unexpected_files,omitempty"`
	// DiffLines is the size of the diff, in changed lines.
	DiffLines int `json:"diff_lines"`
}

// ComputeMetrics measures one run from its session log.
//
// The diff is measured separately, because it needs the workdir, and it is
// passed in as a Diff.
func ComputeMetrics(log *Log, diff Diff) Metrics {
	m := Metrics{
		SettleReason: "unknown",
		ChangedFiles: diff.Files,
		DiffLines:    diff.Lines,
	}

	// The settle reason.
	if s := log.Settle(); s != nil {
		m.SettleReason = s.Reason
		m.SettleSource = s.Details.Source
	}

	m.Turns = log.Turns()
	m.WallMS = log.WallMS()

	// The token accounting and the time to the first token.
	var ttfts []float64
	var ttftTurns []float64
	var cacheReported bool
	var cacheRead int
	for _, u := range log.Usages() {
		m.InputTokens += u.InputTokens
		m.OutputTokens += u.OutputTokens
		if u.CacheReadTokens != nil {
			cacheReported = true
			cacheRead += *u.CacheReadTokens
		}
		ttfts = append(ttfts, u.TTFTMS)
		ttftTurns = append(ttftTurns, float64(u.Turn))
	}
	if len(ttfts) > 0 {
		var sum float64
		for _, v := range ttfts {
			sum += v
		}
		m.TTFTMS = sum / float64(len(ttfts))
		m.TTFTTrend = slope(ttftTurns, ttfts)
		m.TTFTGrowsWithPrefix = m.TTFTTrend > 0 && m.TTFTTrend >= 0.1*m.TTFTMS
	}
	if cacheReported {
		total := m.InputTokens
		if total <= 0 {
			total = cacheRead
		}
		ratio := float64(cacheRead) / float64(total)
		if ratio > 1 {
			ratio = 1
		}
		m.CacheReadTokens = &cacheRead
		m.CacheHitRatio = &ratio
	}

	// The tool calls and their outcomes.
	calls := log.ToolCalls()
	m.ToolCalls = len(calls)
	for _, r := range log.ToolResults() {
		if r.Outcome == "error" {
			m.ToolErrors++
		}
	}
	if m.ToolCalls > 0 {
		m.ToolErrorRate = float64(m.ToolErrors) / float64(m.ToolCalls)
	}

	// A call whose arguments did not parse is a text-mode failure: the
	// model wrote a call the parser could not read.
	for _, c := range calls {
		if !validArgs(c.Arguments) {
			m.TextParseFailures++
		}
	}
	if m.ToolCalls > 0 {
		m.TextParseRate = float64(m.TextParseFailures) / float64(m.ToolCalls)
	}

	// The re-read rate.
	reads := collectReads(calls)
	m.Reads = len(reads)
	m.Rereads, m.RereadRate = rereadStats(reads)

	// The paths changed outside the task's expected paths.
	return m
}

// Diff is what a run changed in the workdir.
type Diff struct {
	// Files are the changed paths, relative to the workdir.
	Files []string `json:"files"`
	// Lines is the size of the diff, in changed lines.
	Lines int `json:"lines"`
}

// read is one file range the run read.
type read struct {
	// path is the file the run read.
	path string
	// start and end are the line range, 1-indexed and inclusive.
	start, end int
	// whole reports that the read had no explicit range.
	whole bool
}

// collectReads returns the file ranges the run read.
//
// A read through the read tool is a read of a line range. A read through
// bash is one of the obvious shell reads: cat, head, tail, sed -n. A
// command that greps a file is a search, not a read, and does not count.
func collectReads(calls []LogRecord) []read {
	var out []read
	for _, c := range calls {
		switch c.Tool {
		case "read":
			path, offset, limit, ok := readArgs(c.Arguments)
			if !ok {
				continue
			}
			out = append(out, read{path: path, start: offset, end: offset + limit - 1, whole: offset == 1 && limit >= 2000})
		case "bash":
			command, ok := stringArg(c.Arguments, "command")
			if !ok {
				continue
			}
			out = append(out, shellReads(command)...)
		}
	}
	return out
}

// readArgs reads a read tool call's arguments.
func readArgs(args json.RawMessage) (path string, offset, limit int, ok bool) {
	var a struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", 0, 0, false
	}
	if a.Path == "" {
		return "", 0, 0, false
	}
	offset = a.Offset
	if offset < 1 {
		offset = 1
	}
	limit = a.Limit
	if limit <= 0 {
		limit = 2000
	}
	return a.Path, offset, limit, true
}

// stringArg reads a string field out of a call's arguments.
func stringArg(args json.RawMessage, key string) (string, bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(args, &m); err != nil {
		return "", false
	}
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// catPattern matches a shell read of a whole file.
var catPattern = regexp.MustCompile(`(?:^|[;&|]\s*|\s)cat\s+(?:--\s+)?([^\s;|&]+)`)

// headPattern matches a shell read of the head of a file.
var headPattern = regexp.MustCompile(`(?:^|[;&|]\s*|\s)head\s+(?:-[A-Za-z0-9]+\s+)*(?:-[A-Za-z0-9]+\s+)*([^\s;|&]+)`)

// tailPattern matches a shell read of the tail of a file.
var tailPattern = regexp.MustCompile(`(?:^|[;&|]\s*|\s)tail\s+(?:-[A-Za-z0-9]+\s+)*([^\s;|&]+)`)

// sedPattern matches a shell read of a line range.
var sedPattern = regexp.MustCompile(`(?:^|[;&|]\s*|\s)sed\s+(?:[^\s;&|]*\s+)*?-n\s+(?:[^\s;&|]*?\s+)*?['"]?(\d+)(?:,(\d+))?p`)

// shellReads returns the file ranges a shell command reads.
//
// The patterns are deliberately narrow: they cover the shapes the model
// uses to pull a file into the context, and nothing else. A command that
// greps is a search, and a command that builds is not a read.
func shellReads(command string) []read {
	var out []read
	for _, m := range catPattern.FindAllStringSubmatch(command, -1) {
		out = append(out, read{path: m[1], whole: true})
	}
	for _, m := range headPattern.FindAllStringSubmatch(command, -1) {
		out = append(out, read{path: m[1], whole: true})
	}
	for _, m := range tailPattern.FindAllStringSubmatch(command, -1) {
		out = append(out, read{path: m[1], whole: true})
	}
	for _, m := range sedPattern.FindAllStringSubmatch(command, -1) {
		start, _ := strconv.Atoi(m[1])
		end := start
		if m[2] != "" {
			end, _ = strconv.Atoi(m[2])
		}
		out = append(out, read{path: m[1], start: start, end: end})
	}
	return out
}

// rereadStats counts the file ranges the run read more than once.
//
// A range read twice is a re-read. A whole-file read that overlaps an
// earlier range read of the same file is a re-read too, because the model
// asked for the same content twice.
func rereadStats(reads []read) (int, float64) {
	if len(reads) == 0 {
		return 0, 0
	}
	counts := map[string]int{}
	for _, r := range reads {
		counts[readKey(r)]++
	}
	rereads := 0
	for _, n := range counts {
		if n > 1 {
			rereads += n - 1
		}
	}
	return rereads, float64(rereads) / float64(len(reads))
}

// readKey returns the key a read is counted under.
//
// The key is the path plus the range, so two reads of the same range
// collide and two reads of different ranges do not. A whole-file read
// covers every range, so it is keyed by the path alone.
func readKey(r read) string {
	if r.whole {
		return "file:" + r.path
	}
	return fmt.Sprintf("range:%s:%d-%d", r.path, r.start, r.end)
}

// slope returns the slope of y against x, by the least-squares fit.
//
// A single point has no slope, and the answer is zero.
func slope(xs, ys []float64) float64 {
	n := float64(len(xs))
	if n < 2 || len(ys) != len(xs) {
		return 0
	}
	var sumX, sumY float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
	}
	meanX := sumX / n
	meanY := sumY / n
	var num, den float64
	for i := range xs {
		dx := xs[i] - meanX
		num += dx * (ys[i] - meanY)
		den += dx * dx
	}
	if den == 0 {
		return 0
	}
	return num / den
}

// validArgs reports whether a call's arguments are a JSON object.
//
// The log records malformed arguments as a JSON string holding the raw
// text, so an argument that is not an object is a call the model did not
// get right.
func validArgs(args json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" {
		return false
	}
	if trimmed[0] != '{' {
		return false
	}
	return json.Valid([]byte(trimmed))
}

// parseTS parses a record's timestamp.
func parseTS(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// mean returns the mean of a list of numbers, or zero when it is empty.
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// stdev returns the sample standard deviation of a list of numbers.
//
// A single value has no spread, and the answer is zero.
func stdev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	m := mean(values)
	var sum float64
	for _, v := range values {
		d := v - m
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(values)-1))
}

// round2 rounds a number to two decimals.
//
// The report holds small numbers, and rounding keeps the JSON stable
// between two runs of the same thing.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// sortedStrings sorts a list of strings in place and returns it.
func sortedStrings(in []string) []string {
	sort.Strings(in)
	return in
}
