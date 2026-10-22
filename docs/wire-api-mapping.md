# Wire API mapping

A paper round-trip through poo's internal message model, for the three wire
APIs the model has to survive: **Chat Completions**, **OpenAI Responses
(stateless)** and **Anthropic Messages**.

The question every row answers is the same one: if a provider sends a piece of
content, does poo's internal model hold it, and can poo put it back on the
wire unchanged?

The answer decides what the model has to carry. The model is frozen after this
document, so a gap found here is a gap fixed here.

## How to read the tables

- **Wire** is the JSON the API sees. Ellipses stand for the payload.
- **Internal** is poo's own shape, in the shorthand of the Go types
  (`Message{Role:user, Origin:user, Content:[text]}`).
- **Back** is what poo puts on the wire when it replays the same history.
- **Lossless** says whether `wire → internal → wire` keeps the meaning. It
  does not mean byte-for-byte: JSON whitespace and key order are not part of
  the meaning, but a value the API treats as opaque **is** part of it.

The internal model is:

```go
type Message struct {
    Role         Role
    Content      []ContentBlock
    ID           string          // "m<n>", session-monotonic
    Origin       Origin          // "user", "model", "job", "system", ...
    DeliveryNote *DeliveryNote   // what the user had seen of the last reply
}

type ContentBlock struct {
    Kind        ContentKind      // text | thinking | tool_call | tool_result | provider
    Text        string
    ToolCallID  string
    Name        string
    Arguments   json.RawMessage
    IsError     bool
    Provider    *ProviderBlock   // opaque provider data
}

type ProviderBlock struct {
    API      API               // chat_completions | responses | anthropic_messages
    Type     string            // the provider's own name for the payload
    Data     json.RawMessage   // the payload, verbatim
    Required bool              // the request fails without it
}
```

