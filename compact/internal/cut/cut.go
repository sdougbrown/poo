// Package cut holds the machinery the replacement compaction strategies
// share: choosing the turns a replacement covers, naming the range, and
// rendering the covered history as text.
//
// A replacement strategy cuts a run of older turns out of the view and
// writes messages in their place. What goes in the messages is the
// strategy's own; the cut, the marker that says the history was
// replaced, the file list the tail no longer shows, and the transcript
// the strategy reads are the same for every one of them, so they live
// here rather than drifting apart beside each other.
package cut

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sdougbrown/poo"
)

// MarkerPrefix starts every replacement's marker line. The model reads
// it as the sign that its history was replaced rather than silently
// dropped, and a strategy that carries a prior replacement forward
// strips the marker to read what follows it.
const MarkerPrefix = "[Context compacted:"

// tailShare is the share of the prompt limit a verbatim tail may fill.
// A prompt limit smaller than twice the configured tail keeps this
// share instead, so a small-prompt model still compacts.
const tailShare = 0.5

// Range is the range of turns a compaction replaces.
type Range struct {
	// Cut is the range of turns, by session turn number.
	Cut poo.TurnRange
	// Start and End are the indexes of the messages the range covers in
	// the view: Start is the first, End the index after the last.
	Start int
	End   int
}

// Marker names the turns a replacement covers.
//
// verb says what the replacement carries, as in "summarized" or
// "reconstructed": the marker reads "[Context compacted: turns 1-3
// summarized below]".
func Marker(r Range, verb string) string {
	return fmt.Sprintf("%s turns %d-%d %s below]", MarkerPrefix, r.Cut.First, r.Cut.Last, verb)
}

// LiveTurns returns the run of turns the view still holds.
//
// A compaction removes a run of turns and leaves the rest where they
// were. The turns a strategy may cut are therefore the leading removed
// turns skipped, and the contiguous run that follows them.
func LiveTurns(turns []poo.TurnSummary) []poo.TurnSummary {
	live := make([]poo.TurnSummary, 0, len(turns))
	started := false
	for _, t := range turns {
		if t.Removed {
			if started {
				break
			}
			continue
		}
		started = true
		live = append(live, t)
	}
	return live
}

// TurnTokens estimates what a turn's messages cost in the window.
func TurnTokens(messages []poo.Message, budget poo.ContextBudget) int {
	ratio := budget.CharsPerToken
	if ratio <= 0 {
		ratio = poo.CharsPerToken
	}
	return int(float64(poo.EstimateHistoryChars(messages)) / ratio)
}

// TailTokens returns how many tokens of the newest history a tail of
// keep tokens may hold.
//
// A prompt limit smaller than twice the keep keeps the share instead,
// so a small-prompt model still compacts.
func TailTokens(budget poo.ContextBudget, keep int) int {
	if limit := budget.PromptLimit(); limit > 0 {
		if share := int(float64(limit) * tailShare); share < keep {
			keep = share
		}
	}
	if keep < 1 {
		keep = 1
	}
	return keep
}

// RoomAfter returns the budget a replacement would leave: the input
// now, less the tokens the covered messages cost, plus what the
// replacement carries.
//
// A strategy compares it with RoomShort to report whether the
// compaction ends the shortage, so the arithmetic the estimate needs is
// one formula the strategies share rather than one per strategy.
func RoomAfter(budget poo.ContextBudget, coveredTokens, replacementTokens int) poo.ContextBudget {
	after := budget
	after.EstimatedInput = budget.EstimatedInput - coveredTokens + replacementTokens
	return after
}

