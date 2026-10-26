# poo eval report

- started: 2026-10-22T16:31:47.450969365Z
- finished: 2026-10-22T16:55:59.449717142Z
- git sha: b88fcb1
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 96.7% | 2.9 | 13.9 | 37106 | 639 | 14544 | 672 | -23.09 | 6.1% | 0.0% | 32.3% | 10 | 85.5% |
| gemma4-think | 60 | 96.7% | 2.9 | 10.1 | 23527 | 985 | 17374 | 330 | -2.31 | 0.7% | 0.0% | 23.9% | 10 | 82.0% |
| minimax-m3.1-flash | 60 | 98.3% | 2.9 | 8.4 | 27608 | 2664 | 25901 | 1149 | 50.02 | 6.8% | 0.0% | 18.2% | 12 | 83.9% |
| qwen | 60 | 100.0% | 0.0 | 5.4 | 13421 | 643 | 9556 | 449 | 26.87 | 0.0% | 0.0% | 14.5% | 10 | n/a |
| **all** | 240 | 97.9% | 1.4 | 9.4 | 25416 | 1233 | 16844 | 650 | 12.87 | 3.4% | 0.0% | 22.2% | 10 | 83.8% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 100.0% | 0.0 | 6.0 | -53.53 | 20.0% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 5.0 | 1.30 | 0.0% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 39.80 | 0.0% | 0.0% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 5.3 | -12.29 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 33.3% | 57.7 | 5.7 | 20.95 | 0.0% | 16.7% | 9 |
| find-off-by-one-loop | gemma4-think | 3 | 33.3% | 57.7 | 2.7 | -7.48 | 6.7% | 0.0% | 0 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 66.7% | 57.7 | 9.3 | -9.42 | 0.0% | 4.8% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 5.3 | 62.09 | 0.0% | 8.3% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -52.56 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -8.17 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | 73.41 | 0.0% | 10.3% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 4.3 | 68.49 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 8.3 | -10.96 | 13.7% | 11.1% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 5.0 | -9.11 | 0.0% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.7 | -193.71 | 0.0% | 18.1% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 4.3 | -5.34 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 6.3 | -30.05 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 7.7 | -25.11 | 0.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.7 | 97.84 | 0.0% | 8.9% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 3.7 | -63.74 | 0.0% | 0.0% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 50.0 | -2.66 | 2.0% | 95.7% | 6 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 11.3 | -3.10 | 0.0% | 23.3% | 8 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 11.0 | 27.10 | 13.3% | 31.1% | 9 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 6.3 | 50.13 | 0.0% | 0.0% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 14.7 | -8.34 | 0.0% | 60.0% | 2 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 17.3 | 10.33 | 6.3% | 63.9% | 4 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 11.3 | 15.58 | 9.1% | 27.6% | 9 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 5.3 | 12.24 | 0.0% | 13.3% | 2 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 11.0 | -5.26 | 0.0% | 33.3% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 11.3 | -1.19 | 0.0% | 50.0% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 19.41 | 7.5% | 8.9% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 6.0 | 40.83 | 0.0% | 35.6% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 20.3 | 3.43 | 10.7% | 38.3% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 11.7 | 7.92 | 0.0% | 22.2% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -45.97 | 11.2% | 20.8% | 3 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 6.0 | 13.42 | 0.0% | 47.8% | 3 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -11.54 | 0.0% | 30.6% | 2 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | -8.38 | 0.0% | 11.1% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | 85.43 | 12.0% | 9.5% | 3 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 5.3 | 41.52 | 0.0% | 33.3% | 3 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 23.7 | 2.02 | 0.0% | 78.1% | 2 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | 33.35 | 0.0% | 33.3% | 2 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.3 | 836.39 | 10.3% | 38.1% | 11 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.3 | 47.45 | 0.0% | 53.3% | 4 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 15.3 | 43.33 | 12.0% | 40.7% | 12 |
| fix-path-join-double-slash | gemma4-think | 3 | 100.0% | 0.0 | 18.3 | 11.53 | 2.0% | 57.6% | 5 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | 19.71 | 8.6% | 22.9% | 19 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 5.3 | 32.31 | 0.0% | 0.0% | 9 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 14.0 | -52.18 | 16.7% | 33.3% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | -13.06 | 0.0% | 33.3% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | -29.11 | 10.9% | 20.8% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 4.7 | 32.68 | 0.0% | 0.0% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 18.3 | 14.80 | 13.4% | 62.5% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 16.7 | 1.34 | 0.0% | 44.4% | 3 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 57.82 | 11.7% | 18.6% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 6.0 | -20.28 | 0.0% | 41.1% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 13.7 | -2.49 | 8.2% | 27.8% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 8.7 | 0.36 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 102.11 | 11.7% | 12.2% | 2 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 4.7 | 108.71 | 0.0% | 5.6% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 13.0 | -13.69 | 0.0% | 50.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 12.0 | 6.18 | 0.0% | 50.0% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.3 | -2.39 | 14.1% | 26.7% | 2 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 4.7 | 198.52 | 0.0% | 0.0% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 18.3 | 11.62 | 6.2% | 57.1% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 16.3 | 4.31 | 0.0% | 60.2% | 4 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 11.65 | 12.0% | 8.9% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 5.0 | 18.29 | 0.0% | 27.8% | 3 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 17.7 | 14.29 | 1.8% | 0.0% | 152 |
| long-change-timeout-everywhere | gemma4-think | 3 | 100.0% | 0.0 | 16.7 | -52.56 | 0.0% | 27.8% | 152 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 16.3 | 1.89 | 4.1% | 37.1% | 172 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 12.7 | 9.47 | 0.0% | 24.3% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 5.0 | -28.49 | 16.7% | 11.1% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -28.25 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 42.62 | 0.0% | 29.7% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 4.0 | 86.31 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -300.42 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | 33.65 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 3.7 | -149.84 | 0.0% | 9.5% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 2.0 | -183.48 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 57 |
| gemma4 | max_turns | 3 |
| gemma4-think | end_turn | 58 |
| gemma4-think | provider_error | 2 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 6 | 406 | 1 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | yes | end_turn | 6 | 501 | 1 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 6 | 465 | 1 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 5 | 345 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 5 | 259 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 5 | 239 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 1048 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1438 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1306 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 5 | 372 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 5 | 395 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 6 | 441 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | yes | end_turn | 5 | 513 | 0 | 1 | 0 | `runs/run-d7426a3454527828/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 6 | 497 | 0 | 0 | 14 | `runs/run-d7426a3454527828/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 6 | 550 | 0 | 0 | 14 | `runs/run-d7426a3454527828/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | no | provider_error | 1 | 509 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` (poo exited 20) |
| find-off-by-one-loop | gemma4-think | 2 | no | provider_error | 1 | 735 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` (poo exited 20) |
| find-off-by-one-loop | gemma4-think | 3 | yes | end_turn | 6 | 282 | 1 | 0 | 0 | `runs/run-d7426a3454527828/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 819 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | no | end_turn | 14 | 897 | 0 | 1 | 0 | `runs/run-d7426a3454527828/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 907 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 5 | 424 | 0 | 1 | 0 | `runs/run-d7426a3454527828/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 6 | 512 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 5 | 350 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 486 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 339 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 245 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 252 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 157 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 156 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 830 | 0 | 1 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 849 | 0 | 1 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1001 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 3 | 316 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 6 | 499 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 4 | 361 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 8 | 455 | 1 | 0 | 0 | `runs/run-d7426a3454527828/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 9 | 337 | 1 | 1 | 0 | `runs/run-d7426a3454527828/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 8 | 512 | 1 | 0 | 0 | `runs/run-d7426a3454527828/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 5 | 254 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 5 | 198 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 5 | 330 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 965 | 0 | 2 | 0 | `runs/run-d7426a3454527828/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 2112 | 0 | 2 | 0 | `runs/run-d7426a3454527828/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 1015 | 0 | 1 | 0 | `runs/run-d7426a3454527828/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 5 | 527 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 5 | 503 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 3 | 372 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 5 | 333 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 9 | 322 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 5 | 236 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 5 | 410 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 10 | 275 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 8 | 244 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 2623 | 0 | 1 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 914 | 0 | 1 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 885 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 3 | 336 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 4 | 588 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 4 | 344 | 0 | 0 | 0 | `runs/run-d7426a3454527828/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | max_turns | 50 | 531 | 1 | 44 | 6 | `runs/run-d7426a3454527828/fix-cache-never-expires/gemma4/seed1/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4 | 2 | yes | max_turns | 50 | 528 | 1 | 44 | 6 | `runs/run-d7426a3454527828/fix-cache-never-expires/gemma4/seed2/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4 | 3 | yes | max_turns | 50 | 347 | 1 | 44 | 6 | `runs/run-d7426a3454527828/fix-cache-never-expires/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 11 | 365 | 0 | 1 | 3 | `runs/run-d7426a3454527828/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 10 | 270 | 0 | 1 | 4 | `runs/run-d7426a3454527828/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 13 | 293 | 0 | 1 | 18 | `runs/run-d7426a3454527828/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 13 | 1122 | 2 | 3 | 8 | `runs/run-d7426a3454527828/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 998 | 2 | 3 | 11 | `runs/run-d7426a3454527828/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 811 | 1 | 4 | 7 | `runs/run-d7426a3454527828/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 6 | 584 | 0 | 0 | 3 | `runs/run-d7426a3454527828/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 6 | 418 | 0 | 0 | 3 | `runs/run-d7426a3454527828/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 7 | 599 | 0 | 0 | 3 | `runs/run-d7426a3454527828/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 14 | 455 | 0 | 3 | 2 | `runs/run-d7426a3454527828/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 15 | 514 | 0 | 3 | 2 | `runs/run-d7426a3454527828/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 15 | 495 | 0 | 3 | 2 | `runs/run-d7426a3454527828/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 17 | 309 | 1 | 5 | 4 | `runs/run-d7426a3454527828/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 17 | 292 | 1 | 6 | 4 | `runs/run-d7426a3454527828/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 18 | 273 | 1 | 5 | 4 | `runs/run-d7426a3454527828/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 739 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 20 | 934 | 1 | 6 | 24 | `runs/run-d7426a3454527828/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1141 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 5 | 347 | 0 | 0 | 1 | `runs/run-d7426a3454527828/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 6 | 482 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 5 | 403 | 0 | 0 | 4 | `runs/run-d7426a3454527828/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 10 | 613 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 11 | 547 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 12 | 552 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 11 | 364 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 12 | 221 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 11 | 175 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 951 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1015 | 1 | 0 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 878 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 8 | 371 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 5 | 450 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 5 | 338 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 16 | 352 | 2 | 1 | 2 | `runs/run-d7426a3454527828/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 19 | 549 | 3 | 2 | 2 | `runs/run-d7426a3454527828/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 26 | 586 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 15 | 331 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 10 | 266 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 10 | 243 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 891 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 957 | 1 | 2 | 2 | `runs/run-d7426a3454527828/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1156 | 1 | 1 | 4 | `runs/run-d7426a3454527828/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 4 | 335 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 9 | 456 | 0 | 6 | 2 | `runs/run-d7426a3454527828/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 5 | 458 | 0 | 2 | 5 | `runs/run-d7426a3454527828/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 10 | 370 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 10 | 296 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 10 | 546 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 9 | 427 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 9 | 327 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 13 | 292 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1027 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 930 | 2 | 1 | 2 | `runs/run-d7426a3454527828/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1206 | 1 | 0 | 4 | `runs/run-d7426a3454527828/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 5 | 356 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 5 | 378 | 0 | 1 | 4 | `runs/run-d7426a3454527828/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 378 | 0 | 1 | 4 | `runs/run-d7426a3454527828/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 19 | 497 | 0 | 3 | 2 | `runs/run-d7426a3454527828/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 26 | 481 | 0 | 14 | 2 | `runs/run-d7426a3454527828/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 26 | 509 | 0 | 13 | 2 | `runs/run-d7426a3454527828/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 10 | 342 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 10 | 254 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 11 | 473 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 16 | 1151 | 1 | 6 | 20 | `runs/run-d7426a3454527828/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 2900 | 1 | 2 | 8 | `runs/run-d7426a3454527828/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 4958 | 1 | 3 | 4 | `runs/run-d7426a3454527828/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 8 | 432 | 0 | 2 | 4 | `runs/run-d7426a3454527828/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 5 | 362 | 0 | 2 | 4 | `runs/run-d7426a3454527828/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 6 | 379 | 0 | 3 | 4 | `runs/run-d7426a3454527828/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | end_turn | 18 | 1621 | 2 | 4 | 13 | `runs/run-d7426a3454527828/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | end_turn | 13 | 843 | 1 | 1 | 12 | `runs/run-d7426a3454527828/fix-path-join-double-slash/gemma4/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 3 | yes | end_turn | 15 | 2308 | 2 | 2 | 11 | `runs/run-d7426a3454527828/fix-path-join-double-slash/gemma4/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 1 | yes | end_turn | 19 | 314 | 1 | 4 | 2 | `runs/run-d7426a3454527828/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | yes | end_turn | 22 | 453 | 0 | 5 | 12 | `runs/run-d7426a3454527828/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 3 | yes | end_turn | 14 | 319 | 0 | 3 | 2 | `runs/run-d7426a3454527828/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1076 | 1 | 1 | 27 | `runs/run-d7426a3454527828/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1058 | 1 | 2 | 11 | `runs/run-d7426a3454527828/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 945 | 1 | 4 | 20 | `runs/run-d7426a3454527828/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 6 | 356 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 5 | 334 | 0 | 0 | 11 | `runs/run-d7426a3454527828/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 5 | 345 | 0 | 0 | 13 | `runs/run-d7426a3454527828/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 14 | 1452 | 2 | 1 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 14 | 1046 | 2 | 1 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 14 | 901 | 2 | 1 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 9 | 410 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 9 | 384 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 9 | 249 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 887 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 915 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 938 | 1 | 2 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 4 | 358 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 5 | 348 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 5 | 347 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 18 | 1651 | 2 | 5 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 18 | 1629 | 2 | 5 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 19 | 1286 | 3 | 5 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 15 | 360 | 0 | 2 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 19 | 284 | 0 | 6 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 16 | 254 | 0 | 2 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 938 | 1 | 1 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1042 | 1 | 2 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 861 | 1 | 1 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 5 | 371 | 0 | 1 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 7 | 383 | 0 | 3 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 6 | 767 | 0 | 2 | 3 | `runs/run-d7426a3454527828/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 13 | 365 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 16 | 398 | 2 | 1 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 12 | 482 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 8 | 319 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 10 | 236 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 8 | 214 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 898 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 3182 | 1 | 0 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 915 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 5 | 436 | 0 | 1 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 4 | 693 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 5 | 506 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 12 | 414 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 14 | 535 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 13 | 470 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 13 | 256 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 13 | 211 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 10 | 162 | 0 | 2 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 884 | 1 | 1 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 977 | 2 | 1 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 1019 | 1 | 2 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 5 | 371 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 5 | 762 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 4 | 991 | 0 | 0 | 2 | `runs/run-d7426a3454527828/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 18 | 658 | 1 | 4 | 3 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 18 | 639 | 1 | 4 | 3 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 19 | 584 | 1 | 4 | 3 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 17 | 317 | 0 | 5 | 3 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 15 | 328 | 0 | 5 | 6 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 17 | 286 | 0 | 5 | 3 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 852 | 1 | 1 | 4 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 787 | 1 | 1 | 4 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 973 | 1 | 0 | 4 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 5 | 400 | 0 | 2 | 3 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 6 | 390 | 0 | 1 | 3 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 4 | 415 | 0 | 0 | 4 | `runs/run-d7426a3454527828/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 20 | 2249 | 1 | 0 | 152 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 17 | 1526 | 0 | 0 | 152 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 16 | 1449 | 0 | 0 | 152 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 13 | 379 | 0 | 1 | 152 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 14 | 834 | 0 | 1 | 152 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | yes | end_turn | 23 | 569 | 0 | 1 | 152 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 17 | 1121 | 1 | 10 | 156 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 15 | 1189 | 1 | 8 | 156 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 17 | 985 | 1 | 13 | 203 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 15 | 666 | 0 | 1 | 156 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 12 | 610 | 0 | 4 | 156 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 11 | 511 | 0 | 2 | 156 | `runs/run-d7426a3454527828/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 5 | 411 | 1 | 0 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 7 | 545 | 1 | 1 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 3 | 208 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 4 | 611 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 4 | 614 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 4 | 237 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 850 | 0 | 3 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 804 | 0 | 5 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 4 | 1012 | 0 | 1 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 5 | 562 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 4 | 556 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 3 | 375 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 3 | 846 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 3 | 370 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 3 | 445 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 3 | 502 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 3 | 363 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 3 | 198 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 1028 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 3 | 737 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 4 | 716 | 0 | 2 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 2 | 588 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 2 | 318 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 2 | 318 | 0 | 0 | 0 | `runs/run-d7426a3454527828/long-find-error-in-log/qwen/seed3/session.jsonl` |
