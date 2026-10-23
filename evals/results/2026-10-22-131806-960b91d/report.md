# poo eval report

- started: 2026-10-22T13:18:06.058877278Z
- finished: 2026-10-22T13:37:42.118377302Z
- git sha: 960b91d
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 95.0% | 0.0 | 15.3 | 46856 | 664 | 12549 | 522 | -11.36 | 5.7% | 0.0% | 30.7% | 10 | 86.5% |
| gemma4-think | 60 | 95.0% | 0.0 | 10.0 | 22821 | 949 | 13885 | 296 | -4.77 | 2.1% | 0.0% | 22.6% | 9 | 83.7% |
| minimax-m3.1-flash | 60 | 100.0% | 0.0 | 8.2 | 35729 | 2152 | 22273 | 1115 | 4.14 | 7.0% | 0.0% | 17.8% | 11 | 82.6% |
| qwen | 60 | 100.0% | 0.0 | 5.2 | 11452 | 604 | 6637 | 388 | 31.04 | 0.0% | 0.0% | 11.8% | 9 | n/a |
| **all** | 240 | 97.5% | 0.0 | 9.7 | 29215 | 1092 | 13836 | 580 | 4.76 | 3.7% | 0.0% | 20.7% | 10 | 84.3% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 100.0% | 0.0 | 6.0 | -12.79 | 20.0% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 5.0 | -43.96 | 0.0% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | 9.15 | 0.0% | 9.7% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 4.0 | 26.70 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 7.0 | 81.33 | 0.0% | 13.3% | 13 |
| find-off-by-one-loop | gemma4-think | 3 | 33.3% | 57.7 | 3.0 | 0.10 | 6.7% | 0.0% | 0 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.7 | -12.35 | 0.0% | 4.8% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 4.7 | 26.88 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -52.61 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -14.58 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 21.12 | 0.0% | 13.0% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 3.7 | -19.73 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 8.0 | 4.04 | 14.5% | 11.1% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 4.7 | 12.78 | 0.0% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 11.0 | 39.57 | 0.0% | 12.4% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 4.7 | 46.19 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 5.0 | -31.58 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 66.7% | 57.7 | 5.0 | -29.73 | 30.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.0 | -11.91 | 0.0% | 12.6% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.0 | 34.16 | 0.0% | 0.0% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 50.0 | -2.67 | 2.0% | 95.7% | 6 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 14.3 | 7.50 | 0.0% | 27.4% | 8 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 11.0 | 1.81 | 13.9% | 18.0% | 5 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 5.3 | 45.39 | 0.0% | 12.2% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 15.0 | -9.65 | 0.0% | 60.0% | 2 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 17.0 | -0.52 | 6.4% | 67.8% | 3 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 40.69 | 12.3% | 13.4% | 3 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.0 | 28.96 | 0.0% | 8.3% | 2 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 11.0 | -15.07 | 0.0% | 33.3% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 11.3 | 10.81 | 0.0% | 50.0% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 16.58 | 10.8% | 9.3% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 5.0 | 28.55 | 0.0% | 31.1% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 17.3 | 3.44 | 14.9% | 35.0% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 10.7 | 2.04 | 0.0% | 35.6% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -19.73 | 11.7% | 13.9% | 3 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 5.3 | 28.03 | 0.0% | 16.7% | 2 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 10.3 | -26.22 | 0.0% | 30.6% | 2 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 9.3 | -0.18 | 0.0% | 11.1% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | 30.63 | 12.2% | 9.7% | 4 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 7.0 | 38.76 | 0.0% | 11.1% | 3 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 25.3 | -3.02 | 5.6% | 42.2% | 2 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | 14.73 | 0.0% | 36.1% | 2 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 15.70 | 12.0% | 15.1% | 3 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.0 | 24.56 | 0.0% | 27.8% | 2 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 26.7 | -11.40 | 5.8% | 48.2% | 13 |
| fix-path-join-double-slash | gemma4-think | 3 | 100.0% | 0.0 | 17.0 | 18.74 | 0.0% | 33.3% | 4 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.0 | 37.93 | 8.9% | 21.9% | 23 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 6.3 | 25.45 | 0.0% | 0.0% | 5 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 15.3 | 11.82 | 19.4% | 38.9% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | 3.68 | 0.0% | 22.2% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.3 | -103.81 | 12.6% | 30.3% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 5.3 | 28.63 | 0.0% | 24.4% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 19.3 | -0.25 | 11.8% | 62.5% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 17.3 | 1.95 | 0.0% | 43.5% | 3 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -41.78 | 11.2% | 20.5% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 5.3 | 32.10 | 0.0% | 13.3% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 23.0 | -2.77 | 0.0% | 33.3% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 9.7 | 8.53 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | 15.34 | 10.1% | 14.3% | 3 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 5.0 | 41.61 | 0.0% | 19.4% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 12.7 | -26.57 | 0.0% | 50.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 12.3 | 4.87 | 0.0% | 50.0% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.0 | 0.16 | 9.3% | 24.1% | 4 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 5.0 | 52.28 | 0.0% | 8.3% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 19.3 | 3.19 | 6.1% | 60.3% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 14.7 | -0.88 | 0.0% | 55.0% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 39.13 | 13.6% | 18.1% | 5 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.0 | 24.45 | 0.0% | 38.9% | 5 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 23.0 | 7.67 | 5.4% | 0.0% | 152 |
| long-change-timeout-everywhere | gemma4-think | 3 | 100.0% | 0.0 | 16.3 | -16.46 | 0.0% | 19.4% | 152 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 16.7 | 2.79 | 2.4% | 34.1% | 156 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 8.7 | 23.27 | 0.0% | 25.3% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 5.0 | 41.68 | 8.3% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -43.03 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | -41.15 | 0.0% | 41.8% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 4.3 | 79.51 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -185.78 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | -31.85 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 3.3 | 42.91 | 0.0% | 18.9% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 2.0 | 5.13 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 55 |
| gemma4 | max_turns | 5 |
| gemma4-think | end_turn | 58 |
| gemma4-think | provider_error | 2 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 6 | 343 | 1 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | yes | end_turn | 6 | 250 | 1 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 6 | 338 | 1 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 5 | 315 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 5 | 370 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 5 | 203 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1331 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1718 | 0 | 1 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1014 | 0 | 1 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 5 | 369 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 4 | 374 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 3 | 315 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 9 | 504 | 0 | 2 | 12 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 6 | 552 | 0 | 0 | 14 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 6 | 491 | 0 | 0 | 14 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | yes | end_turn | 7 | 253 | 1 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | no | provider_error | 1 | 144 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` (poo exited 20) |
| find-off-by-one-loop | gemma4-think | 3 | no | provider_error | 1 | 245 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` (poo exited 20) |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 1117 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 939 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 817 | 0 | 1 | 0 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 3 | 328 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 7 | 396 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 4 | 336 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 357 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 249 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 395 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 306 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 216 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 255 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1088 | 0 | 2 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 946 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 846 | 0 | 1 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 4 | 366 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 4 | 354 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 3 | 558 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 7 | 335 | 1 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 9 | 297 | 1 | 1 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 8 | 437 | 1 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 5 | 388 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 4 | 245 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 5 | 210 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 983 | 0 | 1 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 893 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 18 | 1232 | 0 | 3 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 5 | 417 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 5 | 400 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 4 | 345 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 5 | 320 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 5 | 244 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 5 | 243 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | no | end_turn | 3 | 242 | 1 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 6 | 182 | 1 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 6 | 161 | 1 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 943 | 0 | 1 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1006 | 0 | 1 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 1304 | 0 | 1 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 4 | 344 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 4 | 333 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 4 | 347 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | max_turns | 50 | 536 | 1 | 44 | 6 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/gemma4/seed1/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4 | 2 | yes | max_turns | 50 | 525 | 1 | 44 | 6 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/gemma4/seed2/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4 | 3 | yes | max_turns | 50 | 488 | 1 | 44 | 6 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 21 | 392 | 0 | 4 | 17 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 12 | 323 | 0 | 1 | 3 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 10 | 322 | 0 | 0 | 3 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 1114 | 1 | 3 | 6 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 15 | 1028 | 4 | 1 | 5 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1130 | 1 | 1 | 3 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 4 | 343 | 0 | 0 | 4 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 4 | 411 | 0 | 1 | 3 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 8 | 431 | 0 | 1 | 3 | `runs/run-c7282d4ed7416d5c/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 15 | 423 | 0 | 3 | 2 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 15 | 466 | 0 | 3 | 2 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 15 | 486 | 0 | 3 | 2 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 17 | 344 | 1 | 7 | 2 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 17 | 356 | 1 | 6 | 4 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 17 | 254 | 1 | 6 | 3 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 1118 | 1 | 1 | 3 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 933 | 1 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1289 | 1 | 1 | 4 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 8 | 390 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 5 | 340 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 5 | 345 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 11 | 545 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 11 | 451 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 11 | 477 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 11 | 317 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 11 | 264 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 12 | 301 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1126 | 1 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1158 | 1 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 1104 | 1 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 6 | 477 | 0 | 3 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 5 | 347 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 4 | 338 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 15 | 421 | 1 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 18 | 752 | 3 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 19 | 534 | 3 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 12 | 264 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 10 | 204 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 10 | 378 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 868 | 1 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1099 | 1 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1000 | 1 | 0 | 4 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 5 | 331 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 6 | 338 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 5 | 361 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 10 | 579 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 12 | 519 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 9 | 406 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 9 | 337 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 8 | 328 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 11 | 336 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1193 | 1 | 0 | 4 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 965 | 2 | 1 | 4 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1237 | 1 | 1 | 5 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 5 | 344 | 0 | 0 | 4 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 9 | 403 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 7 | 458 | 0 | 1 | 4 | `runs/run-c7282d4ed7416d5c/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | max_turns | 50 | 835 | 0 | 3 | 2 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/gemma4/seed1/session.jsonl` (poo exited 11) |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 13 | 605 | 1 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 13 | 534 | 1 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 10 | 365 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 11 | 310 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 10 | 309 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 865 | 1 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 814 | 1 | 1 | 4 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 819 | 1 | 1 | 4 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 6 | 364 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 7 | 447 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 5 | 354 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | end_turn | 13 | 1082 | 1 | 1 | 13 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | end_turn | 17 | 1070 | 1 | 4 | 14 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/gemma4/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 3 | yes | max_turns | 50 | 918 | 1 | 5 | 13 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4-think | 1 | yes | end_turn | 24 | 335 | 0 | 6 | 2 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | yes | end_turn | 14 | 309 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 3 | yes | end_turn | 13 | 478 | 0 | 0 | 7 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 930 | 1 | 2 | 19 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 13 | 984 | 1 | 4 | 30 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1141 | 1 | 1 | 21 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 5 | 338 | 0 | 0 | 3 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 7 | 354 | 0 | 0 | 4 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 7 | 353 | 0 | 0 | 7 | `runs/run-c7282d4ed7416d5c/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 16 | 632 | 3 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 14 | 463 | 2 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 16 | 553 | 3 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 11 | 367 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 10 | 281 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 10 | 211 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 961 | 2 | 3 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1758 | 1 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 1270 | 1 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 5 | 349 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 5 | 369 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 6 | 406 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 19 | 445 | 2 | 5 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 20 | 484 | 2 | 5 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 19 | 527 | 2 | 5 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 17 | 344 | 0 | 2 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 19 | 320 | 0 | 4 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 16 | 232 | 0 | 2 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1214 | 1 | 1 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1033 | 1 | 3 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 863 | 1 | 2 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 4 | 342 | 0 | 0 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 6 | 352 | 0 | 2 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 6 | 344 | 0 | 0 | 3 | `runs/run-c7282d4ed7416d5c/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 43 | 587 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 13 | 464 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 13 | 457 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 10 | 303 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 11 | 271 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 8 | 264 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1275 | 1 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 921 | 1 | 1 | 4 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 892 | 1 | 1 | 4 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 5 | 368 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 6 | 401 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 4 | 454 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 13 | 503 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 12 | 549 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 13 | 516 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 10 | 341 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 13 | 248 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 14 | 275 | 0 | 2 | 2 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 1031 | 1 | 4 | 7 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1032 | 1 | 3 | 4 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1340 | 1 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 6 | 344 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 4 | 460 | 0 | 1 | 2 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 5 | 423 | 0 | 0 | 2 | `runs/run-c7282d4ed7416d5c/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 19 | 521 | 1 | 4 | 3 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 19 | 624 | 1 | 4 | 3 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 20 | 629 | 1 | 6 | 3 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 15 | 324 | 0 | 5 | 3 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 15 | 202 | 0 | 5 | 3 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 14 | 212 | 0 | 2 | 3 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1131 | 1 | 1 | 4 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 919 | 2 | 2 | 6 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1056 | 1 | 1 | 4 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 5 | 384 | 0 | 1 | 4 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 7 | 426 | 0 | 2 | 4 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 6 | 390 | 0 | 1 | 6 | `runs/run-c7282d4ed7416d5c/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 22 | 746 | 1 | 0 | 152 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 27 | 974 | 3 | 0 | 152 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 20 | 712 | 0 | 0 | 152 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 13 | 428 | 0 | 0 | 152 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 20 | 381 | 0 | 1 | 152 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | yes | end_turn | 16 | 387 | 0 | 1 | 152 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 20 | 1127 | 1 | 10 | 156 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 14 | 1059 | 0 | 7 | 156 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 16 | 1161 | 1 | 5 | 156 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 8 | 698 | 0 | 1 | 156 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 8 | 552 | 0 | 2 | 156 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 10 | 581 | 0 | 3 | 156 | `runs/run-c7282d4ed7416d5c/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 6 | 711 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 6 | 513 | 1 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 3 | 419 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 4 | 374 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 4 | 310 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 4 | 147 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 1055 | 0 | 10 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1064 | 0 | 3 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 911 | 0 | 4 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 4 | 394 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 5 | 442 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 4 | 512 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 3 | 452 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 3 | 317 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 3 | 517 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 3 | 348 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 3 | 421 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 3 | 187 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 1137 | 0 | 2 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 3 | 3463 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 3 | 1160 | 0 | 1 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 2 | 285 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 2 | 282 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 2 | 283 | 0 | 0 | 0 | `runs/run-c7282d4ed7416d5c/long-find-error-in-log/qwen/seed3/session.jsonl` |
