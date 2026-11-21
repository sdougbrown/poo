package mine

import (
	_ "embed"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/internal/cut"
)

// The mining request's shape.
//
// The request reads the covered transcript as data, the way the
// harnesses that mine a prior session's transcript read it: a pointer
// pass says what to look for, the transcript holds it, and the reply is
// the context a fresh start continues from. The checkpoint's structure
// is the summary strategy's, so a consumer reads one shape whichever
// strategy wrote the replacement.

// The prompts the requests carry live in prompts/*.md, embedded by
// prompts.go. The transcript's shape comes from the cut package, which
// every replacement strategy reads the covered history through.

// pointerRequest builds the message the fork appends.
//
// The fork re-sends the window the model just worked in, so the
// appended message is all the pass adds: the model knows what it was
// doing, and the ask only asks it to name what the reconstruction must
// keep.
func pointerRequest() []poo.Message {
	return []poo.Message{poo.NewTextMessage(poo.RoleUser, pointerPrompt)}
}

// miningRequest builds the messages the mining request carries.
//
// The transcript is text inside the request, not the request itself, so
// the model reads material to construct from rather than a conversation
// to answer. The pointers and the previous context are model output, so
// they carry the same forgery risk as a tool result: one that emits a
// closing tag would end its block early and write the rest as
// instructions.
func miningRequest(covered []poo.Message, pointers, previous string) []poo.Message {
	var b strings.Builder
	b.WriteString("<transcript>\n")
	b.WriteString(cut.Serialize(covered))
	b.WriteString("\n</transcript>\n\n")
	if pointers != "" {
		b.WriteString("<pointers>\n")
		b.WriteString(cut.EscapeTags(pointers))
		b.WriteString("\n</pointers>\n\n")
	}
	if previous != "" {
		b.WriteString("<previous-context>\n")
		b.WriteString(cut.EscapeTags(previous))
		b.WriteString("\n</previous-context>\n\n")
		b.WriteString(updatePrompt)
	} else {
		b.WriteString(minePrompt)
	}
	return []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, systemPrompt),
		poo.NewTextMessage(poo.RoleUser, b.String()),
	}
}
