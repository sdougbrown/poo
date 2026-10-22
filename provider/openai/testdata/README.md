# The recorded streams

The fixtures the transport's tests read. Each one is a raw SSE
response, exactly as an endpoint sent it.

| Fixture | Where it came from |
|---|---|
| `vllm-empty-tool-calls.sse` | **Recorded.** The homelab vLLM's gemma4 parser, copied from `evals/results/2026-10-22-071648-e1346cb/gemma4-empty-tool-calls.log` (2026-10-22). The stream ends with `finish_reason: "tool_calls"` and no tool call. |
| `vllm-answer.sse` | **Shaped.** The vLLM chunk shape of the recorded stream above, with an answer and a usage chunk. |
| `vllm-answer-no-usage.sse` | **Shaped.** The same shape without the usage chunk. |
| `llamacpp-answer.sse` | **Shaped.** The llama.cpp server's chunk shape, with the timings object its last chunk carries. |
| `llamacpp-answer-no-usage.sse` | **Shaped.** The same shape without the usage chunk. |
| `vllm-text-tool-call.sse` | **Shaped.** The vLLM shape with a Hermes-style tool call in the text. |
| `vllm-qwen-xml-tool-call.sse` | **Shaped.** The vLLM shape with a Qwen-style XML tool call in the text. |
| `vllm-mistral-tool-calls.sse` | **Shaped.** The vLLM shape with a Mistral-style `[TOOL_CALLS]` list in the text. |
| `vllm-native-tool-calls.sse` | **Shaped.** The vLLM shape with a native tool call. |
| `vllm-think-tags.sse` | **Shaped.** The vLLM shape with the thinking in the model's own tags. |

A **recorded** fixture is a response an endpoint really sent. A
**shaped** one is written to the wire shape of that endpoint, from the
recorded stream above or from the endpoint's documented format: no
endpoint was reachable while these were written, so the shaped fixtures
have to be re-recorded against the real servers before they are treated
as evidence.

The rule in the plan is that a quirk ships with a recorded fixture. The
fixtures that are shaped are the ones that need a real run; the quirk
they cover is exercised by the test that reads them.

## The error bodies

`errors/` holds one error body per row of the plan's error-kind table.
The body is the raw payload an endpoint sent; the status the row is
about is named by the test that reads it.

| Fixture | The row it covers |
|---|---|
| `errors/429-quota.json` | a 429 whose code is a quota code → `usage_limit` |
| `errors/429-usage-limit.json` | a 429 whose code is `usage_limit` → `usage_limit` |
| `errors/429-quota-rate-limit.json` | a 429 whose code is a rate limit quota code → `usage_limit` |
| `errors/429-rate-limited.json` | another 429 → `rate_limited` |
| `errors/402-payment-required.json` | a 402 → `plan_inactive` |
| `errors/400-billing.json` | a billing code → `plan_inactive` |
| `errors/401-unauthorized.json` | a 401 → `auth_failed` |
| `errors/403-forbidden.json` | a 403 → `auth_failed` |
| `errors/400-invalid-request.json` | another 4xx → `invalid_request` |
| `errors/500-server.json` | a 5xx → `server` |
| `errors/400-context-length.json` | a context-length code → `context_length` |

These are **shaped** as well: no endpoint was reachable to record them,
and the codes the rows name are the ones the plan's table lists.
