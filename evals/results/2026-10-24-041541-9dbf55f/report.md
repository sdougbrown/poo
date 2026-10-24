# poo eval report

- started: 2026-10-24T04:15:41.500750637Z
- finished: 2026-10-24T04:35:43.43939636Z
- git sha: 9dbf55f
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 93.3% | 2.9 | 11.2 | 35790 | 770 | 11909 | 542 | -8.57 | 7.3% | 0.0% | 10.9% | 10 | 91.5% |
| gemma4-think | 60 | 95.0% | 0.0 | 12.3 | 46833 | 812 | 13164 | 576 | -12.70 | 7.8% | 0.0% | 13.5% | 11 | 92.8% |
| minimax-m3.1-flash | 60 | 100.0% | 0.0 | 7.4 | 29303 | 2316 | 22724 | 1006 | -18.45 | 0.5% | 0.0% | 17.9% | 11 | 88.3% |
| qwen | 60 | 100.0% | 0.0 | 6.0 | 18658 | 393 | 6419 | 574 | 3.21 | 0.1% | 0.0% | 5.1% | 10 | n/a |
| **all** | 240 | 97.1% | 0.7 | 9.2 | 32646 | 1073 | 13554 | 675 | -9.13 | 3.9% | 0.0% | 11.8% | 10 | 90.8% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 100.0% | 0.0 | 8.0 | 3.79 | 28.6% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 8.0 | -10.22 | 28.6% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -8.87 | 0.0% | 14.3% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 5.7 | -42.37 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 19.3 | -0.82 | 0.0% | 69.8% | 12 |
| find-off-by-one-loop | gemma4-think | 3 | 0.0% | 0.0 | 18.7 | 6.09 | 0.0% | 68.2% | 13 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -12.00 | 0.0% | 16.7% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 5.0 | 0.39 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -37.09 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -29.57 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.0 | -15.53 | 0.0% | 0.0% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 4.3 | 8.93 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 66.7% | 57.7 | 5.0 | 7.73 | 11.1% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 7.0 | -8.14 | 16.7% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.0 | -16.33 | 0.0% | 11.1% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 4.7 | -39.39 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 7.7 | -4.40 | 4.2% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 5.3 | -16.51 | 0.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.7 | -24.23 | 0.0% | 9.5% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.7 | -63.22 | 0.0% | 6.7% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 19.3 | 36.89 | 8.3% | 27.8% | 3 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 30.0 | 11.46 | 9.7% | 19.4% | 3 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | -3.87 | 0.0% | 17.0% | 6 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 7.0 | 17.89 | 0.0% | 5.6% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 9.7 | -2.59 | 12.5% | 0.0% | 4 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 9.3 | 12.22 | 13.7% | 0.0% | 4 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 17.84 | 0.0% | 15.0% | 3 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.0 | 17.13 | 0.0% | 0.0% | 4 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 8.7 | -10.58 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 8.3 | 1.64 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | 44.81 | 0.0% | 10.3% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 6.0 | -24.70 | 0.0% | 16.7% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 9.7 | 5.36 | 14.3% | 0.0% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -21.16 | 14.3% | 0.0% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.7 | -128.27 | 0.0% | 12.2% | 3 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 6.7 | 5.39 | 0.0% | 8.3% | 3 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -16.39 | 13.1% | 0.0% | 2 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | -16.32 | 12.5% | 0.0% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -35.10 | 0.0% | 14.5% | 3 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 6.0 | 23.15 | 0.0% | 0.0% | 3 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 15.3 | -12.32 | 15.8% | 50.0% | 4 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 15.3 | -15.02 | 15.4% | 50.0% | 4 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.0 | 32.52 | 3.9% | 30.3% | 7 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.3 | 17.89 | 0.0% | 13.3% | 3 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 32.7 | 4.45 | 0.0% | 47.8% | 10 |
| fix-path-join-double-slash | gemma4-think | 3 | 100.0% | 0.0 | 38.3 | -10.25 | 0.0% | 80.0% | 19 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | 1.17 | 5.3% | 16.2% | 25 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 8.0 | -6.22 | 0.0% | 0.0% | 5 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 10.3 | -15.20 | 12.5% | 0.0% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -22.63 | 12.5% | 0.0% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 32.46 | 0.0% | 34.2% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 6.7 | 38.17 | 0.0% | 16.7% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -8.07 | 13.1% | 0.0% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -14.04 | 13.7% | 0.0% | 3 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -17.14 | 0.0% | 6.7% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 6.0 | -4.78 | 0.0% | 0.0% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 8.7 | -7.78 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | -16.38 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.3 | 100.96 | 0.0% | 5.6% | 2 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 6.0 | 9.46 | 0.0% | 16.7% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 9.0 | -22.35 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 8.0 | -6.33 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 5.63 | 0.0% | 18.9% | 3 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 6.0 | 61.07 | 0.0% | 0.0% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 11.7 | -3.52 | 12.1% | 22.2% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 14.0 | -26.94 | 18.2% | 33.3% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -257.40 | 0.0% | 21.7% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.7 | 11.13 | 0.0% | 8.3% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 17.0 | -10.67 | 0.0% | 0.0% | 153 |
| long-change-timeout-everywhere | gemma4-think | 3 | 100.0% | 0.0 | 23.3 | 17.17 | 1.1% | 19.4% | 156 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 14.7 | -28.58 | 0.0% | 30.6% | 158 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 11.0 | 7.62 | 2.2% | 9.5% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -35.56 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | -51.97 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.3 | -29.94 | 0.0% | 34.4% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 3.7 | 42.37 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -42.27 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 3.7 | -37.02 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.7 | -27.12 | 0.0% | 38.9% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 3.3 | -15.76 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 57 |
| gemma4 | max_turns | 1 |
| gemma4 | provider_error | 2 |
| gemma4-think | end_turn | 59 |
| gemma4-think | max_turns | 1 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 8 | 438 | 2 | 0 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | yes | end_turn | 8 | 446 | 2 | 0 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 8 | 447 | 2 | 0 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 8 | 361 | 2 | 0 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 8 | 433 | 2 | 0 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 8 | 286 | 2 | 0 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1235 | 0 | 1 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 822 | 0 | 1 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1137 | 0 | 1 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 6 | 655 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 5 | 668 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 6 | 505 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 18 | 551 | 0 | 4 | 12 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 20 | 520 | 0 | 5 | 12 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 20 | 633 | 0 | 5 | 12 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | no | end_turn | 18 | 667 | 0 | 4 | 14 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | no | end_turn | 18 | 672 | 0 | 4 | 13 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | no | end_turn | 20 | 632 | 0 | 5 | 12 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 968 | 0 | 9 | 0 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1184 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 999 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 6 | 691 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 3 | 440 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 6 | 499 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 268 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 234 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 241 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 308 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 234 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 236 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 770 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 902 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 810 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 3 | 414 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 6 | 433 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 4 | 452 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 7 | 366 | 1 | 0 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | no | provider_error | 1 | 0 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/gemma4/seed2/session.jsonl` (poo exited 20) |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 7 | 292 | 1 | 0 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 7 | 308 | 1 | 0 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 7 | 273 | 1 | 0 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 7 | 246 | 1 | 0 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 12 | 1062 | 0 | 2 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 839 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 11 | 912 | 0 | 1 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 4 | 631 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 5 | 567 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 5 | 420 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 9 | 327 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 5 | 279 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 9 | 291 | 1 | 0 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 5 | 255 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 5 | 257 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 6 | 308 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 1002 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 872 | 0 | 1 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 952 | 0 | 1 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 6 | 437 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 4 | 1500 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 4 | 1562 | 0 | 1 | 0 | `runs/run-b16021a6f713abeb/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 25 | 605 | 1 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 11 | 821 | 1 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | yes | provider_error | 22 | 787 | 2 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/gemma4/seed3/session.jsonl` (poo exited 20) |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 36 | 679 | 2 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 12 | 948 | 2 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 42 | 575 | 2 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1073 | 0 | 1 | 7 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1020 | 0 | 1 | 6 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 858 | 0 | 1 | 4 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 8 | 556 | 0 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 6 | 533 | 0 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 7 | 556 | 0 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 10 | 940 | 1 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 10 | 776 | 1 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 9 | 680 | 1 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 9 | 721 | 1 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 9 | 883 | 1 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 10 | 781 | 1 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1054 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1103 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1000 | 0 | 0 | 6 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 6 | 579 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 6 | 544 | 0 | 0 | 5 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 6 | 524 | 0 | 0 | 5 | `runs/run-b16021a6f713abeb/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 9 | 500 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 9 | 330 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 8 | 313 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 9 | 431 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 8 | 439 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 8 | 572 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 993 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1025 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 964 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 6 | 648 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 6 | 509 | 0 | 2 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 6 | 523 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 10 | 576 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 9 | 593 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 10 | 750 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 10 | 711 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 10 | 628 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 10 | 660 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 968 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1568 | 0 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1194 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 5 | 454 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 7 | 457 | 0 | 0 | 5 | `runs/run-b16021a6f713abeb/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 8 | 522 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 10 | 490 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 10 | 506 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 10 | 562 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 10 | 586 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 10 | 591 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 11 | 437 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1213 | 0 | 1 | 4 | `runs/run-b16021a6f713abeb/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 977 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 955 | 0 | 1 | 4 | `runs/run-b16021a6f713abeb/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 6 | 485 | 0 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 6 | 485 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 485 | 0 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 15 | 559 | 2 | 2 | 4 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 16 | 610 | 2 | 2 | 4 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 15 | 569 | 2 | 2 | 4 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 15 | 474 | 2 | 2 | 4 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 15 | 634 | 2 | 2 | 4 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 16 | 521 | 2 | 2 | 4 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 1075 | 2 | 2 | 13 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1108 | 0 | 3 | 4 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 962 | 0 | 2 | 4 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 6 | 529 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 6 | 554 | 0 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 7 | 545 | 0 | 2 | 2 | `runs/run-b16021a6f713abeb/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | end_turn | 24 | 1206 | 0 | 3 | 11 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | max_turns | 50 | 3139 | 0 | 2 | 10 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/gemma4/seed2/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4 | 3 | yes | end_turn | 24 | 685 | 0 | 1 | 10 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/gemma4/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 1 | yes | max_turns | 50 | 2691 | 0 | 8 | 18 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4-think | 2 | yes | end_turn | 41 | 1107 | 0 | 28 | 31 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 3 | yes | end_turn | 24 | 854 | 0 | 4 | 9 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1039 | 1 | 2 | 29 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 749 | 0 | 1 | 22 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 914 | 1 | 1 | 25 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 6 | 515 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 12 | 950 | 0 | 0 | 12 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 6 | 517 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 11 | 468 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 10 | 428 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 10 | 427 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 10 | 419 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 10 | 429 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 10 | 558 | 1 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 989 | 0 | 3 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1003 | 0 | 2 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 938 | 0 | 4 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 6 | 469 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 7 | 488 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 7 | 587 | 0 | 3 | 2 | `runs/run-b16021a6f713abeb/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 10 | 502 | 1 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 10 | 466 | 1 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 10 | 615 | 1 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 10 | 632 | 1 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 10 | 550 | 1 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 10 | 451 | 1 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 971 | 0 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 912 | 0 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1124 | 0 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 6 | 514 | 0 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 6 | 592 | 0 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 6 | 458 | 0 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 8 | 360 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 9 | 300 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 9 | 312 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 9 | 430 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 9 | 448 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 9 | 413 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1120 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 896 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 997 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 6 | 510 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 6 | 517 | 0 | 2 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 6 | 517 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 9 | 507 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 9 | 453 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 9 | 389 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 8 | 420 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 8 | 473 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 8 | 431 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1060 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 793 | 0 | 1 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 909 | 0 | 2 | 4 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 6 | 693 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 6 | 526 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 6 | 525 | 0 | 0 | 2 | `runs/run-b16021a6f713abeb/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 14 | 705 | 2 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 8 | 714 | 0 | 0 | 3 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 13 | 924 | 2 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 14 | 876 | 2 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 14 | 887 | 2 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 14 | 870 | 2 | 1 | 3 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1768 | 0 | 1 | 4 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 996 | 0 | 1 | 4 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 841 | 0 | 1 | 4 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 6 | 523 | 0 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 6 | 586 | 0 | 0 | 4 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 8 | 595 | 0 | 1 | 4 | `runs/run-b16021a6f713abeb/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 18 | 667 | 0 | 0 | 156 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 17 | 626 | 0 | 0 | 152 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 16 | 613 | 0 | 0 | 152 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 34 | 1039 | 1 | 1 | 156 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 18 | 893 | 0 | 0 | 156 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | yes | end_turn | 18 | 934 | 0 | 1 | 156 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 17 | 1105 | 0 | 4 | 156 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 15 | 1035 | 0 | 13 | 161 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 12 | 939 | 0 | 3 | 156 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 9 | 685 | 0 | 0 | 156 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 13 | 702 | 1 | 1 | 156 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 11 | 697 | 0 | 1 | 156 | `runs/run-b16021a6f713abeb/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 3 | 379 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 3 | 260 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 3 | 262 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 3 | 262 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 3 | 351 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 3 | 420 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 866 | 0 | 2 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1028 | 0 | 1 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 13 | 1014 | 0 | 6 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 3 | 488 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 3 | 578 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 5 | 610 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 4 | 302 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 4 | 269 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 4 | 267 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 3 | 383 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 4 | 301 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 4 | 268 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1195 | 0 | 2 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 844 | 0 | 1 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 4 | 755 | 0 | 1 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 4 | 416 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 4 | 415 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 2 | 393 | 0 | 0 | 0 | `runs/run-b16021a6f713abeb/long-find-error-in-log/qwen/seed3/session.jsonl` |
