package repl

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sdougbrown/poo/wire/protocol"
)

// aBashObservation renders one bash tool result the way the wire
// carries it: the model-facing handle line, then the observation
// envelope.
func aBashObservation(t *testing.T, fields map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("encoding the observation: %v", err)
	}
	return "[r1 · bash]\n" + string(raw)
}

// plainLines is what a rendered preview reads after the ANSI the
// transcript carries for a terminal is taken off.
func plainLines(rendered string) []string {
	lines := strings.Split(ansi.Strip(rendered), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// TestABashObservationReadsAsItsOutput covers the decode: the
// envelope's JSON gives way to the command's own lines, and the
// handle line the model sees stays.
func TestABashObservationReadsAsItsOutput(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolResult("bash", "ok", aBashObservation(t, map[string]any{
		"returncode": 0, "timed_out": false, "output": "one\ntwo\n",
	}), 0)

	rendered := ansi.Strip(out.String())
	for _, want := range []string{"[r1 · bash]", "one", "two"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the result lacks %q:\n%s", want, rendered)
		}
	}
	for _, unwanted := range []string{"returncode", `\n`, "timed_out", "{"} {
		if strings.Contains(rendered, unwanted) {
			t.Errorf("the result kept the envelope's %q:\n%s", unwanted, rendered)
		}
	}
}

// TestABashFailureKeepsItsExitStatus covers a non-zero command: the
// output reads, and the exit status stays visible.
func TestABashFailureKeepsItsExitStatus(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolResult("bash", "error", aBashObservation(t, map[string]any{
		"returncode": 2, "timed_out": false, "output": "missing\n",
	}), 0)

	rendered := ansi.Strip(out.String())
	for _, want := range []string{"missing", "exit 2", "bash: error"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the failure lacks %q:\n%s", want, rendered)
		}
	}
}

// TestABashTimeoutNamesTheTimeout covers the null return code: a
// timed-out command has no exit status, and the timeout is what says
// it failed.
func TestABashTimeoutNamesTheTimeout(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolResult("bash", "error", aBashObservation(t, map[string]any{
		"returncode": nil, "timed_out": true, "output": "partial\n",
	}), 0)

	rendered := ansi.Strip(out.String())
	if !strings.Contains(rendered, "timed out") {
		t.Errorf("the timeout is not named:\n%s", rendered)
	}
	if strings.Contains(rendered, "exit") {
		t.Errorf("a timed-out command has an exit status:\n%s", rendered)
	}
}

// TestACancelledBashCallReadsAsCancelled covers a process the run
// killed: no return code and no timeout, with the outcome naming the
// cancellation.
func TestACancelledBashCallReadsAsCancelled(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolResult("bash", "cancelled", aBashObservation(t, map[string]any{
		"returncode": nil, "timed_out": false, "output": "started\n",
	}), 0)

	rendered := ansi.Strip(out.String())
	for _, want := range []string{"started", "killed", "bash: cancelled"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the cancellation lacks %q:\n%s", want, rendered)
		}
	}
}

// TestACappedBashObservationKeepsItsStatusAndHint covers the elided
// shape: the head, the tail, the character count and the tool's hint
// all read, and the exit status survives the cap.
func TestACappedBashObservationKeepsItsStatusAndHint(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolResult("bash", "error", aBashObservation(t, map[string]any{
		"returncode":   3,
		"timed_out":    false,
		"output_head":  "head line\n",
		"output_tail":  "tail line\n",
		"elided_chars": 1234,
		"hint":         "use head and tail",
	}), 0)

	rendered := ansi.Strip(out.String())
	for _, want := range []string{"head line", "tail line", "1234 characters elided", "use head and tail", "exit 3"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the capped result lacks %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "output_head") || strings.Contains(rendered, "elided_chars") {
		t.Errorf("the capped result kept the envelope's fields:\n%s", rendered)
	}
}

// TestACappedBashObservationKeepsItsStatusOverTheRowCap covers a
// capped body that fills the rows: the status reads even when the output
// does not all fit.
func TestACappedBashObservationKeepsItsStatusOverTheRowCap(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolResult("bash", "error", aBashObservation(t, map[string]any{
		"returncode":   4,
		"timed_out":    false,
		"output_head":  strings.Repeat("head line\n", 10),
		"output_tail":  "tail line\n",
		"elided_chars": 99,
	}), 0)

	rendered := ansi.Strip(out.String())
	for _, want := range []string{"+4 more lines", "99 characters elided", "tail line", "exit 4"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the capped result lacks %q:\n%s", want, rendered)
		}
	}
}

