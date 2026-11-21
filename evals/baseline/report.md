# poo eval report

- started: 2026-11-21T06:08:35.926341083Z
- finished: 2026-11-21T06:34:18.989332442Z
- git sha: fc224a87
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 98.3% | 2.9 | 9.5 | 40408 | 490 | 9019 | 625 | -22.68 | 1.8% | 0.0% | 9.0% | 11 | 87.8% |
| gemma4-think | 60 | 95.0% | 5.0 | 8.8 | 35619 | 654 | 10047 | 318 | -37.26 | 0.1% | 0.0% | 10.1% | 12 | 88.8% |
| minimax-m3.1-flash | 60 | 100.0% | 0.0 | 7.6 | 40415 | 2048 | 23834 | 1360 | -25.05 | 0.1% | 0.0% | 12.6% | 11 | 91.6% |
| qwen | 60 | 100.0% | 0.0 | 6.0 | 24692 | 440 | 8191 | 684 | -103.72 | 0.0% | 0.0% | 2.3% | 10 | n/a |
| **all** | 240 | 98.3% | 1.9 | 8.0 | 35284 | 908 | 12773 | 747 | -47.18 | 0.5% | 0.0% | 8.5% | 11 | 89.4% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 100.0% | 0.0 | 6.3 | -35.56 | 11.1% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 7.7 | -32.09 | 0.0% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -65.31 | 0.0% | 3.7% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 5.3 | -28.00 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 66.7% | 57.7 | 10.3 | -30.82 | 0.0% | 60.0% | 3 |
| find-off-by-one-loop | gemma4-think | 3 | 33.3% | 57.7 | 8.3 | 17.09 | 0.0% | 36.7% | 8 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.3 | -297.71 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 6.0 | -72.78 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -54.29 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -184.59 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | -26.47 | 0.0% | 0.0% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 4.3 | -349.26 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 6.0 | -125.30 | 11.1% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 7.0 | -30.49 | 0.0% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.0 | -140.12 | 0.0% | 11.7% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 4.3 | -120.00 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 5.3 | -73.12 | 6.7% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 6.0 | -76.69 | 0.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -23.12 | 0.0% | 4.2% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.7 | -73.92 | 0.0% | 11.1% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 14.7 | -0.63 | 0.0% | 26.2% | 4 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 13.3 | 2.01 | 2.6% | 30.6% | 4 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -222.47 | 0.0% | 11.1% | 6 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 6.7 | -51.87 | 0.0% | 0.0% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 8.0 | 36.41 | 4.8% | 0.0% | 3 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 7.7 | -11.41 | 0.0% | 0.0% | 2 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 171.07 | 0.0% | 4.8% | 3 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.0 | -57.51 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 8.0 | -1.72 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 9.3 | 3.15 | 0.0% | 0.0% | 7 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 40.53 | 0.0% | 12.2% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 6.3 | -86.76 | 0.0% | 0.0% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 14.0 | 46.29 | 0.0% | 0.0% | 12 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 9.3 | -6.56 | 0.0% | 0.0% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -89.69 | 0.0% | 12.2% | 2 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 6.0 | -99.25 | 0.0% | 0.0% | 5 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 10.3 | 23.06 | 0.0% | 11.1% | 11 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | -12.28 | 0.0% | 0.0% | 7 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -67.05 | 0.0% | 14.5% | 5 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 6.3 | -32.28 | 0.0% | 0.0% | 4 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 7.7 | 49.23 | 0.0% | 11.1% | 3 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 12.7 | 9.92 | 0.0% | 22.2% | 9 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.7 | -66.11 | 0.0% | 25.5% | 6 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.3 | -34.35 | 0.0% | 0.0% | 4 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 19.3 | -21.23 | 0.0% | 55.6% | 17 |
| fix-path-join-double-slash | gemma4-think | 3 | 100.0% | 0.0 | 14.0 | 11.23 | 0.0% | 42.2% | 16 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 469.38 | 2.8% | 6.7% | 20 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 8.7 | -30.82 | 0.0% | 11.1% | 5 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 7.3 | 19.62 | 0.0% | 0.0% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | -1.39 | 0.0% | 11.1% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | 102.19 | 0.0% | 23.7% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 6.0 | -34.63 | 0.0% | 0.0% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 12.3 | 16.68 | 2.1% | 0.0% | 4 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 11.0 | 1.66 | 0.0% | 0.0% | 8 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | 18.59 | 0.0% | 13.2% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 6.7 | -51.06 | 0.0% | 13.3% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 12.3 | 37.68 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 7.7 | 12.59 | 0.0% | 0.0% | 7 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | -54.17 | 0.0% | 9.7% | 3 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 6.3 | -27.56 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 9.0 | 1.14 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | 0.23 | 0.0% | 0.0% | 7 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | 62.80 | 0.0% | 19.6% | 3 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 6.0 | -43.93 | 0.0% | 0.0% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 7.7 | -71.32 | 0.0% | 0.0% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | 9.59 | 0.0% | 22.2% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -84.74 | 0.0% | 19.8% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.0 | -130.95 | 0.0% | 0.0% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 21.7 | 26.79 | 0.0% | 16.7% | 155 |
| long-change-timeout-everywhere | gemma4-think | 3 | 66.7% | 57.7 | 16.7 | -10.04 | 0.0% | 36.1% | 155 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 16.3 | -4.13 | 0.0% | 36.9% | 156 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 12.3 | -63.57 | 0.0% | 10.4% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -214.08 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 2.0 | -201.24 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -203.38 | 0.0% | 14.3% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 3.0 | -499.49 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -82.47 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 2.3 | -245.86 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 4.0 | -21.17 | 0.0% | 8.3% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 3.3 | -186.48 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 59 |
| gemma4 | invalid_turns | 1 |
| gemma4-think | end_turn | 58 |
| gemma4-think | invalid_turns | 2 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 5 | 611 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | yes | end_turn | 7 | 451 | 1 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 7 | 489 | 1 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 8 | 546 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 7 | 200 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 8 | 291 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1032 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 958 | 0 | 1 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1177 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 5 | 574 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 5 | 513 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 6 | 637 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | yes | end_turn | 5 | 459 | 0 | 1 | 0 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | yes | end_turn | 5 | 309 | 0 | 1 | 0 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 21 | 592 | 0 | 8 | 9 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | no | end_turn | 14 | 296 | 0 | 3 | 8 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | yes | end_turn | 6 | 236 | 0 | 1 | 0 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | no | end_turn | 5 | 188 | 0 | 0 | 16 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 3032 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 13 | 1248 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 808 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 7 | 637 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 6 | 563 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 5 | 914 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 503 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 295 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 288 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 525 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 371 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 517 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 945 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 813 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 562 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 4 | 682 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 4 | 1237 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 5 | 551 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 4 | 593 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 7 | 322 | 1 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 7 | 545 | 1 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 6 | 391 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 8 | 275 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 7 | 180 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1319 | 0 | 3 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 12 | 1403 | 0 | 1 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1557 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 3 | 554 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 4 | 545 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 6 | 944 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 6 | 384 | 1 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 5 | 260 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 5 | 561 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 5 | 484 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 8 | 283 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 5 | 252 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1364 | 0 | 1 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 969 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1508 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 5 | 525 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 5 | 547 | 0 | 2 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 4 | 542 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 10 | 457 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 10 | 788 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | yes | end_turn | 24 | 1574 | 0 | 11 | 3 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/gemma4/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 14 | 460 | 0 | 1 | 4 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 14 | 300 | 1 | 1 | 4 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 12 | 346 | 0 | 1 | 3 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 935 | 0 | 1 | 8 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1708 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 1135 | 0 | 1 | 7 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 7 | 810 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 6 | 884 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 7 | 629 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 10 | 498 | 1 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 7 | 574 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 7 | 862 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 8 | 421 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 8 | 281 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 7 | 241 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 2527 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1399 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 839 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 6 | 618 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 6 | 643 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 6 | 599 | 0 | 0 | 1 | `runs/run-76c783c4c3df5dfd/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 8 | 445 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 8 | 354 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 8 | 363 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 11 | 336 | 0 | 0 | 16 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 8 | 333 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 9 | 240 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 938 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 881 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1408 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 6 | 651 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 6 | 565 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 7 | 691 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 14 | 427 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 16 | 716 | 0 | 0 | 27 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 12 | 634 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 7 | 231 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 13 | 332 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 8 | 414 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 817 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1127 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 2045 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 6 | 527 | 0 | 0 | 5 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 6 | 559 | 0 | 0 | 5 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 6 | 964 | 0 | 0 | 5 | `runs/run-76c783c4c3df5dfd/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 10 | 454 | 0 | 0 | 16 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | invalid_turns | 12 | 550 | 0 | 0 | 16 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/gemma4/seed2/session.jsonl` (poo exited 15) |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 9 | 746 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 8 | 432 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 10 | 309 | 0 | 0 | 16 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 9 | 269 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1385 | 0 | 1 | 5 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1334 | 0 | 1 | 5 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1075 | 0 | 1 | 5 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 7 | 530 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 6 | 626 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 605 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 7 | 707 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 9 | 642 | 0 | 1 | 4 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 7 | 761 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 13 | 254 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 16 | 288 | 0 | 4 | 18 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 9 | 256 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1857 | 0 | 1 | 5 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1425 | 0 | 3 | 2 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 13 | 1033 | 0 | 2 | 11 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 6 | 512 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 7 | 654 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 6 | 457 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | end_turn | 22 | 1052 | 0 | 4 | 25 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | end_turn | 19 | 752 | 0 | 2 | 8 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/gemma4/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 3 | yes | end_turn | 17 | 664 | 0 | 2 | 19 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/gemma4/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 1 | yes | end_turn | 12 | 361 | 0 | 1 | 25 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | yes | end_turn | 18 | 325 | 0 | 3 | 14 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 3 | yes | invalid_turns | 12 | 269 | 0 | 1 | 10 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` (poo exited 15) |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 4302 | 0 | 0 | 14 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 2679 | 0 | 0 | 22 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1064 | 1 | 2 | 24 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 7 | 500 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 7 | 798 | 0 | 0 | 6 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 12 | 718 | 0 | 1 | 6 | `runs/run-76c783c4c3df5dfd/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 8 | 575 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 7 | 456 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 7 | 584 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 9 | 318 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 10 | 323 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 8 | 212 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 989 | 0 | 4 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1601 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 2246 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 6 | 516 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 6 | 512 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 6 | 459 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 7 | 564 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 18 | 426 | 1 | 0 | 5 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 12 | 575 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 11 | 389 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 10 | 307 | 0 | 0 | 17 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 12 | 230 | 0 | 0 | 5 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1908 | 0 | 1 | 3 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 2019 | 0 | 1 | 3 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 2000 | 0 | 1 | 3 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 7 | 485 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 7 | 626 | 0 | 2 | 3 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 6 | 594 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 13 | 389 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 11 | 547 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 13 | 457 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 9 | 391 | 0 | 0 | 16 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 7 | 278 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 7 | 171 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 851 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 861 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 830 | 0 | 1 | 4 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 7 | 515 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 7 | 502 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 5 | 466 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 11 | 373 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 8 | 457 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 8 | 348 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 9 | 236 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 10 | 314 | 0 | 0 | 16 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 8 | 192 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 887 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 891 | 0 | 4 | 2 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1460 | 0 | 1 | 2 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 6 | 503 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 6 | 494 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 6 | 485 | 0 | 0 | 2 | `runs/run-76c783c4c3df5dfd/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 8 | 2629 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 7 | 2475 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 8 | 680 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | invalid_turns | 11 | 240 | 0 | 1 | 3 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` (poo exited 15) |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 9 | 240 | 0 | 1 | 3 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 7 | 352 | 0 | 0 | 3 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1184 | 0 | 1 | 4 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1311 | 0 | 2 | 4 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 882 | 0 | 1 | 4 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 6 | 727 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 6 | 1080 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 6 | 806 | 0 | 0 | 4 | `runs/run-76c783c4c3df5dfd/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 16 | 1208 | 0 | 0 | 156 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 28 | 976 | 0 | 1 | 156 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 21 | 799 | 0 | 0 | 152 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 22 | 479 | 0 | 1 | 156 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 12 | 434 | 0 | 0 | 152 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | no | end_turn | 16 | 372 | 0 | 3 | 156 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 13 | 1133 | 0 | 4 | 156 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 20 | 1187 | 0 | 10 | 156 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 16 | 759 | 0 | 15 | 156 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 16 | 1128 | 0 | 5 | 156 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 9 | 737 | 0 | 0 | 156 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 12 | 888 | 0 | 0 | 156 | `runs/run-76c783c4c3df5dfd/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 3 | 513 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 3 | 215 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 3 | 532 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 2 | 183 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 2 | 484 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 2 | 156 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1697 | 0 | 2 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1884 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 991 | 0 | 1 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 3 | 2041 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 3 | 955 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 3 | 722 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 3 | 435 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 3 | 284 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 3 | 317 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 2 | 658 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 3 | 208 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 2 | 211 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 1361 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 4 | 1272 | 0 | 1 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 4 | 816 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 3 | 776 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 3 | 865 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 4 | 650 | 0 | 0 | 0 | `runs/run-76c783c4c3df5dfd/long-find-error-in-log/qwen/seed3/session.jsonl` |
