# poo eval report

- started: 2026-10-21T22:49:43.025504224Z
- finished: 2026-10-21T23:16:43.424169504Z
- git sha: a6ffc43
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 91.7% | 2.9 | 13.3 | 35021 | 817 | 14162 | 537 | -12.62 | 2.9% | 0.0% | 25.4% | 11 | 83.0% |
| gemma4-think | 60 | 93.3% | 2.9 | 10.1 | 19652 | 1601 | 22089 | 280 | 9.71 | 2.9% | 0.0% | 22.4% | 9 | 79.7% |
| minimax-m3.1-flash | 60 | 100.0% | 0.0 | 8.1 | 26530 | 2397 | 25767 | 1153 | 10.31 | 6.7% | 0.0% | 17.5% | 11 | 70.6% |
| qwen | 60 | 100.0% | 0.0 | 5.5 | 8899 | 643 | 7705 | 344 | 21.05 | 0.2% | 0.0% | 11.0% | 9 | n/a |
| **all** | 240 | 96.2% | 1.3 | 9.3 | 22525 | 1365 | 17431 | 578 | 7.11 | 3.2% | 0.0% | 19.1% | 10 | 77.8% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 33.3% | 57.7 | 2.3 | -24.26 | 0.0% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 5.0 | 7.84 | 0.0% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | -49.99 | 0.0% | 9.3% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 4.0 | 39.13 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 4.0 | 117.12 | 0.0% | 0.0% | 12 |
| find-off-by-one-loop | gemma4-think | 3 | 0.0% | 0.0 | 5.0 | 49.29 | 0.0% | 0.0% | 13 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.3 | -4.07 | 0.0% | 7.9% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 3.7 | 21.47 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -36.53 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 3.7 | 35.63 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -6.96 | 0.0% | 9.7% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 5.0 | 26.64 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 5.0 | -31.75 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | 5.22 | 0.0% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 11.0 | 1.40 | 0.0% | 12.8% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 4.3 | -18.95 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 7.7 | -12.37 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 5.0 | -2.36 | 0.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | -14.01 | 0.0% | 17.3% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.7 | -43.74 | 0.0% | 0.0% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 19.3 | 22.50 | 7.2% | 56.6% | 4 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 15.0 | 8.33 | 16.7% | 36.5% | 5 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | -3.58 | 11.2% | 19.8% | 7 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 7.0 | 11.46 | 3.7% | 25.0% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 10.7 | 32.23 | 0.0% | 8.3% | 3 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 15.0 | 9.86 | 14.1% | 37.8% | 2 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -38.67 | 11.4% | 11.1% | 2 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.7 | 23.04 | 0.0% | 16.7% | 2 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 9.0 | -16.59 | 0.0% | 33.3% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 9.3 | 8.90 | 3.7% | 33.3% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 15.74 | 11.7% | 10.3% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 6.3 | 20.13 | 0.0% | 16.7% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 13.7 | 16.29 | 2.4% | 35.6% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -13.74 | 0.0% | 24.4% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 23.81 | 11.6% | 16.2% | 2 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 6.0 | 34.97 | 0.0% | 24.4% | 2 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 15.0 | 36.82 | 6.4% | 30.0% | 5 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 11.3 | -8.04 | 11.4% | 22.2% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -26.31 | 10.7% | 6.7% | 4 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 7.0 | 33.45 | 0.0% | 6.7% | 2 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 37.3 | 5.49 | 0.0% | 50.0% | 2 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 12.7 | 7.32 | 2.8% | 46.7% | 3 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | 532.64 | 11.2% | 17.0% | 6 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.7 | 44.24 | 0.0% | 17.8% | 3 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 28.7 | 15.49 | 0.0% | 48.2% | 17 |
| fix-path-join-double-slash | gemma4-think | 3 | 100.0% | 0.0 | 16.0 | 11.46 | 4.8% | 51.4% | 20 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -13.08 | 9.8% | 15.6% | 24 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 5.3 | 40.26 | 0.0% | 0.0% | 7 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -14.82 | 11.1% | 33.3% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 24.3 | 2.42 | 0.0% | 36.1% | 7 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -4.04 | 11.2% | 16.4% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 5.3 | 35.21 | 0.0% | 6.7% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 16.7 | 0.86 | 6.4% | 50.0% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 13.0 | 7.04 | 0.0% | 43.3% | 3 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -46.18 | 10.7% | 14.0% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 5.3 | 21.39 | 0.0% | 0.0% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 10.3 | -1.99 | 17.5% | 22.2% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 12.3 | 3.50 | 0.0% | 19.4% | 2 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -50.13 | 11.4% | 11.4% | 2 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 5.0 | -16.97 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 18.7 | -10.34 | 0.0% | 62.5% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 11.7 | 6.00 | 0.0% | 35.6% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 35.17 | 10.9% | 24.4% | 2 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 5.7 | 135.59 | 0.0% | 31.7% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 33.3 | -10.17 | 6.4% | 54.2% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 15.7 | -0.44 | 4.4% | 45.3% | 4 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -61.41 | 11.6% | 17.7% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.3 | -55.84 | 0.0% | 44.4% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 13.0 | -88.72 | 0.0% | 23.3% | 152 |
| long-change-timeout-everywhere | gemma4-think | 3 | 66.7% | 57.7 | 8.7 | 1.49 | 0.0% | 16.7% | 105 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 16.3 | -19.69 | 1.1% | 40.9% | 156 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 10.0 | 27.69 | 0.0% | 16.7% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -57.23 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 2.0 | 16.53 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -41.04 | 0.0% | 33.7% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 4.0 | 149.47 | 0.0% | 13.9% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -194.47 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | 38.01 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.0 | -23.34 | 0.0% | 37.6% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 2.3 | -107.54 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 57 |
| gemma4 | max_turns | 3 |
| gemma4-think | end_turn | 58 |
| gemma4-think | max_turns | 1 |
| gemma4-think | output_truncated | 1 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 5 | 368 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 5 | 253 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 5 | 259 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 5 | 189 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 933 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 898 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1049 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 4 | 255 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 4 | 270 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 4 | 261 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 5 | 609 | 0 | 0 | 18 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 6 | 579 | 0 | 0 | 17 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 1 | 148 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | no | end_turn | 5 | 247 | 0 | 0 | 13 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | no | end_turn | 5 | 309 | 0 | 0 | 13 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | no | end_turn | 5 | 294 | 0 | 0 | 14 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1582 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 2165 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 11 | 1807 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 3 | 224 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 4 | 224 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 4 | 263 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 281 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 232 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 231 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 168 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 193 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 3 | 202 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1219 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 986 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 868 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 4 | 280 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 6 | 253 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 5 | 277 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 5 | 366 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 5 | 260 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 5 | 348 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 4 | 266 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 4 | 283 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 4 | 224 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 1183 | 0 | 2 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 1012 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 12 | 974 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 5 | 315 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 4 | 272 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 4 | 402 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 9 | 413 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 5 | 379 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 9 | 326 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 5 | 200 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 5 | 190 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 5 | 236 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1088 | 0 | 2 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1192 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 951 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 5 | 937 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 5 | 268 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 4 | 314 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 26 | 937 | 2 | 13 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 18 | 1153 | 1 | 6 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | yes | end_turn | 14 | 1216 | 1 | 2 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/gemma4/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 15 | 380 | 3 | 2 | 7 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 14 | 235 | 2 | 2 | 5 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 16 | 263 | 2 | 3 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1134 | 1 | 1 | 7 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1085 | 1 | 1 | 7 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1105 | 1 | 2 | 7 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 10 | 549 | 1 | 1 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 6 | 320 | 0 | 1 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 5 | 355 | 0 | 1 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 13 | 478 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 9 | 656 | 0 | 0 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 10 | 520 | 0 | 0 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 14 | 365 | 2 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 14 | 232 | 2 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 17 | 263 | 2 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1079 | 1 | 0 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 916 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1256 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 7 | 277 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 6 | 327 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 7 | 333 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 9 | 356 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 9 | 435 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 9 | 467 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 9 | 232 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 9 | 262 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 10 | 268 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 918 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 801 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 993 | 1 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 8 | 361 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 5 | 278 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 6 | 284 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 13 | 653 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 13 | 670 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 15 | 646 | 1 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 10 | 336 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 8 | 383 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 12 | 276 | 0 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1102 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 927 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1035 | 1 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 6 | 307 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 7 | 344 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 5 | 266 | 0 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 15 | 1313 | 1 | 1 | 12 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 13 | 1373 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 17 | 952 | 2 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 10 | 232 | 1 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 14 | 407 | 3 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 10 | 608 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 998 | 1 | 0 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 924 | 1 | 0 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1078 | 1 | 1 | 5 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 9 | 336 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 6 | 334 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 356 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 23 | 523 | 0 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 39 | 555 | 0 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | max_turns | 50 | 535 | 0 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 13 | 360 | 0 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 12 | 277 | 0 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 13 | 225 | 1 | 2 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 2484 | 1 | 1 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 3639 | 1 | 2 | 11 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1039 | 1 | 1 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 6 | 293 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 8 | 309 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 6 | 299 | 0 | 0 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | end_turn | 19 | 720 | 0 | 5 | 16 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | max_turns | 50 | 642 | 0 | 1 | 18 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/gemma4/seed2/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4 | 3 | yes | end_turn | 17 | 661 | 0 | 4 | 16 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/gemma4/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 1 | yes | end_turn | 18 | 347 | 0 | 1 | 7 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | yes | output_truncated | 15 | 319 | 0 | 6 | 29 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` (poo exited 10) |
| fix-path-join-double-slash | gemma4-think | 3 | yes | end_turn | 15 | 267 | 2 | 5 | 24 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 936 | 1 | 2 | 26 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1244 | 1 | 1 | 20 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 851 | 1 | 1 | 27 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 5 | 290 | 0 | 0 | 10 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 4 | 277 | 0 | 0 | 5 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 7 | 308 | 0 | 0 | 5 | `runs/run-5ed4e0182ed4bfd9/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 10 | 390 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 10 | 301 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 10 | 302 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 10 | 250 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 13 | 254 | 0 | 2 | 16 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | max_turns | 50 | 366 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/gemma4-think/seed3/session.jsonl` (poo exited 11) |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1086 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1062 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 965 | 1 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 6 | 294 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 5 | 264 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 5 | 270 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 18 | 594 | 1 | 3 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 15 | 480 | 1 | 2 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 17 | 476 | 1 | 3 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 12 | 355 | 0 | 2 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 12 | 241 | 0 | 2 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 15 | 246 | 0 | 3 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1129 | 1 | 1 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1056 | 1 | 1 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 888 | 1 | 1 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 5 | 277 | 0 | 0 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 6 | 340 | 0 | 0 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 5 | 324 | 0 | 0 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 11 | 402 | 2 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 9 | 381 | 1 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 11 | 438 | 2 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 13 | 282 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 13 | 307 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 11 | 269 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1345 | 1 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1032 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 871 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 6 | 317 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 4 | 411 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 5 | 282 | 0 | 0 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 19 | 580 | 0 | 5 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 21 | 533 | 0 | 5 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 16 | 559 | 0 | 5 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 10 | 302 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 10 | 246 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 15 | 236 | 0 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1150 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1060 | 1 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1048 | 1 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 7 | 296 | 0 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 5 | 272 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 5 | 593 | 0 | 1 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | max_turns | 50 | 634 | 1 | 5 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` (poo exited 11) |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 35 | 706 | 1 | 3 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 15 | 573 | 2 | 2 | 2 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 14 | 321 | 0 | 2 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 14 | 280 | 1 | 2 | 6 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 19 | 247 | 1 | 5 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1138 | 1 | 2 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1029 | 1 | 1 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1340 | 1 | 1 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 6 | 676 | 0 | 1 | 3 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 5 | 304 | 0 | 2 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 8 | 354 | 0 | 2 | 4 | `runs/run-5ed4e0182ed4bfd9/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 17 | 616 | 0 | 0 | 152 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 8 | 1164 | 0 | 14 | 152 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 14 | 662 | 0 | 0 | 152 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 12 | 490 | 0 | 2 | 152 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 12 | 432 | 0 | 0 | 162 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | no | end_turn | 2 | 349 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 19 | 1288 | 1 | 14 | 156 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 13 | 1394 | 0 | 4 | 156 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 17 | 1448 | 0 | 11 | 156 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 12 | 483 | 0 | 3 | 156 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 10 | 485 | 0 | 0 | 156 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 8 | 554 | 0 | 0 | 156 | `runs/run-5ed4e0182ed4bfd9/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 4 | 407 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 4 | 295 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 4 | 295 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 2 | 162 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 2 | 164 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 2 | 161 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1107 | 0 | 3 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 916 | 0 | 2 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 871 | 0 | 5 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 4 | 444 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 4 | 414 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 4 | 483 | 0 | 1 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 3 | 559 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 3 | 437 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 3 | 429 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 3 | 431 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 3 | 212 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 3 | 149 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 764 | 0 | 2 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 835 | 0 | 6 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 4 | 888 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 3 | 418 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 2 | 271 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 2 | 217 | 0 | 0 | 0 | `runs/run-5ed4e0182ed4bfd9/long-find-error-in-log/qwen/seed3/session.jsonl` |
