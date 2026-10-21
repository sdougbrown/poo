# poo eval report

- started: 2026-10-21T00:37:28.093509511Z
- finished: 2026-10-21T01:04:28.616528947Z
- git sha: f6a832f
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 90.0% | 5.0 | 13.3 | 36717 | 764 | 13972 | 525 | 0.19 | 2.2% | 0.0% | 25.7% | 5946 | 83.4% |
| gemma4-think | 60 | 86.7% | 7.6 | 10.8 | 25874 | 1508 | 22207 | 269 | 11.21 | 1.5% | 0.0% | 21.3% | 2975 | 80.5% |
| minimax-m3.1-flash | 60 | 100.0% | 0.0 | 8.2 | 24842 | 2607 | 27762 | 1150 | -22.67 | 6.9% | 0.0% | 17.7% | 11 | 70.9% |
| qwen | 60 | 100.0% | 0.0 | 5.5 | 9989 | 687 | 8260 | 348 | 24.79 | 0.5% | 0.0% | 12.4% | 10 | n/a |
| **all** | 240 | 94.2% | 3.1 | 9.4 | 24355 | 1392 | 18050 | 573 | 3.38 | 2.8% | 0.0% | 19.3% | 2235 | 78.3% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 33.3% | 57.7 | 2.3 | -5.46 | 0.0% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 66.7% | 57.7 | 3.7 | 18.52 | 0.0% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 51.09 | 0.0% | 4.8% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 5.3 | 2.89 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 3.7 | 123.50 | 0.0% | 0.0% | 12 |
| find-off-by-one-loop | gemma4-think | 3 | 0.0% | 0.0 | 4.0 | 34.24 | 0.0% | 0.0% | 9 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 67.90 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 5.0 | 33.73 | 6.7% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -42.86 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | 14.48 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | -59.29 | 0.0% | 5.6% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 4.7 | 37.31 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 5.0 | -23.43 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 4.7 | 24.57 | 0.0% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | -46.52 | 5.9% | 3.7% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 4.3 | 32.65 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 6.3 | -47.42 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 5.0 | 5.27 | 0.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.3 | -143.30 | 0.0% | 13.7% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.0 | 33.92 | 0.0% | 0.0% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 25.7 | 23.17 | 8.1% | 76.0% | 4 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 16.3 | 2.88 | 8.1% | 49.5% | 4 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.3 | 1.28 | 12.8% | 21.8% | 5 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 7.0 | 41.60 | 0.0% | 30.6% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 11.3 | -3.60 | 0.0% | 35.6% | 2 |
| fix-csv-skips-header | gemma4-think | 3 | 66.7% | 57.7 | 7.3 | 65.21 | 0.0% | 22.2% | 1 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -0.05 | 13.0% | 11.1% | 3 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.0 | 15.63 | 0.0% | 6.7% | 3 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 9.0 | -16.84 | 0.0% | 33.3% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 9.3 | 1.97 | 0.0% | 22.2% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -96.07 | 9.8% | 20.9% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 6.3 | 30.14 | 0.0% | 27.8% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 13.0 | 0.05 | 0.0% | 33.3% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 66.7% | 57.7 | 7.7 | 1.43 | 0.0% | 11.1% | 1 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -9.65 | 12.3% | 14.5% | 4 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 5.3 | 38.48 | 0.0% | 11.1% | 5 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 14.7 | 6.97 | 2.2% | 25.0% | 118713 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 11.3 | 3.93 | 4.4% | 11.1% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -45.15 | 11.2% | 11.1% | 3 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 6.7 | 29.91 | 0.0% | 15.0% | 2 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 30.0 | 12.37 | 0.0% | 50.0% | 2 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 26.0 | -2.69 | 2.1% | 54.2% | 6 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.3 | 13.92 | 9.9% | 26.8% | 6 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 5.7 | 44.62 | 0.0% | 26.1% | 4 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 27.7 | 91.63 | 0.0% | 30.0% | 15 |
| fix-path-join-double-slash | gemma4-think | 3 | 66.7% | 57.7 | 16.7 | -46.83 | 8.2% | 46.8% | 4 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.3 | -47.60 | 6.8% | 38.7% | 23 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 5.3 | 29.66 | 0.0% | 6.7% | 4 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -15.05 | 11.1% | 33.3% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 12.7 | 5.72 | 0.0% | 33.3% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -77.22 | 11.2% | 15.3% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 5.7 | 31.74 | 0.0% | 0.0% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 16.0 | 4.40 | 4.2% | 50.0% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 12.3 | 2.10 | 0.0% | 37.8% | 3 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 21.44 | 11.1% | 8.9% | 4 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 5.7 | 24.58 | 0.0% | 28.3% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 10.3 | -1.61 | 13.3% | 33.3% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 13.0 | 11.17 | 0.0% | 25.0% | 59356 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 45.73 | 11.7% | 20.6% | 3 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 5.0 | 33.96 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 17.0 | -5.67 | 0.0% | 74.3% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 11.0 | -0.17 | 0.0% | 30.6% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.3 | -65.44 | 8.6% | 29.3% | 5 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 5.7 | 28.48 | 0.0% | 19.4% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 66.7% | 57.7 | 34.0 | 1.69 | 5.1% | 38.9% | 2 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 28.3 | 1.27 | 2.6% | 42.1% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -18.22 | 10.2% | 14.5% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.3 | 36.91 | 4.2% | 44.4% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 19.7 | 7.16 | 0.0% | 0.0% | 152 |
| long-change-timeout-everywhere | gemma4-think | 3 | 66.7% | 57.7 | 18.0 | -0.77 | 4.8% | 40.5% | 103 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 17.0 | -27.09 | 2.6% | 40.2% | 156 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 9.3 | -23.94 | 0.0% | 21.4% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -80.37 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 2.3 | 38.55 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -71.07 | 0.0% | 24.1% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 4.0 | 123.98 | 0.0% | 11.1% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -24.89 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | 43.36 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 4.7 | 51.83 | 0.0% | 28.9% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 2.3 | -130.42 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 58 |
| gemma4 | max_turns | 2 |
| gemma4-think | end_turn | 58 |
| gemma4-think | max_turns | 2 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 5 | 313 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 5 | 263 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | no | end_turn | 1 | 201 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 5 | 276 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1196 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1371 | 0 | 1 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1033 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 6 | 317 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 6 | 385 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 4 | 342 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 5 | 652 | 0 | 0 | 18 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 5 | 733 | 0 | 0 | 17 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 1 | 193 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | no | end_turn | 2 | 184 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | no | end_turn | 5 | 320 | 0 | 0 | 13 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | no | end_turn | 5 | 291 | 0 | 0 | 13 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1005 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1276 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1116 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 5 | 264 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 6 | 280 | 1 | 0 | 0 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 4 | 291 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 239 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 227 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 289 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 195 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 188 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 189 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1120 | 0 | 1 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1201 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 1200 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 4 | 291 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 5 | 355 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 5 | 301 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 5 | 321 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 5 | 419 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 5 | 418 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 5 | 212 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 4 | 207 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 5 | 229 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 955 | 0 | 1 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 1341 | 3 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 797 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 5 | 271 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 4 | 298 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 4 | 271 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 5 | 352 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 5 | 335 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 9 | 563 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 5 | 237 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 5 | 195 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 5 | 246 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 1359 | 0 | 1 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1585 | 0 | 1 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1179 | 0 | 1 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 4 | 229 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 3 | 232 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 5 | 250 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 25 | 877 | 2 | 12 | 4 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 26 | 909 | 2 | 13 | 4 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | yes | end_turn | 26 | 936 | 2 | 13 | 4 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/gemma4/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 19 | 328 | 2 | 5 | 3 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 15 | 258 | 1 | 3 | 4 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 15 | 288 | 1 | 3 | 4 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 1238 | 1 | 4 | 3 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 892 | 1 | 1 | 6 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 11 | 1222 | 2 | 1 | 7 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 6 | 308 | 0 | 1 | 3 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 7 | 310 | 0 | 1 | 3 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 8 | 314 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 16 | 401 | 0 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 9 | 511 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 9 | 349 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | no | end_turn | 2 | 225 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 10 | 260 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 10 | 240 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1268 | 1 | 0 | 4 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1490 | 1 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1220 | 2 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 5 | 307 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 6 | 326 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 7 | 428 | 0 | 0 | 4 | `runs/run-ce12218c336f16dc/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 9 | 493 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 9 | 449 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 9 | 420 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 9 | 321 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 9 | 256 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 10 | 211 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1664 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1191 | 1 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 853 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 6 | 333 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 6 | 305 | 0 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 7 | 361 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 14 | 422 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 11 | 359 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 14 | 436 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 11 | 278 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | no | end_turn | 1 | 277 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 11 | 211 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 884 | 1 | 1 | 4 | `runs/run-ce12218c336f16dc/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 992 | 1 | 1 | 5 | `runs/run-ce12218c336f16dc/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 931 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 5 | 306 | 0 | 0 | 5 | `runs/run-ce12218c336f16dc/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 6 | 301 | 0 | 1 | 5 | `runs/run-ce12218c336f16dc/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 5 | 307 | 0 | 0 | 4 | `runs/run-ce12218c336f16dc/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 15 | 482 | 0 | 1 | 178064 | `runs/run-ce12218c336f16dc/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 14 | 477 | 0 | 1 | 178064 | `runs/run-ce12218c336f16dc/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 15 | 480 | 1 | 1 | 12 | `runs/run-ce12218c336f16dc/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 16 | 318 | 2 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 9 | 289 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 9 | 201 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 850 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 925 | 1 | 0 | 5 | `runs/run-ce12218c336f16dc/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1411 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 7 | 301 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 7 | 323 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 417 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 25 | 734 | 0 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 40 | 767 | 0 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 25 | 800 | 0 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 16 | 355 | 1 | 5 | 13 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | max_turns | 50 | 336 | 0 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 12 | 310 | 0 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 12 | 891 | 1 | 3 | 5 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 790 | 1 | 1 | 10 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 1003 | 1 | 2 | 4 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 5 | 278 | 0 | 1 | 4 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 6 | 307 | 0 | 1 | 4 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 6 | 338 | 0 | 1 | 4 | `runs/run-ce12218c336f16dc/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | max_turns | 50 | 1289 | 0 | 1 | 16 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | end_turn | 13 | 1436 | 0 | 2 | 14 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/gemma4/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 3 | yes | end_turn | 20 | 2624 | 0 | 1 | 16 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/gemma4/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 1 | no | end_turn | 10 | 621 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | yes | end_turn | 24 | 346 | 0 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 3 | yes | end_turn | 16 | 372 | 2 | 4 | 7 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 12 | 1072 | 1 | 9 | 18 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 1083 | 1 | 6 | 33 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 1052 | 1 | 4 | 17 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 5 | 269 | 0 | 0 | 4 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 5 | 257 | 0 | 0 | 5 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 6 | 303 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 10 | 392 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 10 | 300 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 10 | 301 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 14 | 257 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 11 | 219 | 0 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 13 | 235 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1110 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1111 | 1 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1183 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 5 | 271 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 7 | 289 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 5 | 272 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 15 | 378 | 1 | 2 | 4 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 14 | 412 | 0 | 2 | 3 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 19 | 507 | 1 | 3 | 3 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 13 | 303 | 0 | 2 | 3 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 12 | 263 | 0 | 2 | 3 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 12 | 236 | 0 | 2 | 3 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1132 | 1 | 1 | 3 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1079 | 1 | 0 | 5 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 868 | 1 | 1 | 3 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 6 | 265 | 0 | 0 | 3 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 6 | 342 | 0 | 3 | 3 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 5 | 310 | 0 | 1 | 3 | `runs/run-ce12218c336f16dc/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 11 | 388 | 2 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 11 | 295 | 2 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 9 | 405 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 14 | 350 | 0 | 1 | 178064 | `runs/run-ce12218c336f16dc/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 10 | 284 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 15 | 346 | 0 | 2 | 2 | `runs/run-ce12218c336f16dc/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1018 | 1 | 2 | 4 | `runs/run-ce12218c336f16dc/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1112 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1238 | 1 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 6 | 294 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 5 | 289 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 4 | 281 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 15 | 396 | 0 | 5 | 2 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 20 | 483 | 0 | 8 | 2 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 16 | 507 | 0 | 5 | 2 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 10 | 333 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 13 | 289 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 10 | 204 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 1182 | 1 | 5 | 4 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 1387 | 1 | 2 | 4 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1125 | 1 | 3 | 6 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 6 | 282 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 5 | 289 | 0 | 0 | 2 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 6 | 287 | 0 | 1 | 2 | `runs/run-ce12218c336f16dc/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 31 | 595 | 1 | 3 | 3 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | no | max_turns | 50 | 766 | 1 | 0 | 0 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 21 | 869 | 2 | 6 | 3 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 18 | 333 | 0 | 3 | 3 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | max_turns | 50 | 320 | 1 | 3 | 3 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 17 | 291 | 1 | 2 | 3 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1162 | 1 | 1 | 4 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 980 | 1 | 1 | 4 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 885 | 1 | 1 | 4 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 6 | 281 | 0 | 2 | 3 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 8 | 282 | 1 | 2 | 5 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 5 | 293 | 0 | 2 | 4 | `runs/run-ce12218c336f16dc/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 16 | 720 | 0 | 0 | 152 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 19 | 464 | 0 | 0 | 152 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 24 | 741 | 0 | 0 | 152 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 20 | 358 | 1 | 1 | 152 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | no | end_turn | 12 | 330 | 1 | 0 | 0 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | yes | end_turn | 22 | 289 | 0 | 5 | 156 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 19 | 1435 | 1 | 13 | 156 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 16 | 1683 | 0 | 13 | 156 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 16 | 1155 | 1 | 6 | 156 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 10 | 625 | 0 | 2 | 156 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 8 | 683 | 0 | 1 | 156 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 10 | 942 | 0 | 2 | 156 | `runs/run-ce12218c336f16dc/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 4 | 447 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 4 | 294 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 4 | 293 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 2 | 165 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 2 | 159 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 3 | 206 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1216 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 1363 | 0 | 2 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1243 | 0 | 3 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 4 | 415 | 0 | 1 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 4 | 663 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 4 | 647 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 3 | 304 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 3 | 242 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 3 | 306 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 3 | 184 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 3 | 209 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 3 | 250 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 1058 | 0 | 1 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1396 | 0 | 1 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 4 | 1222 | 0 | 2 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 2 | 506 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 3 | 272 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 2 | 719 | 0 | 0 | 0 | `runs/run-ce12218c336f16dc/long-find-error-in-log/qwen/seed3/session.jsonl` |
