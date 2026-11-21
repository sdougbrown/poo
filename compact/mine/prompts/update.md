The text in <transcript> is NEW conversation material to incorporate into the existing working context provided in <previous-context> tags. Anything in <pointers> names what the context must preserve.

Update the existing working context with the new material. RULES:
- PRESERVE all existing information from the previous context
- ADD new goals, decisions, and state from the new material
- UPDATE the State section: move work from in progress to done when it completed, and record what the new material changed in the world
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, commands, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## State
[The state of the work: what exists, what changed, what was tried and abandoned, what is verified and what is not. Preserve previous state, apply the new material.]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise.
