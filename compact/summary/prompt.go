package summary

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/sdougbrown/poo"
)

// The summarization request's shape.
//
// The prompts and the serialized conversation follow pi's compaction,
// Claude Code's /compact and OpenHands' condensers: a conversation
// rendered as text, a system prompt that forbids continuing it, a
// structured checkpoint format, and an update form that carries the
// previous summary forward. The strategy ships the format those tools
// found worth keeping rather than inventing a new one.

// systemPrompt keeps the model in the summarizer's role.
const systemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

// summarizePrompt asks for the first summary of a session.
const summarizePrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// updatePrompt asks for a summary that carries the previous one forward.
//
// A run can compact more than once, and each compaction summarizes only
// the turns since the last one. Without the previous summary the
// checkpoint would describe the newest work and forget the goal, the
// decisions and the constraints that came before it.
const updatePrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// summarizationRequest builds the messages the summarization request
// carries.
//
// The conversation is text inside the request, not the request itself,
// so the model reads a transcript to summarize rather than a
// conversation to answer.
func summarizationRequest(covered []poo.Message, previous string) []poo.Message {
	var b strings.Builder
	b.WriteString("<conversation>\n")
	b.WriteString(serialize(covered))
	b.WriteString("\n</conversation>\n\n")
	if previous != "" {
		b.WriteString("<previous-summary>\n")
		b.WriteString(previous)
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

// escapeTags defangs the tags the request uses to delimit its parts.
//
// A tool result is not the request's own words, and one that emits a
// closing tag could pose as the end of the conversation or as an earlier
// summary. The role prefixes stay forgeable, as they are in any
// transcript format, but the structure the request is parsed by does not.
var escapeTags = strings.NewReplacer(
	"<conversation>", `\<conversation>`,
	"</conversation>", `<\/conversation>`,
	"<previous-summary>", `\<previous-summary>`,
	"</previous-summary>", `<\/previous-summary>`,
)

// serialize renders the covered messages as one block of text.
//
// Tool results are capped: the full text of one is what the summary does
// not need, and it is what would make the request larger than the window
// it summarizes. Thinking is left out for the same reason, and because a
// provider often drops reasoning that came from another request, so it is
// not reliably part of the context either.
func serialize(messages []poo.Message) string {
	parts := make([]string, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case poo.RoleAssistant:
			if text := strings.TrimSpace(m.Text()); text != "" {
				parts = append(parts, "[Assistant]: "+text)
			}
			calls := make([]string, 0, len(m.Content))
			for _, c := range m.ToolCalls() {
				calls = append(calls, callText(c))
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
	return escapeTags.Replace(strings.Join(parts, "\n\n"))
}

// callText renders one tool call as a line of the transcript.
func callText(c poo.ContentBlock) string {
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
	// A call's arguments are read by the summarizer, so they are ordered:
	// a transcript that reshuffles them reads as a different call.
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

// fileList states the files the summarized work read and changed.
//
// The list is what the tail no longer shows, and it is the prior art's
// answer to the re-read problem: a summary that neglects to mention a
// file the model edited sends the model back to read it. The calls name
// the files, so the list does not depend on the summary repeating them.
func fileList(files FileOp, messages []poo.Message) string {
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

// bullets renders paths as a list, one per line.
func bullets(paths []string) string {
	lines := make([]string, len(paths))
	for i, path := range paths {
		lines[i] = "- " + path
	}
	return strings.Join(lines, "\n")
}

// defaultFileOp reports the file a call to one of poo's own file tools
// names.
//
// poo ships read, edit and write, and each names its file in a "path"
// argument. A consumer's tools are its own, so the default knows only
// these, and a strategy configured by that consumer reports its own.
func defaultFileOp(name string, arguments json.RawMessage) (string, bool) {
	path := argumentPath(arguments)
	if path == "" {
		return "", false
	}
	switch name {
	case "read":
		return path, false
	case "edit", "write":
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
