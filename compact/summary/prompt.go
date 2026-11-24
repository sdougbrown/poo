package summary

import (
	_ "embed"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/internal/cut"
)

// The summarization request's shape.
//
// The prompts and the serialized conversation follow pi's compaction,
// Claude Code's /compact and OpenHands' condensers. A conversation is
// rendered as text, and a system prompt forbids continuing it. The
// checkpoint has a fixed structure, and an update form carries the
// previous summary forward. The strategy ships the format those tools
// found worth keeping rather than inventing a new one.

// The prompts the request carries live in prompts/*.md, embedded by
// prompts.go. The transcript's shape comes from the cut package, which
// every replacement strategy reads the covered history through.

// summarizationRequest builds the messages the summarization request
// carries.
//
// The conversation is text inside the request, not the request itself,
// so the model reads a transcript to summarize rather than a
// conversation to answer.
func summarizationRequest(covered []poo.Message, previous string) []poo.Message {
	var b strings.Builder
	b.WriteString("<conversation>\n")
	b.WriteString(cut.Serialize(covered))
	b.WriteString("\n</conversation>\n\n")
	if previous != "" {
		// The previous summary is model output, so it carries the same
		// forgery risk as a tool result: one that emits the closing tag
		// would end the block early and write the rest as instructions.
		b.WriteString("<previous-summary>\n")
		b.WriteString(cut.EscapeTags(previous))
		b.WriteString("\n</previous-summary>\n\n")
		b.WriteString(updatePrompt)
	} else {
		b.WriteString(summarizePrompt)
	}
	return []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, systemPrompt),
		poo.NewTextMessage(poo.RoleUser, b.String()),
	}
}
