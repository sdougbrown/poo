---
title: Conformance & Testing
description: Verify custom binaries with poo/wire/conformance, run client integration tests with conformance.NewFakeEndpoint, and unit-test sessions with poo/pootest.
---

# Conformance & Testing

Poo ships two exported testing packages: `poo/wire/conformance` for black-box protocol verification and client integration tests against real binaries, and `poo/pootest` for in-process Go unit tests against `poo.Session`.

## Wire conformance suite (`poo/wire/conformance`)

`poo/wire/conformance` is a black-box test suite. It launches a compiled binary with a temporary settings file pointing at a local fake OpenAI-compatible endpoint. It then drives the binary strictly over stdin and stdout. It imports only the Go standard library and `poo/wire/protocol`.

```go
func TestMyBinarySpeaksTheProtocol(t *testing.T) {
	report := conformance.Run(t, conformance.Target{
		Binary:  binary,
		Args:    []string{"serve"},
		Profile: "search",
		Env:     []string{"MY_POO_ID=conformance"},
	})
	if len(report.Skipped) > 0 {
		t.Logf("skipped: %v", report.Skipped)
	}
}
```

### Target requirements

- `Target.Binary` must be a compiled executable path (build your command before calling `conformance.Run`).
- The suite passes `--settings <file>` on every invocation and `--ephemeral` for the ephemeral-session flow, so your binary must accept both flags.
- `Target.Profile` must name a profile the binary announces in `poo/initialize` (defaults to `"code"` when empty).
- `Target.Env` appends child-only environment variables (useful for binaries that require an identity or socket path) without mutating the test process's environment.
- `Target.Fixtures` lets you supply custom `ScriptedAnswer` turns, model name, and context limit to exercise tools unique to your binary.

### Adaptive flows and forward compatibility

Flows adapt to what the binary announces in its handshake:

- Bash-scripting flows run only when `bash` is offered.
- Typed-result flows run only when `finish` is offered.
- Feed flows run only when `capabilities.feeds` is `true`.

`conformance.Run` returns a `Report` listing the flows that ran and any that were skipped. A full-featured binary can assert `len(report.Skipped) == 0` in CI.

Responses are decoded under the protocol's forward-compatibility rules. Unexpected fields on known DTOs fail the test unless covered by an announced capability. Unknown capability keys and unknown event types are tolerated.

## Fake endpoint for client tests (`conformance.NewFakeEndpoint`)

Wire clients can test against a real `poo` binary without running the full conformance suite by importing `conformance.NewFakeEndpoint`:

```go
endpoint := conformance.NewFakeEndpoint(
	conformance.ScriptedAnswer{Text: "hello from poo"},
	conformance.ScriptedAnswer{ToolCalls: []conformance.ToolCall{
		{Name: "bash", Arguments: map[string]any{"command": "echo hi"}},
	}},
)
defer endpoint.Close()

settings := filepath.Join(dir, "settings.json")
if err := endpoint.WriteSettings(settings, "conformance-model", 32768, nil); err != nil {
	return err
}
// Launch the binary with `--settings settings` and drive it over stdio.
```

`ScriptedAnswer` supports:

- `Text`: complete assistant reply text.
- `Chunks`: explicit delta chunks for testing streaming behavior.
- `ToolCalls`: ordered `conformance.ToolCall` entries (`Name`, `Arguments`).
- `Status` and `Error`: HTTP status and `ProviderError` body for rate-limit and error-handling tests.
- `Delay`: artificial latency before responding (useful for testing `run/message` interrupts or `run/cancel`).
- `FailAfterDelta`: emit initial text then break the stream mid-response.

When scripted turns are exhausted, the endpoint repeats its final turn so clients do not need to script every follow-up request. Call `endpoint.RequestBodies()` to inspect the exact HTTP request bodies the binary sent in order, and `endpoint.Close()` when finished.

## In-process test helpers (`poo/pootest`)

`poo/pootest` exports in-process test doubles for Go unit tests that construct a `poo.Session` directly. It imports only `poo`, `poo/decide`, and the standard library.

### Scripted transport (`pootest.Transport`)

`pootest.Transport` implements `poo.ChatTransport` over a fixed sequence of `pootest.Turn` values (`Text`, `Thinking`, `Calls`, `Finish`, token counts, `Blocks`, `Err`, and mid-stream `Fail`).

- Unlike `FakeEndpoint`, `pootest.Transport` returns `pootest.ErrScriptExhausted` when turns run out so a one-turn test fails fast instead of looping.
- Supports recording every `poo.ChatRequest` and rendered prompt snapshot, scripting fork turns (for compaction or sub-requests), and scripting native compaction responses.

### In-memory record sink (`pootest.Sink`)

`pootest.NewSink()` provides a thread-safe, in-memory `poo.RecordSink` and `ReadRecords` reader:

- Stamps sequence numbers and timestamps onto every accepted `poo.Record`.
- `WaitRecords(ctx, name, n)` blocks until at least `n` records of type `name` are recorded (avoiding polling loops in tests).
- `FailAt(n)` and `SetError(err)` inject store write failures on the *n*-th append (defaulting to `pootest.ErrSinkFailed`).

### Stub decider (`pootest.StubDecider`)

`pootest.NewStubDecider(probability)` implements `decide.Decider`, assigning `probability` to the first option of each question and splitting the remainder evenly across the rest:

- `SetChoice(name)` pins the returned choice; `SetError(err)` injects decision failures.
- `Calls()`, `Questions()`, and `States()` inspect every decision request made during the test.
- Pair with `pootest.NewRecordingSink()` to capture `decide.Decision` records.

### Scripted compactor (`pootest.Compactor`)

`pootest.NewCompactor(should, compaction)` implements `poo.Compactor` with a fixed decision and compaction payload:

- `SetShouldAfter(turns)` delays returning `true` from `ShouldCompact` until the view has at least `turns` turns.
- `SetError(err)` injects compaction errors.
- `Fork` and `ForkJSON` call the session's `poo.ForkFunc` while recording `ForkCall` arguments; `Calls()`, `Views()`, and `Forks()` inspect all invocations.