// TestAWireElidedCountReadsAsCharacters covers the event's own
// elided_chars: the wire counts characters, so the transcript says
// characters rather than calling the count lines.
func TestAWireElidedCountReadsAsCharacters(t *testing.T) {
	var out bytes.Buffer
	m := &machine{out: &out, r: newRenderer(&out), status: newStatus("")}
	m.event(protocol.Event{Type: "tool.result", Payload: map[string]any{
		"run_id": "r1", "tool": "bash", "outcome": "ok",
		"text": "one line\n", "elided_chars": float64(250),
	}}, "r1", &runState{})

	rendered := ansi.Strip(out.String())
	if !strings.Contains(rendered, "250 characters elided") {
		t.Errorf("the wire's character count does not read:\n%s", rendered)
	}
	if strings.Contains(rendered, "250 more lines") {
		t.Errorf("the character count is labelled as lines:\n%s", rendered)
	}
}

// TestALongResultIsBoundedByTheTerminalWidth covers a single huge
// line: the preview ends at the terminal's edge rather than flooding
// the rows.
func TestALongResultIsBoundedByTheTerminalWidth(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.width = 40
	r.toolResult("grep", "ok", strings.Repeat("x", 500), 0)

	lines := plainLines(r.transcriptText())
	if len(lines) != 1 {
		t.Fatalf("the result reads as %d lines:\n%s", len(lines), r.transcriptText())
	}
	if width := ansi.StringWidth(lines[0]); width > 40 {
		t.Errorf("the line is %d columns wide, want at most 40: %q", width, lines[0])
	}
	if !strings.Contains(lines[0], "…") {
		t.Errorf("the clipped line does not say it was cut: %q", lines[0])
	}
}

// TestALongToolSummaryIsBounded covers the call's decoration: a
// summary on one long line ends at the terminal's edge.
func TestALongToolSummaryIsBounded(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.width = 40
	r.toolCall("bash", strings.Repeat("y", 500))

	lines := plainLines(r.transcriptText())
	if len(lines) != 1 {
		t.Fatalf("the call reads as %d lines:\n%s", len(lines), r.transcriptText())
	}
	if width := ansi.StringWidth(lines[0]); width > 40 {
		t.Errorf("the call is %d columns wide, want at most 40: %q", width, lines[0])
	}
	if !strings.Contains(lines[0], "…") {
		t.Errorf("the clipped summary does not say it was cut: %q", lines[0])
	}
}

// TestASourceResultKeepsItsIndentation covers a tool whose text is
// source: the leading spaces the source has stay, so the code reads
// as code.
func TestASourceResultKeepsItsIndentation(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolResult("read", "ok", "[a.go · lines 1–3 of 9]\nfunc f() {\n    return 1\n}\n", 0)

	rendered := ansi.Strip(out.String())
	if !strings.Contains(rendered, "\n      return 1") {
		t.Errorf("the source lost its indentation:\n%q", rendered)
	}
}

// TestAnUnknownResultFallsBackToItsText covers a result that is not a
// bash observation: its text reads as it came, unescaped and undecoded.
func TestAnUnknownResultFallsBackToItsText(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	text := `{"foo":"bar","note":"not a bash observation"}`
	r.toolResult("other", "ok", text, 0)

	rendered := ansi.Strip(out.String())
	if !strings.Contains(rendered, text) {
		t.Errorf("the unknown result was rewritten:\n%s", rendered)
	}
}

// TestAMalformedObservationFallsBackToItsText covers a cut-short,
// wrong-typed or output-less envelope: the original text reads.
func TestAMalformedObservationFallsBackToItsText(t *testing.T) {
	for _, envelope := range []string{
		`{"returncode":0,"timed_out":`,
		`{"returncode":"zero","timed_out":false,"output":"x"}`,
		`{"returncode":0,"timed_out":false,"output":null}`,
		`{"returncode":null,"timed_out":null,"output":"x"}`,
		`{"returncode":0,"timed_out":false,"note":"no output"}`,
	} {
		var out bytes.Buffer
		r := newRenderer(&out)
		r.toolResult("bash", "ok", "[r1 · bash]\n"+envelope, 0)

		rendered := ansi.Strip(out.String())
		if !strings.Contains(rendered, envelope) {
			t.Errorf("the malformed result %q was not kept:\n%s", envelope, rendered)
		}
	}
}

// TestABashObservationCannotDriveTheTerminal covers the sanitizing: a
// command's escape sequences, carriage returns and controls print as
// nothing rather than moving the cursor or erasing the transcript.
func TestABashObservationCannotDriveTheTerminal(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.width = 40
	r.toolResult("bash", "ok", aBashObservation(t, map[string]any{
		"returncode": 0, "timed_out": false,
		"output": "before\x1b[2Jafter\rrewritten\x00end\n",
	}), 0)

	rendered := r.transcriptText()
	for _, unwanted := range []string{"\x1b[2J", "\r", "\x00"} {
		if strings.Contains(rendered, unwanted) {
			t.Errorf("the transcript holds %q:\n%q", unwanted, rendered)
		}
	}
	if !strings.Contains(rendered, "beforeafterrewrittenend") {
		t.Errorf("the data did not read:\n%q", rendered)
	}
}

