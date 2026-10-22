# poo eval report

- started: 2026-10-21T07:03:08.291673616Z
- finished: 2026-10-21T07:25:40.332725192Z
- git sha: 1b0ac14
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 91.7% | 2.9 | 12.1 | 30465 | 586 | 11962 | 577 | 0.79 | 2.7% | 0.0% | 27.5% | 10 | 80.9% |
| gemma4-think | 60 | 85.0% | 5.0 | 9.2 | 13787 | 1391 | 18245 | 263 | 13.61 | 3.0% | 0.0% | 22.1% | 8 | 79.5% |
| minimax-m3.1-flash | 60 | 100.0% | 0.0 | 8.0 | 24820 | 2208 | 26257 | 1217 | -16.61 | 7.2% | 0.0% | 15.9% | 10 | 69.1% |
| qwen | 60 | 100.0% | 0.0 | 5.2 | 10041 | 669 | 6308 | 321 | 39.42 | 0.5% | 0.0% | 12.8% | 10 | n/a |
| **all** | 240 | 94.2% | 1.4 | 8.6 | 19778 | 1213 | 15693 | 595 | 9.30 | 3.4% | 0.0% | 19.6% | 10 | 76.5% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 66.7% | 57.7 | 3.7 | -26.91 | 0.0% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 5.7 | 18.93 | 0.0% | 8.3% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -53.87 | 0.0% | 5.6% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 5.0 | 30.59 | 0.0% | 9.5% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 3.7 | 110.82 | 0.0% | 0.0% | 11 |
| find-off-by-one-loop | gemma4-think | 3 | 0.0% | 0.0 | 5.0 | 30.87 | 0.0% | 0.0% | 13 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.0 | -30.55 | 5.9% | 0.0% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 4.3 | 32.96 | 9.5% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -65.35 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | 25.94 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -49.99 | 0.0% | 8.3% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 4.0 | 33.24 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 5.0 | -16.16 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 66.7% | 57.7 | 3.3 | 15.64 | 0.0% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | -8.16 | 0.0% | 6.7% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 4.3 | -42.79 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 5.0 | -4.43 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 5.3 | -0.12 | 0.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -42.41 | 0.0% | 17.9% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.0 | 5.62 | 0.0% | 5.6% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 24.7 | 31.44 | 8.5% | 75.0% | 4 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 14.0 | 8.75 | 7.2% | 36.3% | 15 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.3 | 33.96 | 8.1% | 31.4% | 6 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 7.3 | 39.31 | 0.0% | 10.3% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 10.3 | -2.90 | 0.0% | 42.2% | 2 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 14.3 | 1.60 | 12.2% | 43.3% | 2 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -147.74 | 11.2% | 16.7% | 2 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 5.0 | 33.41 | 0.0% | 22.2% | 2 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 8.7 | -11.44 | 0.0% | 33.3% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 9.7 | 4.80 | 7.4% | 33.3% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -58.19 | 11.2% | 18.1% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 5.0 | 49.44 | 0.0% | 27.8% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 12.0 | 6.89 | 0.0% | 33.3% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 11.7 | 1.81 | 8.8% | 35.6% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 252.19 | 12.2% | 14.5% | 3 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 6.7 | 34.83 | 0.0% | 37.3% | 2 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 15.3 | 7.79 | 6.9% | 25.0% | 8 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | 3.28 | 0.0% | 0.0% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 22.47 | 12.6% | 15.6% | 3 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 6.3 | 46.97 | 0.0% | 15.0% | 3 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 21.7 | 7.40 | 0.0% | 50.0% | 2 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 12.3 | 7.20 | 2.6% | 53.3% | 3 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | 21.51 | 10.2% | 16.4% | 8 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.0 | 34.10 | 0.0% | 28.2% | 3 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 20.0 | 111.87 | 0.0% | 46.4% | 12 |
| fix-path-join-double-slash | gemma4-think | 3 | 66.7% | 57.7 | 11.3 | -1.39 | 12.8% | 44.4% | 2 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | 30.47 | 9.3% | 15.8% | 15 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 5.0 | 38.81 | 0.0% | 0.0% | 10 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -20.55 | 11.1% | 33.3% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 66.7% | 57.7 | 9.7 | 13.72 | 0.0% | 8.3% | 1 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -1.88 | 12.0% | 8.9% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 5.3 | 34.11 | 0.0% | 6.7% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 17.7 | 6.84 | 6.0% | 52.4% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 66.7% | 57.7 | 10.0 | -6.15 | 0.0% | 26.7% | 2 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -16.38 | 12.5% | 18.1% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 4.3 | 56.34 | 0.0% | 0.0% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -11.32 | 13.3% | 22.2% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 12.3 | 8.80 | 5.1% | 36.1% | 2 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -89.38 | 12.0% | 18.9% | 2 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 4.7 | 20.91 | 0.0% | 13.3% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 16.7 | 8.81 | 2.2% | 68.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 11.7 | 15.73 | 0.0% | 36.1% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | 50.79 | 11.6% | 14.8% | 2 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 5.3 | 33.67 | 0.0% | 26.2% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 66.7% | 57.7 | 28.3 | 5.27 | 6.6% | 35.7% | 2 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 16.0 | -0.21 | 4.2% | 54.2% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 18.46 | 12.0% | 16.7% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.3 | 55.36 | 0.0% | 42.9% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 18.3 | 7.71 | 0.0% | 33.3% | 153 |
| long-change-timeout-everywhere | gemma4-think | 3 | 33.3% | 57.7 | 11.3 | 99.28 | 0.0% | 25.0% | 103 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 14.7 | -12.53 | 2.8% | 37.0% | 158 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 9.3 | 37.13 | 0.0% | 0.0% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -75.19 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 2.0 | 12.62 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -89.58 | 0.0% | 19.2% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 4.3 | 201.88 | 0.0% | 11.1% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -54.80 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | 11.17 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.3 | -161.47 | 0.0% | 17.3% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 2.3 | 12.47 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 59 |
| gemma4 | max_turns | 1 |
| gemma4-think | end_turn | 58 |
| gemma4-think | provider_error | 2 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 5 | 361 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 5 | 552 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 6 | 262 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 6 | 255 | 0 | 1 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 5 | 257 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1217 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1198 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1074 | 0 | 1 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 6 | 250 | 0 | 2 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 5 | 268 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 4 | 233 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 5 | 732 | 0 | 0 | 17 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 1 | 196 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 5 | 535 | 0 | 0 | 17 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | no | end_turn | 5 | 250 | 0 | 0 | 13 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | no | end_turn | 5 | 230 | 0 | 0 | 13 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | no | end_turn | 5 | 198 | 0 | 0 | 14 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 1134 | 3 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1092 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1408 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 6 | 259 | 2 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 3 | 237 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 4 | 235 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 492 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 232 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 234 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 238 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 235 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 199 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 940 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1218 | 0 | 2 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 973 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 4 | 277 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 4 | 307 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 4 | 296 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 5 | 326 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 5 | 261 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 5 | 338 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 5 | 200 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | no | end_turn | 1 | 155 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 4 | 210 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1067 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 1159 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1016 | 0 | 2 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 5 | 265 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 4 | 473 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 4 | 294 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 5 | 275 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 5 | 255 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 5 | 236 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 6 | 187 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 5 | 179 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 5 | 197 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1259 | 0 | 1 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1013 | 0 | 2 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1178 | 0 | 1 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 5 | 434 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 3 | 283 | 0 | 1 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 4 | 255 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 24 | 2282 | 2 | 12 | 4 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 25 | 2100 | 2 | 12 | 4 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | yes | end_turn | 25 | 1725 | 2 | 12 | 4 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/gemma4/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 11 | 313 | 0 | 1 | 4 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 14 | 279 | 2 | 2 | 3 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 17 | 307 | 1 | 5 | 39 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1224 | 1 | 3 | 3 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1317 | 1 | 3 | 7 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 13 | 1091 | 1 | 4 | 8 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 7 | 323 | 0 | 0 | 4 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 8 | 312 | 0 | 1 | 3 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 7 | 371 | 0 | 1 | 3 | `runs/run-b978db97a1d7a2e6/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 13 | 502 | 0 | 3 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 9 | 675 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 9 | 631 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 12 | 309 | 1 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 15 | 311 | 2 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 16 | 278 | 2 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1167 | 1 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1535 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1528 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 6 | 319 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 5 | 262 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 4 | 277 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 8 | 413 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 9 | 388 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 9 | 426 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 10 | 293 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 9 | 218 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 10 | 371 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1556 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1120 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 967 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 4 | 354 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 7 | 300 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 4 | 282 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 13 | 353 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 12 | 514 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 11 | 526 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 10 | 324 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 12 | 305 | 2 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 13 | 251 | 1 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1025 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1947 | 1 | 1 | 4 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 922 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 7 | 295 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 6 | 270 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 7 | 315 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 17 | 513 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 14 | 642 | 1 | 1 | 12 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 15 | 489 | 1 | 1 | 11 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 9 | 306 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 13 | 304 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 9 | 319 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1303 | 1 | 1 | 5 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 857 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 919 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 5 | 285 | 0 | 1 | 4 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 8 | 341 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 306 | 0 | 0 | 4 | `runs/run-b978db97a1d7a2e6/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 29 | 744 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 10 | 489 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 26 | 724 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | provider_error | 14 | 325 | 1 | 3 | 4 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 11 | 257 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 12 | 267 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 1064 | 1 | 2 | 16 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1021 | 1 | 1 | 4 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1248 | 1 | 1 | 4 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 6 | 280 | 0 | 1 | 4 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 6 | 289 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 6 | 307 | 0 | 3 | 4 | `runs/run-b978db97a1d7a2e6/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | end_turn | 17 | 925 | 0 | 4 | 10 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | end_turn | 15 | 2117 | 0 | 4 | 9 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/gemma4/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 3 | yes | end_turn | 28 | 721 | 0 | 1 | 16 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/gemma4/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 1 | no | end_turn | 10 | 354 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | yes | end_turn | 12 | 308 | 2 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 3 | yes | provider_error | 12 | 226 | 1 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1256 | 1 | 1 | 15 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1135 | 1 | 2 | 16 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1466 | 1 | 1 | 13 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 5 | 275 | 0 | 0 | 17 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 5 | 278 | 0 | 0 | 6 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 5 | 262 | 0 | 0 | 8 | `runs/run-b978db97a1d7a2e6/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 10 | 391 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 10 | 454 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 10 | 465 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 14 | 290 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | no | end_turn | 2 | 219 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 13 | 289 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 970 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1067 | 1 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1338 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 5 | 288 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 5 | 263 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 6 | 312 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 17 | 407 | 1 | 3 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 18 | 427 | 1 | 4 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 18 | 521 | 1 | 3 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 13 | 318 | 0 | 2 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 12 | 265 | 0 | 2 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | no | end_turn | 5 | 230 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1229 | 1 | 1 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1021 | 1 | 2 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1050 | 1 | 2 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 4 | 274 | 0 | 0 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 5 | 321 | 0 | 0 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 4 | 270 | 0 | 0 | 3 | `runs/run-b978db97a1d7a2e6/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 8 | 378 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 11 | 482 | 2 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 11 | 470 | 2 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 11 | 325 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 14 | 295 | 2 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 12 | 228 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1297 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1156 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 925 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 4 | 370 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 4 | 267 | 0 | 0 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 6 | 266 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 16 | 534 | 1 | 5 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 18 | 544 | 0 | 7 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 16 | 551 | 0 | 5 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 12 | 278 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 13 | 351 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 10 | 295 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1132 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1196 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 984 | 1 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 5 | 253 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 6 | 284 | 0 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 5 | 280 | 0 | 1 | 2 | `runs/run-b978db97a1d7a2e6/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | no | max_turns | 50 | 692 | 1 | 0 | 0 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 16 | 810 | 1 | 2 | 2 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 19 | 769 | 2 | 4 | 3 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 15 | 338 | 1 | 3 | 3 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 14 | 295 | 0 | 3 | 3 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 19 | 300 | 1 | 5 | 3 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1117 | 1 | 1 | 4 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1205 | 1 | 1 | 4 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1349 | 1 | 1 | 4 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 8 | 307 | 0 | 2 | 3 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 5 | 432 | 0 | 2 | 4 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 6 | 320 | 0 | 3 | 4 | `runs/run-b978db97a1d7a2e6/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 15 | 759 | 0 | 0 | 152 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 25 | 602 | 0 | 1 | 152 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 15 | 623 | 0 | 2 | 156 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 18 | 264 | 0 | 2 | 152 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | no | end_turn | 2 | 358 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | no | end_turn | 14 | 405 | 0 | 1 | 156 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 16 | 1556 | 1 | 7 | 156 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 14 | 1674 | 1 | 8 | 163 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 14 | 1257 | 0 | 5 | 156 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 10 | 580 | 0 | 0 | 156 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 9 | 599 | 0 | 0 | 156 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 9 | 518 | 0 | 0 | 156 | `runs/run-b978db97a1d7a2e6/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 4 | 439 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 4 | 292 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 4 | 294 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 2 | 161 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 2 | 165 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 2 | 163 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 1819 | 0 | 1 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1886 | 0 | 3 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1497 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 3 | 628 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 5 | 526 | 0 | 1 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 5 | 564 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 3 | 306 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 3 | 248 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 3 | 246 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 3 | 188 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 3 | 151 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 3 | 148 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1383 | 0 | 1 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1240 | 0 | 3 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 4 | 1063 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 3 | 233 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 2 | 223 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 2 | 240 | 0 | 0 | 0 | `runs/run-b978db97a1d7a2e6/long-find-error-in-log/qwen/seed3/session.jsonl` |