// Select returns the turns a replacement covers: everything before the
// verbatim tail.
//
// The tail is the newest turns of the view, as many as keepTokens
// holds. The range covers everything before it. It starts at the
// earliest turn still in the view and stops before any turn a
// compaction removed, so the range the core is asked to replace is one
// contiguous run.
//
// The second return is false when there is nothing older to replace: a
// view that holds a single turn has no work behind it, and replacing
// that turn would replace the work the run is in the middle of.
func Select(messages []poo.Message, turns []poo.TurnSummary, budget poo.ContextBudget, keepTokens int) (Range, bool) {
	live := LiveTurns(turns)
	if len(live) < 2 {
		return Range{}, false
	}
	keep := TailTokens(budget, keepTokens)
	tail := len(live) - 1
	tokens := 0
	for i := len(live) - 1; i >= 0; i-- {
		tokens += TurnTokens(messages[live[i].Start:live[i].End], budget)
		tail = i
		if tokens >= keep {
			break
		}
	}
	if tail == 0 {
		return Range{}, false
	}
	return Range{
		Cut:   poo.TurnRange{First: live[0].TurnIndex, Last: live[tail-1].TurnIndex},
		Start: live[0].Start,
		End:   live[tail-1].End,
	}, true
}

// FileOp reports the file a tool call names.
//
// path is the file the call names, and changed reports whether the call
// changed it. A call the function does not recognize returns an empty
// path, and the file is not listed.
type FileOp func(name string, arguments json.RawMessage) (path string, changed bool)

// FileList states the files the covered work read and changed.
//
// The list is what the tail no longer shows. It is the prior art's
// answer to the re-read problem: a replacement that neglects to mention
// a file the model edited sends the model back to read it. The calls
// name the files, so the list does not depend on the replacement
// repeating them.
func FileList(files FileOp, messages []poo.Message) string {
	read := map[string]bool{}
	changed := map[string]bool{}
	for _, m := range messages {
		for _, c := range m.ToolCalls() {
			path, isChanged := files(c.Name, c.Arguments)
			if path == "" {
				continue
			}
			if isChanged {
				changed[path] = true
				continue
			}
			read[path] = true
		}
	}
	readOnly := make([]string, 0, len(read))
	for path := range read {
		if !changed[path] {
			readOnly = append(readOnly, path)
		}
	}
	modified := make([]string, 0, len(changed))
	for path := range changed {
		modified = append(modified, path)
	}
	sort.Strings(readOnly)
	sort.Strings(modified)
	var sections []string
	if len(readOnly) > 0 {
		sections = append(sections, "Files read:\n"+bullets(readOnly))
	}
	if len(modified) > 0 {
		sections = append(sections, "Files modified:\n"+bullets(modified))
	}
	return strings.Join(sections, "\n\n")
}

// DefaultFileOp reports the file a call to one of poo's own file tools
// names.
//
// poo ships read and edit, and each names its file in a "path"
// argument. A consumer's tools are its own, so the default knows only
// these two, and a consumer configures its strategy's Files for the
// rest.
func DefaultFileOp(name string, arguments json.RawMessage) (string, bool) {
	path := argumentPath(arguments)
	if path == "" {
		return "", false
	}
	switch name {
	case "read":
		return path, false
	case "edit":
		return path, true
	}
	return "", false
}

// argumentPath returns the path a call names, when it names one.
func argumentPath(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ""
	}
	return args.Path
}

// Retention is the pointer a replacement ends with.
//
// The store keeps every original message, so the text the cut removed
// is not gone. The pointer names the turns and the session they belong
// to, and the history tool that reads them, so a model that misses a
// detail knows where to look.
func Retention(turns poo.TurnRange, sessionID string, historyTool bool) string {
	where := ""
	if sessionID != "" {
		where = " of session " + sessionID
	}
	recover := ""
	if historyTool {
		recover = " The history tool reads them: search the retained turns with a pattern, or read one whole with its turn number."
	}
	return "[The turns replaced above are retained in this session's store" + where +
		" (turns " + strconv.Itoa(turns.First) + "-" + strconv.Itoa(turns.Last) + "): nothing was deleted." + recover + "]"
}

// bullets renders paths as a list, one per line.
func bullets(paths []string) string {
	lines := make([]string, len(paths))
	for i, path := range paths {
		lines[i] = "- " + path
	}
	return strings.Join(lines, "\n")
}