// TestADecodedResultStillKeepsTheRowCap covers the row bound on a
// decoded body: the transcript keeps the head and says how many lines
// it cut.
func TestADecodedResultStillKeepsTheRowCap(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("output line\n")
	}
	r.toolResult("bash", "ok", aBashObservation(t, map[string]any{
		"returncode": 0, "timed_out": false, "output": b.String(),
	}), 0)

	rendered := ansi.Strip(out.String())
	if !strings.Contains(rendered, "+12 more lines") {
		t.Errorf("the result does not say what it cut:\n%s", rendered)
	}
}

// TestAWideRuneResultIsBoundedByVisualWidth covers characters wider
// than a column: the preview ends at the terminal's edge in columns.
func TestAWideRuneResultIsBoundedByVisualWidth(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.width = 20
	r.toolResult("grep", "ok", strings.Repeat("世", 100), 0)

	lines := plainLines(r.transcriptText())
	if len(lines) != 1 {
		t.Fatalf("the result reads as %d lines:\n%s", len(lines), r.transcriptText())
	}
	if width := ansi.StringWidth(lines[0]); width > 20 {
		t.Errorf("the line is %d columns wide, want at most 20: %q", width, lines[0])
	}
	if !strings.HasSuffix(strings.TrimSpace(lines[0]), "…") {
		t.Errorf("the clipped line does not say it was cut: %q", lines[0])
	}
}

// TestASeparateStderrReadsBelowItsLabel covers a command whose streams
// are captured apart: the error output reads under its own label.
func TestASeparateStderrReadsBelowItsLabel(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolResult("bash", "error", aBashObservation(t, map[string]any{
		"returncode": 1, "timed_out": false,
		"output": "out line\n",
		"stderr": "err line\n",
	}), 0)

	rendered := ansi.Strip(out.String())
	stdout := strings.Index(rendered, "out line")
	label := strings.Index(rendered, "stderr:")
	stderr := strings.Index(rendered, "err line")
	if stdout < 0 || label < 0 || stderr < 0 {
		t.Fatalf("the streams do not read:\n%s", rendered)
	}
	if !(stdout < label && label < stderr) {
		t.Errorf("the streams read out of order:\n%s", rendered)
	}
	if !strings.Contains(rendered, "exit 1") {
		t.Errorf("the exit status does not read:\n%s", rendered)
	}
}

// TestACappedStderrKeepsItsTailAndStatus covers an elided error stream:
// its label, tail, elided count, hint and exit status all read.
func TestACappedStderrKeepsItsTailAndStatus(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolResult("bash", "error", aBashObservation(t, map[string]any{
		"returncode":   7,
		"timed_out":    false,
		"output":       "out line\n",
		"stderr_head":  "err head one\nerr head two\n",
		"stderr_tail":  "err tail\n",
		"elided_chars": 500,
		"hint":         "narrow the range",
	}), 0)

	rendered := ansi.Strip(out.String())
	for _, want := range []string{
		"stderr:", "err head one", "err tail", "500 characters elided",
		"narrow the range", "exit 7",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the capped stderr lacks %q:\n%s", want, rendered)
		}
	}
}

// TestAResizedTerminalBoundsTheNextPreview covers the live width: a
// terminal narrowed after a render clips the next summary and result.
func TestAResizedTerminalBoundsTheNextPreview(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	width := 80
	r.liveWidth = func() int { return width }
	r.toolCall("bash", strings.Repeat("a", 200))
	r.toolResult("grep", "ok", strings.Repeat("b", 200), 0)
	for _, line := range plainLines(r.transcriptText()) {
		if got := ansi.StringWidth(line); got > 80 {
			t.Errorf("a wide terminal line is %d columns:\n%q", got, line)
		}
	}

	width = 20
	out.Reset()
	r.transcript.Reset()
	r.toolCall("bash", strings.Repeat("a", 200))
	r.toolResult("grep", "ok", strings.Repeat("c", 200), 0)
	lines := plainLines(r.transcriptText())
	if len(lines) != 2 {
		t.Fatalf("the narrowed render reads as %d lines:\n%s", len(lines), r.transcriptText())
	}
	for _, line := range lines {
		if got := ansi.StringWidth(line); got > 20 {
			t.Errorf("a narrowed line is %d columns, want at most 20:\n%q", got, line)
		}
		if !strings.Contains(line, "…") {
			t.Errorf("a narrowed line does not say it was cut:\n%q", line)
		}
	}
}
