# poo eval report

- started: 2026-10-22T10:33:36.988247928Z
- finished: 2026-10-22T10:55:31.890405352Z
- git sha: 0d338e7
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 93.3% | 2.9 | 12.2 | 32149 | 680 | 11731 | 500 | -11.08 | 7.2% | 0.0% | 30.7% | 10 | 85.4% |
| gemma4-think | 60 | 96.7% | 2.9 | 10.0 | 24777 | 1160 | 16168 | 286 | -4.08 | 1.8% | 0.0% | 22.9% | 10 | 82.2% |
| minimax-m3.1-flash | 60 | 100.0% | 0.0 | 8.2 | 30768 | 2408 | 24435 | 1117 | 12.53 | 7.5% | 0.0% | 16.6% | 11 | 85.4% |
| qwen | 60 | 98.3% | 2.9 | 5.3 | 11457 | 671 | 6842 | 385 | 30.42 | 0.2% | 0.0% | 17.2% | 10 | n/a |
| **all** | 240 | 97.1% | 0.7 | 8.9 | 24788 | 1230 | 14794 | 572 | 6.95 | 4.2% | 0.0% | 21.9% | 10 | 84.3% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 100.0% | 0.0 | 6.0 | -31.55 | 20.0% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 5.0 | -8.74 | 0.0% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | -165.57 | 0.0% | 12.2% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 4.7 | 52.46 | 0.0% | 6.7% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 5.3 | 49.29 | 0.0% | 13.3% | 9 |
| find-off-by-one-loop | gemma4-think | 3 | 33.3% | 57.7 | 2.7 | 8.78 | 6.7% | 0.0% | 0 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.7 | 24.65 | 0.0% | 10.8% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 4.3 | -13.05 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -63.82 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -22.00 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 3.86 | 0.0% | 15.1% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 4.0 | 37.25 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 8.0 | -2.56 | 14.5% | 11.1% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 5.0 | -4.22 | 0.0% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.7 | 8.27 | 0.0% | 7.0% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 5.3 | -23.52 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 66.7% | 57.7 | 3.7 | -26.63 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 6.0 | -8.45 | 20.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.3 | -5.27 | 0.0% | 14.3% | 0 |
| find-version-mismatch | qwen | 3 | 66.7% | 57.7 | 4.0 | 29.23 | 0.0% | 16.7% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 19.0 | 21.30 | 6.2% | 66.7% | 3 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 10.7 | -1.93 | 0.0% | 30.0% | 4 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | -16.85 | 9.8% | 14.8% | 7 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 6.3 | 13.71 | 0.0% | 35.6% | 4 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 13.3 | -2.03 | 0.0% | 50.0% | 2 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 14.0 | -1.89 | 8.0% | 50.8% | 3 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | 27.18 | 14.4% | 16.7% | 3 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 5.7 | 34.58 | 4.2% | 11.1% | 3 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 11.0 | -16.50 | 0.0% | 33.3% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 11.3 | -6.65 | 0.0% | 44.4% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | 5.51 | 14.4% | 20.6% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 5.3 | 33.73 | 0.0% | 38.9% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 15.7 | -8.43 | 19.2% | 30.6% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | 13.15 | 0.0% | 19.4% | 3 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -6.58 | 11.1% | 13.1% | 3 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 5.7 | 28.09 | 0.0% | 35.6% | 3 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 10.3 | -19.79 | 3.3% | 33.3% | 2 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | -1.77 | 0.0% | 22.2% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -15.24 | 11.2% | 5.6% | 4 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 6.0 | 42.91 | 0.0% | 25.0% | 4 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 14.3 | -6.10 | 0.0% | 66.7% | 2 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 11.0 | 10.55 | 0.0% | 38.9% | 2 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -14.28 | 11.7% | 20.4% | 5 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.0 | 45.32 | 0.0% | 33.3% | 3 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 30.7 | -10.32 | 7.9% | 66.7% | 11 |
| fix-path-join-double-slash | gemma4-think | 3 | 100.0% | 0.0 | 21.7 | 19.04 | 0.0% | 40.6% | 17 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.7 | 44.63 | 12.7% | 23.6% | 28 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 6.0 | 30.65 | 0.0% | 6.7% | 8 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 14.0 | -7.73 | 16.7% | 33.3% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | 0.15 | 0.0% | 33.3% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 42.83 | 14.8% | 22.6% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 5.0 | 36.54 | 0.0% | 0.0% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 18.7 | -3.11 | 13.4% | 62.5% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 16.7 | -0.97 | 0.0% | 43.3% | 3 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -23.75 | 11.8% | 15.1% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 4.7 | 32.88 | 0.0% | 16.7% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 12.0 | -18.85 | 11.1% | 33.3% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 8.7 | -3.87 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -23.30 | 8.9% | 18.1% | 3 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 7.0 | 29.47 | 0.0% | 16.7% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 12.3 | -12.95 | 0.0% | 50.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 12.0 | -3.46 | 0.0% | 44.4% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.3 | 19.68 | 15.6% | 24.4% | 4 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 4.3 | 35.03 | 0.0% | 19.4% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 18.7 | 2.94 | 5.8% | 63.5% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 13.3 | 0.67 | 0.0% | 50.0% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 293.28 | 10.9% | 13.9% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 8.0 | 31.89 | 0.0% | 45.8% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 19.7 | 9.34 | 1.7% | 0.0% | 152 |
| long-change-timeout-everywhere | gemma4-think | 3 | 100.0% | 0.0 | 20.7 | 5.55 | 1.5% | 41.1% | 152 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 16.7 | -12.98 | 2.9% | 34.1% | 158 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 8.3 | 5.07 | 0.0% | 24.6% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 5.3 | 32.99 | 25.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -12.41 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 5.98 | 0.0% | 29.0% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 3.7 | 123.37 | 0.0% | 11.1% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -107.03 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | -63.13 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 3.7 | 58.60 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 2.0 | 2.80 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 57 |
| gemma4 | max_turns | 1 |
| gemma4 | provider_error | 2 |
| gemma4-think | end_turn | 58 |
| gemma4-think | provider_error | 2 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 6 | 331 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | yes | end_turn | 6 | 474 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 6 | 491 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 5 | 371 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 5 | 221 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 5 | 265 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 867 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 1148 | 0 | 2 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 2148 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 4 | 337 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 5 | 428 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 5 | 438 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 6 | 624 | 0 | 0 | 14 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 9 | 593 | 0 | 2 | 13 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | provider_error | 1 | 0 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/gemma4/seed3/session.jsonl` (poo exited 20) |
| find-off-by-one-loop | gemma4-think | 1 | no | provider_error | 1 | 454 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` (poo exited 20) |
| find-off-by-one-loop | gemma4-think | 2 | yes | end_turn | 6 | 256 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | no | provider_error | 1 | 190 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` (poo exited 20) |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 13 | 979 | 0 | 2 | 0 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 2361 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1044 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 5 | 329 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 5 | 353 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 3 | 413 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 341 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 249 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 434 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 314 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 212 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 252 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 741 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 964 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1048 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 3 | 345 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 4 | 355 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 5 | 339 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 8 | 308 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 9 | 301 | 1 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 7 | 331 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 5 | 324 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 5 | 218 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 5 | 179 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1043 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 1120 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 908 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 5 | 329 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 6 | 382 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 5 | 593 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 5 | 319 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 5 | 243 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | no | provider_error | 1 | 0 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/gemma4/seed3/session.jsonl` (poo exited 20) |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 6 | 237 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 6 | 168 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 6 | 164 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 853 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 891 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 842 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 4 | 303 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | no | end_turn | 4 | 341 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 4 | 335 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 19 | 879 | 1 | 6 | 3 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 19 | 969 | 1 | 6 | 3 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | yes | end_turn | 19 | 776 | 1 | 6 | 3 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/gemma4/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 11 | 335 | 0 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 10 | 263 | 0 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 11 | 268 | 0 | 2 | 4 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 775 | 1 | 1 | 9 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1055 | 1 | 1 | 6 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 1217 | 1 | 1 | 7 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 6 | 415 | 0 | 1 | 3 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 7 | 486 | 0 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 6 | 604 | 0 | 2 | 6 | `runs/run-fea947ffc1025ab5/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 13 | 438 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 14 | 416 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 13 | 514 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 14 | 325 | 1 | 3 | 4 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 15 | 258 | 1 | 5 | 3 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 13 | 250 | 1 | 2 | 3 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 787 | 1 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1135 | 1 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 923 | 2 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 5 | 339 | 0 | 0 | 4 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 7 | 399 | 1 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 5 | 335 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 11 | 446 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 10 | 629 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 12 | 421 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 11 | 250 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 13 | 366 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 10 | 313 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 1144 | 1 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1082 | 2 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 995 | 1 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 6 | 347 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 5 | 348 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 5 | 374 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 20 | 407 | 4 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 15 | 499 | 3 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 12 | 580 | 1 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 11 | 313 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 10 | 270 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 10 | 216 | 0 | 1 | 6 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 946 | 1 | 2 | 4 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 860 | 1 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1098 | 1 | 0 | 4 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 6 | 352 | 0 | 1 | 5 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 6 | 328 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 5 | 331 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 12 | 602 | 1 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 10 | 507 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 9 | 339 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 11 | 313 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 11 | 315 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 9 | 319 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1377 | 1 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 919 | 1 | 0 | 5 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1189 | 1 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 5 | 360 | 0 | 0 | 4 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 6 | 388 | 0 | 2 | 4 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 7 | 403 | 0 | 2 | 4 | `runs/run-fea947ffc1025ab5/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 14 | 607 | 0 | 4 | 2 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 14 | 442 | 0 | 4 | 2 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 15 | 457 | 0 | 4 | 2 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 11 | 339 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 11 | 242 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 11 | 269 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1147 | 1 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 939 | 1 | 3 | 4 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1130 | 1 | 1 | 7 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 5 | 378 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 6 | 408 | 0 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 7 | 358 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | end_turn | 20 | 1120 | 1 | 6 | 14 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | end_turn | 22 | 1260 | 3 | 6 | 17 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/gemma4/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 3 | yes | max_turns | 50 | 738 | 1 | 4 | 2 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4-think | 1 | yes | end_turn | 19 | 394 | 0 | 1 | 7 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | yes | end_turn | 14 | 362 | 0 | 1 | 7 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 3 | yes | end_turn | 32 | 483 | 0 | 10 | 36 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 935 | 1 | 2 | 19 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1191 | 1 | 3 | 33 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 15 | 1126 | 5 | 5 | 33 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 6 | 357 | 0 | 0 | 14 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 6 | 356 | 0 | 0 | 6 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 6 | 385 | 0 | 1 | 5 | `runs/run-fea947ffc1025ab5/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 14 | 400 | 2 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 14 | 421 | 2 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 14 | 459 | 2 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 9 | 296 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 9 | 207 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 9 | 363 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1430 | 1 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 919 | 1 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 910 | 2 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 5 | 348 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 5 | 354 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 5 | 362 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 18 | 627 | 2 | 5 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 19 | 644 | 3 | 5 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 19 | 668 | 2 | 5 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 17 | 330 | 0 | 2 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 17 | 321 | 0 | 2 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 16 | 305 | 0 | 3 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 843 | 1 | 1 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 980 | 1 | 1 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 812 | 1 | 1 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 6 | 345 | 0 | 3 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 4 | 326 | 0 | 0 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 4 | 339 | 0 | 0 | 3 | `runs/run-fea947ffc1025ab5/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 12 | 486 | 1 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 12 | 416 | 1 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 12 | 401 | 1 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 10 | 338 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 8 | 264 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 8 | 166 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 920 | 0 | 3 | 4 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1326 | 1 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 853 | 1 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 7 | 394 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 7 | 373 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 7 | 368 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 13 | 493 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 12 | 506 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 12 | 492 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 13 | 300 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 12 | 328 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 11 | 276 | 0 | 2 | 2 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 12 | 880 | 3 | 3 | 5 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1052 | 2 | 3 | 4 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1000 | 1 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 4 | 338 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 5 | 326 | 0 | 1 | 2 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 4 | 336 | 0 | 0 | 2 | `runs/run-fea947ffc1025ab5/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 17 | 756 | 1 | 4 | 3 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 19 | 674 | 1 | 6 | 3 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 20 | 638 | 1 | 6 | 3 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 13 | 311 | 0 | 3 | 3 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 13 | 246 | 0 | 3 | 3 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 14 | 220 | 0 | 3 | 3 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1648 | 1 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 3125 | 1 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1958 | 1 | 1 | 4 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 7 | 392 | 0 | 2 | 3 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 7 | 393 | 0 | 3 | 4 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 10 | 414 | 0 | 3 | 4 | `runs/run-fea947ffc1025ab5/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 22 | 457 | 1 | 0 | 152 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 20 | 607 | 0 | 0 | 152 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 17 | 750 | 0 | 0 | 152 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 23 | 390 | 1 | 1 | 152 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 23 | 349 | 0 | 2 | 152 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | yes | end_turn | 16 | 469 | 0 | 2 | 152 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 16 | 1218 | 1 | 5 | 156 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 16 | 1092 | 0 | 9 | 156 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 18 | 1170 | 1 | 8 | 161 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 10 | 522 | 0 | 4 | 156 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 7 | 612 | 0 | 2 | 156 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 8 | 490 | 0 | 1 | 156 | `runs/run-fea947ffc1025ab5/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 6 | 512 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 6 | 413 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 4 | 265 | 1 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 4 | 302 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 4 | 148 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 4 | 147 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1120 | 0 | 4 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 987 | 0 | 8 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1129 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 4 | 522 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 4 | 588 | 0 | 1 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 3 | 435 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 3 | 365 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 3 | 238 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 3 | 234 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 3 | 264 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 3 | 382 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 3 | 138 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 861 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 4 | 737 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 3 | 1128 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 2 | 282 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 2 | 282 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 2 | 283 | 0 | 0 | 0 | `runs/run-fea947ffc1025ab5/long-find-error-in-log/qwen/seed3/session.jsonl` |
