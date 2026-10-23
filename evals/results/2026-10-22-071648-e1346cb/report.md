# poo eval report

- started: 2026-10-22T07:16:48.583132087Z
- finished: 2026-10-22T07:44:13.119992598Z
- git sha: e1346cb
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 86.7% | 2.9 | 15.5 | 59103 | 592 | 12375 | 480 | -15.22 | 4.0% | 0.0% | 27.2% | 10 | 80.2% |
| gemma4-think | 60 | 95.0% | 5.0 | 10.4 | 27678 | 1233 | 19249 | 314 | -6.14 | 2.9% | 0.0% | 23.0% | 6133 | 82.2% |
| minimax-m3.1-flash | 60 | 100.0% | 0.0 | 8.3 | 28111 | 2444 | 26611 | 1156 | 12.03 | 7.2% | 0.0% | 17.5% | 11 | 83.5% |
| qwen | 60 | 100.0% | 0.0 | 5.4 | 13111 | 629 | 8933 | 425 | 21.85 | 0.0% | 0.0% | 14.5% | 10 | n/a |
| **all** | 240 | 95.4% | 1.9 | 9.9 | 32001 | 1224 | 16792 | 594 | 3.13 | 3.5% | 0.0% | 20.5% | 1541 | 81.9% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 100.0% | 0.0 | 6.3 | -33.91 | 18.9% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 6.0 | -12.35 | 20.0% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 30.59 | 0.0% | 8.3% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 5.3 | 3.99 | 0.0% | 5.6% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 3.7 | 13.17 | 0.0% | 13.3% | 4 |
| find-off-by-one-loop | gemma4-think | 3 | 100.0% | 0.0 | 5.3 | -6.26 | 6.7% | 0.0% | 0 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 18.46 | 0.0% | 20.9% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 4.3 | -1.28 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -55.55 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -12.56 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.7 | 52.42 | 0.0% | 28.0% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 4.7 | 9.87 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 7.7 | 1.69 | 8.9% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 5.0 | -4.56 | 0.0% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.3 | 71.91 | 0.0% | 6.1% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 3.7 | 36.48 | 0.0% | 8.3% | 0 |
| find-version-mismatch | gemma4 | 3 | 0.0% | 0.0 | 1.0 | 0.00 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 6.0 | -27.52 | 20.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -42.65 | 0.0% | 13.1% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.0 | 32.68 | 0.0% | 0.0% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 66.7% | 57.7 | 40.0 | 13.76 | 0.0% | 32.4% | 2 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 17.0 | 31.16 | 0.0% | 26.1% | 61239 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.3 | 29.74 | 11.0% | 10.8% | 7 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 6.3 | 39.30 | 0.0% | 11.1% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 38.0 | -1.62 | 2.6% | 16.7% | 12 |
| fix-csv-skips-header | gemma4-think | 3 | 66.7% | 57.7 | 14.3 | -5.08 | 9.9% | 61.7% | 2 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.7 | -6.35 | 11.4% | 21.1% | 2 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.0 | 60.05 | 0.0% | 33.3% | 2 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 12.0 | -15.02 | 2.8% | 33.3% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 11.0 | -1.05 | 0.0% | 50.0% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.0 | 64.49 | 13.7% | 20.1% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 6.0 | 27.83 | 0.0% | 30.6% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 15.0 | 1.46 | 8.8% | 25.0% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | -12.24 | 0.0% | 19.4% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 7.98 | 13.7% | 9.7% | 3 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 6.0 | -27.07 | 0.0% | 30.6% | 3 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 9.0 | -8.12 | 0.0% | 33.3% | 2 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 9.3 | -0.13 | 0.0% | 33.3% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -6.43 | 12.6% | 4.8% | 3 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 5.3 | 12.42 | 0.0% | 22.2% | 3 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 37.7 | 4.13 | 0.0% | 50.0% | 2 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 10.7 | 0.23 | 0.0% | 50.0% | 2 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.7 | -42.81 | 9.0% | 26.0% | 11 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.3 | -46.50 | 0.0% | 38.9% | 3 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 49.3 | 5.90 | 10.2% | 66.7% | 2 |
| fix-path-join-double-slash | gemma4-think | 3 | 66.7% | 57.7 | 20.3 | 10.75 | 0.0% | 32.4% | 61240 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.7 | 80.32 | 8.1% | 28.5% | 19 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 6.0 | 16.99 | 0.0% | 5.6% | 6 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -10.42 | 0.0% | 33.3% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | 13.91 | 0.0% | 33.3% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -2.67 | 11.2% | 15.1% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 5.3 | 30.28 | 0.0% | 0.0% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 17.7 | 2.57 | 12.0% | 62.5% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 66.7% | 57.7 | 23.3 | -26.01 | 0.0% | 30.9% | 2 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 60.61 | 12.5% | 5.6% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 5.3 | 31.96 | 0.0% | 21.7% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -16.13 | 11.1% | 33.3% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 8.3 | 0.32 | 0.0% | 0.0% | 5 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -43.87 | 11.1% | 12.2% | 3 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 5.7 | 37.07 | 0.0% | 11.1% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 10.7 | -8.43 | 0.0% | 50.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 11.7 | 9.38 | 0.0% | 53.3% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.3 | 33.00 | 14.9% | 21.6% | 3 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 4.7 | 29.72 | 0.0% | 11.1% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 10.3 | 22.42 | 0.0% | 43.3% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 13.3 | -1.40 | 0.0% | 46.7% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.0 | -20.58 | 12.6% | 25.2% | 6 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 5.0 | 137.45 | 0.0% | 31.9% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 20.7 | 2.43 | 5.1% | 50.0% | 152 |
| long-change-timeout-everywhere | gemma4-think | 3 | 100.0% | 0.0 | 16.7 | -8.77 | 2.1% | 22.2% | 152 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 13.7 | -106.84 | 1.6% | 36.5% | 156 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 12.0 | 0.53 | 0.0% | 28.8% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -95.82 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -12.22 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -56.25 | 0.0% | 20.4% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 3.7 | -2.60 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4 | 3 | 66.7% | 57.7 | 2.3 | -126.86 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | -58.31 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 4.3 | 119.50 | 0.0% | 16.7% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 2.0 | 7.92 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 53 |
| gemma4 | max_turns | 7 |
| gemma4-think | end_turn | 59 |
| gemma4-think | output_truncated | 1 |
| minimax-m3.1-flash | end_turn | 59 |
| minimax-m3.1-flash | provider_error | 1 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 6 | 356 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | yes | end_turn | 6 | 448 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 7 | 257 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 6 | 236 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 6 | 294 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 6 | 292 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1624 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1030 | 0 | 2 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 957 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 5 | 344 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 7 | 462 | 0 | 1 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 4 | 420 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 9 | 632 | 0 | 2 | 11 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | yes | end_turn | 5 | 315 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | yes | end_turn | 5 | 314 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | yes | end_turn | 6 | 270 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1014 | 0 | 3 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1288 | 0 | 2 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1166 | 0 | 1 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 4 | 540 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 5 | 350 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 4 | 307 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 415 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 438 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 341 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 298 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 205 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 331 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1006 | 0 | 1 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1283 | 0 | 5 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 906 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 4 | 312 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 4 | 339 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 6 | 390 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 9 | 554 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 8 | 411 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 6 | 329 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 5 | 163 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 5 | 207 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 5 | 355 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 1023 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 1271 | 0 | 2 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 1300 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 4 | 326 | 0 | 1 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 3 | 444 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 4 | 341 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 6 | 380 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 6 | 222 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 6 | 157 | 1 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1078 | 0 | 1 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1221 | 0 | 1 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 1012 | 0 | 1 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 4 | 312 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 4 | 334 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 4 | 340 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 32 | 732 | 0 | 2 | 4 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 38 | 745 | 0 | 4 | 3 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | no | max_turns | 50 | 680 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 19 | 418 | 0 | 2 | 47 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 23 | 342 | 0 | 1 | 183667 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 9 | 484 | 0 | 1 | 3 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 914 | 1 | 1 | 7 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 1115 | 1 | 2 | 7 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 12 | 924 | 2 | 0 | 7 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 7 | 421 | 0 | 1 | 3 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 6 | 369 | 0 | 0 | 3 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 6 | 384 | 0 | 0 | 3 | `runs/run-ee9482c9f727c83f/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | max_turns | 50 | 471 | 0 | 0 | 4 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/gemma4/seed1/session.jsonl` (poo exited 11) |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 14 | 638 | 1 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | max_turns | 50 | 481 | 0 | 0 | 29 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 15 | 373 | 1 | 5 | 2 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | no | end_turn | 12 | 279 | 1 | 3 | 1 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 16 | 260 | 2 | 5 | 4 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 1069 | 1 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1018 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 874 | 2 | 3 | 2 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 6 | 361 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 6 | 553 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 6 | 394 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 12 | 634 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 13 | 697 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 11 | 505 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 11 | 349 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 11 | 288 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 11 | 321 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1153 | 1 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 1289 | 2 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1152 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 7 | 352 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 5 | 341 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 6 | 349 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 14 | 513 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 14 | 465 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 17 | 487 | 3 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 6 | 398 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 12 | 272 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 13 | 267 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1008 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1070 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 842 | 1 | 0 | 4 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 7 | 553 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 6 | 405 | 0 | 1 | 6 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 5 | 339 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 9 | 523 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 9 | 460 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 9 | 455 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 10 | 328 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 9 | 338 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 9 | 410 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1133 | 1 | 0 | 5 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 964 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1152 | 1 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 5 | 474 | 0 | 0 | 4 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 5 | 388 | 0 | 1 | 4 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 432 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | max_turns | 50 | 873 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/gemma4/seed1/session.jsonl` (poo exited 11) |
| fix-off-by-one-slice | gemma4 | 2 | yes | max_turns | 50 | 860 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/gemma4/seed2/session.jsonl` (poo exited 11) |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 13 | 730 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 11 | 338 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 10 | 248 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | output_truncated | 11 | 321 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` (poo exited 10) |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 14 | 1252 | 1 | 3 | 20 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 915 | 1 | 2 | 8 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1247 | 1 | 2 | 4 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 8 | 404 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 5 | 649 | 0 | 0 | 4 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 6 | 409 | 0 | 2 | 4 | `runs/run-ee9482c9f727c83f/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | max_turns | 50 | 538 | 5 | 4 | 2 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/gemma4/seed1/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4 | 2 | yes | end_turn | 48 | 853 | 5 | 4 | 2 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/gemma4/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 3 | yes | max_turns | 50 | 783 | 5 | 4 | 2 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4-think | 1 | yes | end_turn | 15 | 337 | 0 | 2 | 27 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | yes | end_turn | 16 | 432 | 0 | 4 | 7 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 3 | no | end_turn | 30 | 334 | 0 | 0 | 183687 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 895 | 1 | 3 | 15 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1092 | 1 | 2 | 21 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 12 | 1136 | 1 | 4 | 20 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 7 | 402 | 0 | 1 | 4 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 5 | 356 | 0 | 0 | 9 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 6 | 505 | 0 | 0 | 6 | `runs/run-ee9482c9f727c83f/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 10 | 399 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 10 | 305 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 10 | 306 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 9 | 360 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 9 | 268 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 9 | 241 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1092 | 1 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1051 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 978 | 1 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 5 | 384 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 5 | 353 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 6 | 382 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 17 | 615 | 2 | 5 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 18 | 670 | 2 | 5 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 18 | 626 | 2 | 5 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 19 | 287 | 0 | 3 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 18 | 746 | 0 | 4 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | no | end_turn | 33 | 288 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1201 | 1 | 0 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1331 | 1 | 1 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1239 | 1 | 0 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 5 | 466 | 0 | 2 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 6 | 375 | 0 | 1 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 5 | 359 | 0 | 0 | 3 | `runs/run-ee9482c9f727c83f/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 10 | 573 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 10 | 536 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 10 | 499 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 8 | 328 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 9 | 346 | 0 | 0 | 11 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 8 | 322 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1074 | 1 | 0 | 4 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1269 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1039 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 7 | 473 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 5 | 502 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 5 | 595 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 10 | 531 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 11 | 497 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 11 | 504 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 13 | 333 | 0 | 3 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 11 | 314 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 11 | 272 | 0 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 13 | 959 | 4 | 3 | 4 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 889 | 1 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1111 | 1 | 2 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 5 | 348 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 5 | 344 | 0 | 0 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 4 | 342 | 0 | 1 | 2 | `runs/run-ee9482c9f727c83f/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 10 | 675 | 0 | 2 | 3 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 11 | 658 | 0 | 3 | 3 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 10 | 717 | 0 | 2 | 3 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 12 | 304 | 0 | 2 | 3 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 15 | 274 | 0 | 3 | 3 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 13 | 321 | 0 | 3 | 3 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1061 | 1 | 1 | 4 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 1032 | 2 | 3 | 9 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1224 | 1 | 2 | 4 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 5 | 422 | 0 | 1 | 4 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 5 | 596 | 0 | 1 | 4 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 5 | 604 | 0 | 3 | 4 | `runs/run-ee9482c9f727c83f/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 21 | 517 | 1 | 1 | 152 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 21 | 465 | 1 | 1 | 152 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 20 | 377 | 1 | 1 | 152 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 15 | 468 | 0 | 0 | 152 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 17 | 389 | 1 | 1 | 152 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | yes | end_turn | 18 | 403 | 0 | 1 | 152 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | provider_error | 12 | 1261 | 0 | 7 | 156 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` (poo exited 20) |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 16 | 2986 | 0 | 12 | 156 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 13 | 1624 | 1 | 6 | 156 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 13 | 764 | 0 | 4 | 156 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 12 | 553 | 0 | 3 | 156 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 11 | 600 | 0 | 1 | 156 | `runs/run-ee9482c9f727c83f/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 4 | 478 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 4 | 284 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 4 | 287 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 4 | 299 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 4 | 146 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 4 | 145 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1202 | 0 | 3 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1272 | 0 | 1 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1155 | 0 | 2 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 3 | 346 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 4 | 480 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 4 | 958 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 3 | 360 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 3 | 637 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 3 | 307 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 3 | 314 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 3 | 183 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 1628 | 0 | 4 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 4 | 886 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 1406 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 2 | 280 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 2 | 277 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 2 | 281 | 0 | 0 | 0 | `runs/run-ee9482c9f727c83f/long-find-error-in-log/qwen/seed3/session.jsonl` |
