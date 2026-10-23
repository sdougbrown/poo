package poo

import "fmt"

// The tool-result handles.
//
// Every tool result's model-facing text starts with a short handle
// line, so the model can name the result later and a compaction
// strategy can find it. The handles are append-only, so they are safe
// for the prompt cache.

// HandleLine returns the line that starts a tool result's model-facing
// text.
//
// The line names the result's handle and the tool that produced it:
// "[r1 · bash]". A strategy that rewrites a result builds the same
// line, with the compacted marker, through CompactedHandleLine.
func HandleLine(handle, tool string) string {
	return fmt.Sprintf("[%s · %s]", handle, tool)
}

// CompactedHandleLine returns the line that starts the model-facing
// text of a result a compaction rewrote.
//
// The marker tells the model the result was rewritten: "[r1 · bash ·
// compacted]".
func CompactedHandleLine(handle, tool string) string {
	return fmt.Sprintf("[%s · %s · compacted]", handle, tool)
}

// modelFacingText returns the text the model sees for a tool result.
//
// The handle line comes first, and the tool's own text follows it on
// the next line. A result with no text of its own is the handle line
// alone.
func modelFacingText(handle, tool, text string) string {
	line := HandleLine(handle, tool)
	if text == "" {
		return line
	}
	return line + "\n" + text
}
