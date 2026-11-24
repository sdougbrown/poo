package pointer

import (
	_ "embed"
	"sort"
	"strconv"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/internal/cut"
)

// The requests the strategy carries.
//
// The shape follows the mine strategy's: the transcript is text inside
// the request, the model's earlier replies carry forgery risk and are
// escaped, and the checkpoint has a fixed structure. What differs is the
// input: the orientation seed and the query matches stand in for the
// transcript, so the compose ask stays inside the compaction model's
// window whatever the session's size.

// The prompts the requests carry live in prompts/*.md, embedded by
// prompts.go.
var (
	// pointerPrompt asks the fork for the queries and the notes, as one
	// JSON object.
	//
	//go:embed prompts/pointer.md
	pointerPrompt string
	// composePrompt asks for the first reconstruction of a session.
	//
	//go:embed prompts/compose.md
	composePrompt string
	// updatePrompt asks for a reconstruction that carries the previous
	// one forward.
	//
	//go:embed prompts/update.md
	updatePrompt string
	// systemPrompt keeps the model in the reconstruction's role.
	//
	//go:embed prompts/system.md
	systemPrompt string
	// reformPrompt asks the compaction role to re-form a malformed
	// pointer reply into the machine-readable shape.
	//
	//go:embed prompts/reform.md
	reformPrompt string
)

// pointerRequest builds the message the fork appends.
//
// The fork re-sends the window the model just worked in, so the
// appended message is all the pass adds.
func pointerRequest() []poo.Message {
	return []poo.Message{poo.NewTextMessage(poo.RoleUser, pointerPrompt)}
}

// composeRequest builds the messages the compose request carries.
//
// The seed orients the model, the matches are what the queries hit, and
// the notes are the fork's own words. The matches are the model's view
// of its own history, but the lines are cut-rendered, so a forgery that
// emits a closing tag is defanged the same way the transcript is.
func composeRequest(seed []poo.Message, matches []poo.HistoryMatch, notes, previous string) []poo.Message {
	var b strings.Builder
	b.WriteString("<task>\n")
	b.WriteString(seedText(seed))
	b.WriteString("\n</task>\n\n")
	if len(matches) > 0 {
		b.WriteString("<matches>\n")
		b.WriteString(matchText(matches))
		b.WriteString("\n</matches>\n\n")
	}
	if notes != "" {
		b.WriteString("<notes>\n")
		b.WriteString(cut.EscapeTags(notes))
		b.WriteString("\n</notes>\n\n")
	}
	if previous != "" {
		b.WriteString("<previous-context>\n")
		b.WriteString(cut.EscapeTags(previous))
		b.WriteString("\n</previous-context>\n\n")
		b.WriteString(updatePrompt)
	} else {
		b.WriteString(composePrompt)
	}
	return []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, systemPrompt),
		poo.NewTextMessage(poo.RoleUser, b.String()),
	}
}

// seedText renders the orientation messages.
func seedText(seed []poo.Message) string {
	parts := make([]string, 0, len(seed))
	for _, m := range seed {
		if text := strings.TrimSpace(m.Text()); text != "" {
			parts = append(parts, "["+string(m.Role)+"]: "+cut.EscapeTags(text))
		}
	}
	return strings.Join(parts, "\n\n")
}

// matchText renders the matches as the lines the compose ask reads,
// oldest turn first.
func matchText(matches []poo.HistoryMatch) string {
	sorted := make([]poo.HistoryMatch, len(matches))
	copy(sorted, matches)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Turn < sorted[j].Turn })
	lines := make([]string, 0, len(sorted))
	for _, m := range sorted {
		// The text is stored content - tool results included - so a
		// line that reads as a tag is escaped like the transcript's
		// own: the block it lands in is the one the compose ask reads.
		lines = append(lines, "turn "+itoa(m.Turn)+": "+cut.EscapeTags(m.Text))
	}
	return strings.Join(lines, "\n")
}

// itoa renders a turn number.
func itoa(n int) string { return strconv.Itoa(n) }

// reformRequest builds the messages the reformat ask carries: the
// instruction and the malformed reply, and nothing else.
func reformRequest(malformed string) []poo.Message {
	return []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, systemPrompt),
		poo.NewTextMessage(poo.RoleUser, reformPrompt+"\n\n"+cut.EscapeTags(malformed)),
	}
}
