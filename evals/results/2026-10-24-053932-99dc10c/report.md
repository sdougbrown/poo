# poo eval report

- started: 2026-10-24T05:39:32.364267775Z
- finished: 2026-10-24T06:00:10.488189117Z
- git sha: 8a5db72
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 91.7% | 2.9 | 11.5 | 44784 | 734 | 13997 | 576 | -15.80 | 5.5% | 0.0% | 9.6% | 10 | 91.9% |
| gemma4-think | 60 | 91.7% | 2.9 | 12.1 | 47982 | 675 | 11785 | 534 | -16.14 | 6.3% | 0.0% | 9.0% | 8 | 92.8% |
| minimax-m3.1-flash | 60 | 100.0% | 0.0 | 7.4 | 30743 | 1613 | 18140 | 1031 | 1.57 | 0.4% | 0.0% | 13.4% | 11 | 87.3% |
| qwen | 60 | 100.0% | 0.0 | 7.1 | 24532 | 521 | 8535 | 602 | -1.01 | 1.0% | 0.0% | 6.3% | 10 | n/a |
| **all** | 240 | 95.8% | 1.4 | 9.5 | 37010 | 886 | 13114 | 686 | -7.85 | 3.3% | 0.0% | 9.6% | 10 | 90.7% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 100.0% | 0.0 | 8.0 | -0.89 | 28.6% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 8.3 | -15.13 | 31.6% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 1.30 | 0.0% | 0.0% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 5.3 | 9.26 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 100.0% | 0.0 | 5.0 | -119.76 | 0.0% | 50.0% | 0 |
| find-off-by-one-loop | gemma4-think | 3 | 100.0% | 0.0 | 5.0 | -55.46 | 0.0% | 50.0% | 0 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | 2.98 | 0.0% | 9.5% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 5.0 | 6.16 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -78.79 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -72.30 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.7 | -31.79 | 0.0% | 0.0% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 6.3 | -20.49 | 18.1% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 66.7% | 57.7 | 5.0 | 1.35 | 11.1% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 7.0 | -15.00 | 16.7% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | 31.36 | 0.0% | 15.9% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 5.0 | -53.10 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 5.0 | -34.85 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 7.0 | -4.63 | 0.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -37.11 | 0.0% | 8.5% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.7 | -54.38 | 0.0% | 0.0% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 66.7% | 57.7 | 39.7 | 20.18 | 1.6% | 16.7% | 2 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 38.3 | 15.49 | 5.4% | 8.3% | 3 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | -38.22 | 0.0% | 13.2% | 8 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 6.7 | 32.03 | 0.0% | 0.0% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 9.7 | -11.91 | 12.5% | 0.0% | 2 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 9.7 | -6.54 | 12.5% | 0.0% | 2 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | -3.90 | 0.0% | 17.5% | 3 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.0 | 23.76 | 0.0% | 0.0% | 3 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 9.3 | -17.07 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 8.7 | -15.05 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 20.03 | 0.0% | 9.5% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 6.0 | 23.00 | 0.0% | 0.0% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 10.0 | 18.65 | 0.0% | 0.0% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 12.0 | 6.66 | 5.6% | 0.0% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -91.13 | 0.0% | 23.6% | 3 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 6.3 | 13.36 | 0.0% | 0.0% | 3 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 9.7 | -15.62 | 12.5% | 0.0% | 2 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | 16.97 | 12.5% | 0.0% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -43.09 | 0.0% | 18.6% | 4 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 6.3 | 20.50 | 0.0% | 15.0% | 3 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -23.27 | 12.5% | 0.0% | 4 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -16.53 | 12.5% | 0.0% | 4 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 72.46 | 3.7% | 20.6% | 3 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 7.0 | 31.12 | 0.0% | 13.3% | 3 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 43.3 | 31.79 | 5.0% | 91.1% | 17 |
| fix-path-join-double-slash | gemma4-think | 3 | 66.7% | 57.7 | 42.7 | -13.10 | 5.2% | 89.1% | 11 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 19.45 | 0.0% | 20.4% | 17 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 24.7 | -11.42 | 0.0% | 43.9% | 10 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 8.0 | 4.18 | 0.0% | 0.0% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 8.3 | 6.84 | 0.0% | 0.0% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -26.08 | 0.0% | 15.3% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 6.3 | 16.00 | 0.0% | 0.0% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 9.3 | 3.74 | 12.5% | 0.0% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 9.7 | -2.54 | 13.7% | 0.0% | 3 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.7 | 50.29 | 0.0% | 11.4% | 4 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 6.3 | 16.77 | 0.0% | 8.3% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 8.3 | -16.06 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 8.3 | -14.80 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 26.24 | 0.0% | 22.0% | 2 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 7.3 | 17.51 | 0.0% | 6.7% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 8.0 | 3.53 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 8.3 | 0.87 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 25.40 | 4.2% | 5.6% | 2 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 6.0 | -29.56 | 0.0% | 20.0% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 11.3 | 4.00 | 14.1% | 33.3% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | -8.43 | 11.1% | 33.3% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 70.33 | 0.0% | 10.8% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.7 | -10.43 | 0.0% | 0.0% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 22.3 | 17.35 | 0.0% | 0.0% | 155 |
| long-change-timeout-everywhere | gemma4-think | 3 | 66.7% | 57.7 | 30.3 | 18.73 | 0.0% | 0.0% | 129 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 16.7 | 20.83 | 1.2% | 33.4% | 158 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 13.0 | -15.48 | 2.2% | 19.4% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -102.64 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | -148.82 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -64.90 | 0.0% | 12.2% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 4.0 | 2.71 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4 | 3 | 0.0% | 0.0 | 1.0 | 0.00 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 0.0% | 0.0 | 1.0 | 0.00 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 3.7 | 26.87 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 3.7 | -37.61 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 54 |
| gemma4 | max_turns | 2 |
| gemma4 | provider_error | 4 |
| gemma4-think | end_turn | 53 |
| gemma4-think | max_turns | 4 |
| gemma4-think | provider_error | 3 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 59 |
| qwen | max_turns | 1 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 8 | 313 | 2 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | yes | end_turn | 8 | 301 | 2 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 8 | 281 | 2 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 8 | 468 | 2 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 9 | 400 | 3 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 8 | 414 | 2 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 820 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1106 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1021 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 6 | 457 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 6 | 492 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 4 | 494 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | yes | end_turn | 5 | 496 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | yes | end_turn | 5 | 394 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | yes | end_turn | 5 | 385 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | yes | end_turn | 5 | 259 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | yes | end_turn | 5 | 571 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | yes | end_turn | 5 | 425 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1182 | 0 | 2 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 995 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 815 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 7 | 981 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 5 | 473 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 3 | 454 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 431 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 387 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 307 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 309 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 344 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 266 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 904 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 914 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 959 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 5 | 491 | 2 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 7 | 781 | 1 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 7 | 608 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 7 | 422 | 1 | 0 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | no | provider_error | 1 | 0 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/gemma4/seed2/session.jsonl` (poo exited 20) |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 7 | 292 | 1 | 0 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 7 | 257 | 1 | 0 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 7 | 401 | 1 | 0 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 7 | 359 | 1 | 0 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 980 | 0 | 4 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 891 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1091 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 5 | 698 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 5 | 750 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 5 | 645 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 5 | 290 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 5 | 393 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 5 | 435 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 8 | 572 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 8 | 307 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 5 | 265 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 890 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 987 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 987 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 5 | 631 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 4 | 614 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 5 | 708 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 31 | 1139 | 0 | 0 | 3 | `runs/run-80b0aede01cce977/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 38 | 1362 | 1 | 1 | 3 | `runs/run-80b0aede01cce977/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | no | max_turns | 50 | 735 | 1 | 0 | 0 | `runs/run-80b0aede01cce977/fix-cache-never-expires/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 34 | 704 | 1 | 0 | 3 | `runs/run-80b0aede01cce977/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 31 | 750 | 2 | 1 | 3 | `runs/run-80b0aede01cce977/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | max_turns | 50 | 692 | 3 | 0 | 3 | `runs/run-80b0aede01cce977/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 972 | 0 | 1 | 7 | `runs/run-80b0aede01cce977/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 866 | 0 | 1 | 9 | `runs/run-80b0aede01cce977/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 922 | 0 | 1 | 7 | `runs/run-80b0aede01cce977/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 6 | 513 | 0 | 0 | 3 | `runs/run-80b0aede01cce977/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 7 | 673 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 7 | 723 | 0 | 0 | 3 | `runs/run-80b0aede01cce977/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 10 | 984 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 10 | 713 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 9 | 695 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 9 | 857 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 10 | 775 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 10 | 598 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 995 | 0 | 2 | 3 | `runs/run-80b0aede01cce977/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 917 | 0 | 1 | 2 | `runs/run-80b0aede01cce977/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 958 | 0 | 1 | 4 | `runs/run-80b0aede01cce977/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 6 | 568 | 0 | 0 | 4 | `runs/run-80b0aede01cce977/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 6 | 598 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 6 | 655 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 9 | 489 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 10 | 410 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 9 | 324 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 9 | 424 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 8 | 478 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 9 | 503 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 948 | 0 | 2 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1030 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 819 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 6 | 464 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 6 | 1001 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 6 | 597 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 10 | 589 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 10 | 596 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 10 | 545 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 13 | 545 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 10 | 596 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 13 | 482 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1161 | 0 | 3 | 4 | `runs/run-80b0aede01cce977/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1180 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1383 | 0 | 3 | 4 | `runs/run-80b0aede01cce977/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 6 | 568 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 7 | 571 | 0 | 0 | 5 | `runs/run-80b0aede01cce977/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 6 | 455 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 11 | 530 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 9 | 631 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 9 | 621 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 9 | 495 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 9 | 669 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 9 | 559 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 759 | 0 | 1 | 4 | `runs/run-80b0aede01cce977/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1087 | 0 | 2 | 5 | `runs/run-80b0aede01cce977/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 904 | 0 | 1 | 4 | `runs/run-80b0aede01cce977/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 7 | 476 | 0 | 1 | 2 | `runs/run-80b0aede01cce977/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 6 | 527 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 484 | 0 | 1 | 5 | `runs/run-80b0aede01cce977/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 9 | 496 | 1 | 0 | 4 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 10 | 654 | 1 | 0 | 4 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 11 | 482 | 1 | 0 | 4 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 9 | 532 | 1 | 0 | 4 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 10 | 369 | 1 | 0 | 4 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 11 | 360 | 1 | 0 | 4 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1223 | 0 | 1 | 2 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1057 | 0 | 1 | 4 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1109 | 1 | 1 | 4 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 8 | 529 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 7 | 610 | 0 | 2 | 4 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 6 | 705 | 0 | 0 | 4 | `runs/run-80b0aede01cce977/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | max_turns | 50 | 2023 | 2 | 20 | 23 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/gemma4/seed1/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4 | 2 | yes | end_turn | 47 | 977 | 2 | 29 | 8 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/gemma4/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 3 | yes | end_turn | 33 | 2312 | 2 | 16 | 20 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/gemma4/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 1 | yes | end_turn | 28 | 1145 | 2 | 11 | 8 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | no | max_turns | 50 | 1490 | 2 | 24 | 11 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4-think | 3 | yes | max_turns | 50 | 870 | 2 | 19 | 14 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 872 | 0 | 2 | 17 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1074 | 0 | 1 | 20 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 817 | 0 | 1 | 13 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | max_turns | 50 | 576 | 0 | 14 | 9 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/qwen/seed1/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 13 | 572 | 0 | 4 | 10 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 11 | 612 | 0 | 1 | 11 | `runs/run-80b0aede01cce977/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 8 | 492 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 8 | 436 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 8 | 431 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 8 | 706 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 9 | 644 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 8 | 682 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 918 | 0 | 3 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 901 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 843 | 0 | 1 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 6 | 477 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 6 | 550 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 7 | 549 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 9 | 656 | 1 | 0 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 10 | 467 | 1 | 0 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 9 | 682 | 1 | 0 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 9 | 684 | 1 | 0 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 10 | 649 | 1 | 0 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 10 | 562 | 1 | 0 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 1313 | 0 | 0 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1036 | 0 | 1 | 5 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 974 | 0 | 1 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 6 | 450 | 0 | 0 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 6 | 452 | 0 | 1 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 7 | 463 | 0 | 0 | 3 | `runs/run-80b0aede01cce977/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 8 | 412 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 8 | 417 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 9 | 366 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 8 | 468 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 9 | 293 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 8 | 293 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1396 | 0 | 1 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 799 | 0 | 1 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 886 | 0 | 3 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 9 | 493 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 6 | 454 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 7 | 474 | 0 | 1 | 2 | `runs/run-80b0aede01cce977/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 8 | 730 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 8 | 530 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 8 | 540 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 8 | 557 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 9 | 548 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 8 | 550 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 960 | 0 | 1 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 983 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1048 | 1 | 0 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 6 | 512 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 6 | 733 | 0 | 0 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 6 | 649 | 0 | 3 | 2 | `runs/run-80b0aede01cce977/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 11 | 830 | 1 | 1 | 3 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 13 | 847 | 2 | 1 | 3 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 10 | 900 | 1 | 1 | 3 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 11 | 858 | 1 | 1 | 3 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 10 | 687 | 1 | 1 | 3 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 10 | 804 | 1 | 1 | 3 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 911 | 0 | 1 | 4 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1048 | 0 | 0 | 4 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1238 | 0 | 1 | 3 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 7 | 575 | 0 | 0 | 4 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 6 | 687 | 0 | 0 | 4 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 7 | 588 | 0 | 0 | 4 | `runs/run-80b0aede01cce977/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 31 | 752 | 0 | 0 | 156 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 16 | 827 | 0 | 0 | 156 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 20 | 731 | 0 | 0 | 152 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 19 | 861 | 0 | 0 | 156 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 22 | 770 | 0 | 0 | 156 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | no | max_turns | 50 | 839 | 0 | 0 | 74 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` (poo exited 11) |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 16 | 1348 | 0 | 6 | 156 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 18 | 1283 | 1 | 10 | 163 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 16 | 1326 | 0 | 5 | 156 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 12 | 816 | 0 | 1 | 156 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 15 | 801 | 1 | 2 | 156 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 12 | 711 | 0 | 1 | 156 | `runs/run-80b0aede01cce977/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 3 | 328 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 3 | 349 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 3 | 411 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 3 | 405 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 3 | 409 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 3 | 289 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1322 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 1287 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1567 | 0 | 1 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 3 | 594 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 6 | 908 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 3 | 930 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | no | provider_error | 1 | 0 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/gemma4/seed1/session.jsonl` (poo exited 20) |
| long-find-error-in-log | gemma4 | 2 | no | provider_error | 1 | 0 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/gemma4/seed2/session.jsonl` (poo exited 20) |
| long-find-error-in-log | gemma4 | 3 | no | provider_error | 1 | 0 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/gemma4/seed3/session.jsonl` (poo exited 20) |
| long-find-error-in-log | gemma4-think | 1 | no | provider_error | 1 | 0 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/gemma4-think/seed1/session.jsonl` (poo exited 20) |
| long-find-error-in-log | gemma4-think | 2 | no | provider_error | 1 | 0 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/gemma4-think/seed2/session.jsonl` (poo exited 20) |
| long-find-error-in-log | gemma4-think | 3 | no | provider_error | 1 | 0 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/gemma4-think/seed3/session.jsonl` (poo exited 20) |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 869 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 4 | 1018 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 3 | 993 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 4 | 459 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 4 | 641 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 3 | 406 | 0 | 0 | 0 | `runs/run-80b0aede01cce977/long-find-error-in-log/qwen/seed3/session.jsonl` |