A `ProviderBlock` is the plan's first-class opaque block. It rides either
alone (a `provider` content block) or as the companion of a visible block (a
thinking block's signature).

## Chat Completions

`POST /v1/chat/completions`. The API poo v1 ships.

| # | Wire | Internal | Back | Lossless |
|---|---|---|---|---|
| C1 | `{"role":"system","content":"…"}` | `Message{Role:system, Origin:system, Content:[text]}` | the same message | **yes** |
| C2 | `{"role":"user","content":"…"}` | `Message{Role:user, Origin:user, Content:[text]}` | the same message | **yes** |
| C3 | `{"role":"assistant","content":"…"}` | `Message{Role:assistant, Origin:model, ID:"m3", Content:[text]}` | the same message, without the id | **yes** — the id is poo's, not the API's |
| C4 | `{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}]}` | `Content{kind:tool_call, tool_call_id:"call_1", name:"bash", arguments:{"command":"ls"}}` | the same call, with the arguments re-encoded as a JSON string | **yes, in value** — the wire carries the arguments as a *string*; poo holds the parsed value, so whitespace inside that string is not preserved |
| C5 | `{"role":"tool","tool_call_id":"call_1","content":"out"}` | `Message{Role:tool, Origin:tool, Content:[{kind:tool_result, tool_call_id:"call_1", name:"bash", text:"out"}]}` | the same message | **yes**, except the two fields the wire has no room for: the tool's `name` (read back from the matching call) and `is_error` (conveyed in the text) |
| C6 | `delta.reasoning_content` (a server extension, not in the spec) | `Content{kind:thinking, text}` | dropped | **no** — Chat Completions has no field that carries thinking back. The model keeps the thinking in the view; the request loses it. |
| C7 | `delta.tool_calls[]` fragments | `Content{kind:tool_call}` | reassembled | **yes** |
| C8 | `usage.prompt_tokens`, `usage.completion_tokens`, `usage.prompt_tokens_details.cached_tokens` | `UsageReport{InputTokens, OutputTokens, CacheReadTokens}` | — (response side) | **yes** |
| C9 | `finish_reason: "stop"｜"length"｜"tool_calls"` | `FinishReason` | the same | **yes** |
| C10 | `tools[].function.{name,description,parameters}` | `ToolSpec{Name, Description, Schema}` | the same | **yes** |
| C11 | `{"role":"assistant","content":null,"refusal":"…"}` — a refusal is a **top-level string** on the message, not a content part | `Content{kind:text}` | a plain string | **no** — see *Scope decisions* |

Two asymmetries are worth naming because they are properties of the API, not
of poo's model:

- **Thinking does not survive the request.** `reasoning_content` is an
  extension many servers send and no server accepts back. A history that
  carries thinking is a history the endpoint never sees again.
- **A tool result carries no error flag.** Chat Completions has no
  `is_error` on a tool message: the failure is in the text the model reads.
  poo keeps `is_error` in its own record and in the model's view, because
  Anthropic has a field for it and the two APIs should not behave
  differently inside poo.

## OpenAI Responses (stateless)

`POST /v1/responses`, with no `previous_response_id` and no `store`: every
request carries the whole history, which is poo's rule.

| # | Wire | Internal | Back | Lossless |
|---|---|---|---|---|
| R1 | `{"type":"message","role":"user","content":[{"type":"input_text","text":"…"}]}` | `Message{Role:user, Origin:user, Content:[text]}` | a message item with `input_text` | **yes** |
| R2 | `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"…"}]}` | `Message{Role:assistant, Origin:model, Content:[text]}` | a message item with `output_text` | **yes** |
| R3 | `{"type":"function_call","call_id":"call_1","name":"bash","arguments":"{\"command\":\"ls\"}"}` | `Content{kind:tool_call, tool_call_id:"call_1", name:"bash", arguments:{"command":"ls"}}` | its own item, arguments re-encoded as a string | **yes, in value** |
| R4 | `{"type":"function_call_output","call_id":"call_1","output":"…"}` | `Content{kind:tool_result, tool_call_id:"call_1", text:"…"}` | its own item | **yes** |
| R5 | `{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"…"}],"encrypted_content":"…"}` | `Content{kind:thinking, text:"…", provider:{api:responses, type:"reasoning", data:{"id":"rs_1","encrypted_content":"…"}}}` | one reasoning item again: the summary from the thinking text, the id and the encrypted content from the provider block | **in the summary shape** — the encrypted content is opaque and travels back untouched, and the summary's text survives. The *part* boundaries do not: the thinking is one string, so a summary of several parts comes back as one part. See *Where this document is not sure*. |
| R6 | a compaction item (an opaque item the provider's own compaction produced) | `Content{kind:provider, provider:{api:responses, type:"compaction", data:<the item, verbatim>, required:true}}` | the same item, verbatim | **yes** — the payload is never interpreted, only carried |
| R7 | `instructions` (the system prompt) | `Message{Role:system, Origin:system, Content:[text]}` | the `instructions` field | **yes, in content** — the field's shape differs, the prompt does not |
| R8 | `usage.input_tokens`, `usage.output_tokens`, `usage.input_tokens_details.cached_tokens` | `UsageReport` | — (response side) | **yes** |
| R9 | `max_output_tokens` | `ChatRequest.MaxTokens` | the same | **yes** |
| R10 | `{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"…"}]}` — a refusal is a **content part** here, unlike Chat Completions | `Content{kind:text}` | a plain string | **no** — see *Scope decisions* |

The structural difference from Chat Completions is that a function call and
its output are **top-level items**, not messages. poo's model keeps a tool
call as a block of the assistant message, so the transport splits one
assistant message into a message item plus one item per call, in order. The
order is preserved, because the API matches a call to its output by position
as well as by id.

## Anthropic Messages

`POST /v1/messages`.

| # | Wire | Internal | Back | Lossless |
|---|---|---|---|---|
| A1 | `system` (a string or a list of blocks) | `Message{Role:system, Origin:system, Content:[text]}` | the `system` field | **yes, in content** |
| A2 | `{"role":"user","content":[{"type":"text","text":"…"}]}` | `Message{Role:user, Origin:user, Content:[text]}` | the same | **yes** |
| A3 | `{"role":"assistant","content":[{"type":"text","text":"…"}]}` | `Message{Role:assistant, Origin:model, Content:[text]}` | the same | **yes** |
| A4 | `{"type":"tool_use","id":"toolu_1","name":"bash","input":{"command":"ls"}}` | `Content{kind:tool_call, tool_call_id:"toolu_1", name:"bash", arguments:{"command":"ls"}}` | the same block | **yes** — the input is a JSON *object* here, not a string |
| A5 | `{"type":"tool_result","tool_use_id":"toolu_1","content":"…","is_error":false}` | `Content{kind:tool_result, tool_call_id:"toolu_1", text:"…", is_error:false}` | the same block | **yes** — the tool's name is not on the wire, and poo does not need it there |
| A6 | `{"type":"thinking","thinking":"…","signature":"sig"}` | `Content{kind:thinking, text:"…", provider:{api:anthropic_messages, type:"thinking_signature", data:"\"sig\""}}` | one thinking block again: the text from the block, the signature from the provider block | **yes** — the signature is opaque and must travel back untouched |
| A7 | `{"type":"redacted_thinking","data":"…"}` | `Content{kind:provider, provider:{api:anthropic_messages, type:"redacted_thinking", data:"\"…\""}}` | the same block, verbatim | **yes** |
| A8 | `stop_reason: "end_turn"｜"max_tokens"｜"tool_use"` | `FinishReason` | the same, mapped | **yes** |
| A9 | `usage.input_tokens`, `usage.output_tokens`, `usage.cache_read_input_tokens`, `usage.cache_creation_input_tokens` | `UsageReport{InputTokens, OutputTokens, CacheReadTokens}` | — (response side) | **yes**, except the cache *write* count, which the plan's usage record has no field for. See *Scope decisions*. |

Two placement rules belong to the API, and a transport has to honour them:

- A tool result lives in a **user** message, not in one of its own. poo keeps
  tool results in their own message with `role: tool`; the transport merges
  them into the user message that follows the assistant's `tool_use`.
- Thinking blocks come **first** in an assistant message. poo preserves block
  order, so the transport keeps the order and the rule holds.

## Gaps this round trip found, and how they are closed

1. **Opaque provider data had nowhere to go.** Anthropic's thinking
   signature, Anthropic's redacted thinking, the Responses reasoning item's
   encrypted content and the Responses compaction item are all values the
   model must hold without interpreting them, and must give back unchanged.
   *Closed:* `ProviderBlock{api, type, data, required}` is a first-class
   block. It rides alone as a `provider` content block, or as the companion
   of a visible block (a thinking block's signature).

2. **A visible block needed room for an invisible companion.** The
   Anthropic thinking block is one block on the wire and two facts in poo:
   the thinking text and the signature. Splitting it into two blocks would
   break the "one thinking block" shape the API expects.
   *Closed:* a content block carries an optional `Provider` companion. The
   transport merges the two back into the API's own block.

3. **Messages had no id.** The plan's delivery note names an assistant
   message, and the message id is what the note refers to.
   *Closed:* `Message.ID` holds a session-monotonic `m<n>`, and
   `poo.MessageIDs` hands them out. The counter belongs to the session, so
   a second run's ids continue where the first run's stopped.

4. **Messages had no origin.** A user message can come from a person, a
   steer, an interrupt, a follow-up, a job or a hook, and the model and the
   log both need to know which.
   *Closed:* `Message.Origin`, with the plan's values.

5. **A user message had nowhere to put a delivery note.** The note is
   rendered when the input is accepted and must survive to the record, so
   replay is exact.
   *Closed:* `Message.DeliveryNote{message_id, upto_chars, text}`.

6. **A tool result's tool name and error flag have no home on one of the
   APIs.** Chat Completions' tool message carries neither.
   *Closed:* this is not a model gap. poo holds both, the transport sends
   what the API accepts, and the difference is recorded in the mapping above
   rather than worked around in the model.

## Scope decisions

These are deliberate, and they are not gaps in the model:

- **Refusal parts** are not represented. The two APIs differ in where a
  refusal lives: the Responses API sends it as a `refusal` content part
  (`{"type":"refusal","refusal":"…"}`), and Chat Completions sends it as
  a top-level `message.refusal` string next to the content. v1 ships one
  transport, the plan's block list does not name refusal, and a refusal is
  only distinguishable by the message's shape — a shape the
  OpenAI-compatible servers v1 targets do not all accept. The refusal text
  is kept as a text block, so nothing the model said is lost; what is
  lost is the *label*.
- **Images and audio** are not carried in the model. The plan lists them as
  provider *input modalities*, and v1's tools produce text.
- **A tool result that carries images** is out of scope for the same
  reason.
- **The cache-write token count** (Anthropic's
  `cache_creation_input_tokens`) has no field: the plan's usage record names
  `input_tokens`, `output_tokens`, `cache_read_tokens` and `ttft_ms`, and
  this stage adds no field to that vocabulary.
- **A foreign API's opaque block** is omitted from a request when it is not
  required, and fails the request with `session_incompatible` when it is.
  The rule needs no model change: the block says which API it belongs to.

## Where this document is not sure

Stated plainly, because a wrong detail in a mapping is a bug that waits for
the transport that implements it:

- **The Responses compaction item.** The API has a compaction endpoint that
  returns a compacted item set including a compaction item with encrypted
  content. The exact field names of that item are not pinned here. The model
  does not care: the item is carried verbatim in a `provider` block, and the
  transport that implements the endpoint is the only thing that has to know
  its shape.
- **The Responses reasoning item's `summary` versus `content`.** A
  reasoning item carries either a `summary` (a list of `summary_text`
  parts) or `content` (a list of `reasoning_text` parts), and the two
  hold different things: the summary is the model's own account of its
  reasoning, and the content is the reasoning itself. poo's model has one
  thinking string, so the *text* survives in either shape but the *part
  boundaries* do not, and an item whose reasoning arrived as `content`
  cannot be told from one whose summary said the same words. The
  transport that implements the Responses API must decide how the two
  shapes map onto the one block; the model does not depend on the answer,
  because the opaque `encrypted_content` is carried verbatim either way.
- **Whether the Responses API accepts a `system`-role message item.** The
  documented way to send a system prompt is the top-level `instructions`
  field, and that is what the mapping uses. A system-role item is not
  assumed to work.
- **Anthropic's exact ordering rule for thinking blocks.** The rule used
  here is that thinking blocks come first in an assistant message. The
  transport that implements Anthropic must confirm it against the API
  before it ships; the model does not depend on the answer, because it keeps
  the blocks in order either way.
- **The Chat Completions content-part array.** The `text` and `refusal` part
  types are part of the newer spec; the servers poo targets mostly take a
  plain string. poo sends a string. A refusal, when the server sends one
  at all, arrives as the message's top-level `refusal` string, not as a
  part.
- **Anthropic's `cache_creation_input_tokens`.** The field name is taken
  from the usage block's shape; the value is not carried, so a wrong guess
  costs nothing.

## What the model therefore has to carry

The three APIs together need: text, thinking, tool calls, tool results, one
opaque companion per visible block, and whole opaque items. That is exactly
`ContentKind`'s five values plus `ProviderBlock`, and nothing more. The model
is frozen.
