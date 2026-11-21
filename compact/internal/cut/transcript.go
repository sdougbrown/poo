package cut

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/sdougbrown/poo"
)

// Serialize renders the covered messages as one block of transcript
// text.
//
// Tool results are capped. The full text of one is what a replacement
// does not need, and it is what would make a strategy's request larger
// than the window it reads. Thinking is left out for the same reason. A
// provider also often drops reasoning that came from another request,
// so it is not reliably part of the context either.
func Serialize(messages []poo.Message) string {
	parts := make([]string, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case poo.RoleAssistant:
			if text := strings.TrimSpace(m.Text()); text != "" {
				parts = append(parts, "[Assistant]: "+text)
			}
			calls := make([]string, 0, len(m.Content))
			for _, c := range m.ToolCalls() {
				calls = append(calls, CallText(c))
			}
			if len(calls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(calls, "; "))
			}
		case poo.RoleTool:
			for _, c := range m.Content {
				if c.Kind != poo.ContentToolResult {
					continue
				}
				text := strings.TrimSpace(c.Text)
				if text == "" {
					continue
				}
				parts = append(parts, "[Tool result]: "+poo.CapSummary(text))
			}
		default:
			if text := strings.TrimSpace(m.Text()); text != "" {
				parts = append(parts, "[User]: "+text)
			}
		}
	}
	return EscapeTags(strings.Join(parts, "\n\n"))
}

// CallText renders one tool call as a line of the transcript.
func CallText(c poo.ContentBlock) string {
	if len(c.Arguments) == 0 {
		return c.Name + "()"
	}
	var args map[string]any
	if err := json.Unmarshal(c.Arguments, &args); err != nil {
		return c.Name + "(" + string(c.Arguments) + ")"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	// A call's arguments are read by the strategy's request, so they are
	// ordered: a transcript that reshuffles them reads as a different
	// call.
	sort.Strings(keys)
	rendered := make([]string, 0, len(keys))
	for _, k := range keys {
		v, err := json.Marshal(args[k])
		if err != nil {
			continue
		}
		rendered = append(rendered, k+"="+string(v))
	}
	return c.Name + "(" + strings.Join(rendered, ", ") + ")"
}

// EscapeTags defangs the tags a strategy's request uses to delimit its
// parts.
//
// A tool result or a model's earlier reply is not the request's own
// words. One that emits a closing tag could pose as the end of the
// transcript, or as an earlier replacement. The role prefixes stay
// forgeable, as they are in any transcript format, but the structure
// the request is parsed by does not.
var escapeTags = strings.NewReplacer(
	"<transcript>", `\<transcript>`,
	"</transcript>", `<\/transcript>`,
	"<pointers>", `\<pointers>`,
	"</pointers>", `<\/pointers>`,
	"<previous-context>", `\<previous-context>`,
	"</previous-context>", `<\/previous-context>`,
	"<conversation>", `\<conversation>`,
	"</conversation>", `<\/conversation>`,
	"<previous-summary>", `\<previous-summary>`,
	"</previous-summary>", `<\/previous-summary>`,
)

// EscapeTags escapes the tags a strategy's request delimits its parts
// by.
func EscapeTags(text string) string { return escapeTags.Replace(text) }

// PreviousSummary returns the text an earlier compaction left in the
// view, so a request carries it forward instead of covering that work
// again from nothing.
//
// Any compaction's visible text counts, not only one strategy's: a run
// whose strategy changed keeps the checkpoint the earlier one wrote. A
// provider window and the marker the core writes for it are not
// checkpoints, and neither is an empty message.
func PreviousSummary(messages []poo.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Origin != poo.OriginCompaction {
			continue
		}
		text := strings.TrimSpace(m.Text())
		if text == "" || text == poo.CompactionMarkerText {
			continue
		}
		// A replacement carries its marker line first: the checkpoint is
		// what follows it.
		if strings.HasPrefix(text, MarkerPrefix) {
			if _, rest, ok := strings.Cut(text, "\n\n"); ok {
				return strings.TrimSpace(rest)
			}
		}
		return text
	}
	return ""
}
