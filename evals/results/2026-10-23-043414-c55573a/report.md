# poo eval report

- started: 2026-10-23T04:34:14.373631617Z
- finished: 2026-10-23T04:57:34.415209322Z
- git sha: c55573a
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 95.0% | 0.0 | 13.6 | 51505 | 656 | 16229 | 757 | -35.68 | 5.5% | 0.0% | 9.1% | 10 | 90.5% |
| gemma4-think | 60 | 95.0% | 0.0 | 14.9 | 54857 | 868 | 19240 | 833 | 8.31 | 5.1% | 0.0% | 7.8% | 10 | 91.2% |
| minimax-m3.1-flash | 60 | 100.0% | 0.0 | 7.4 | 29965 | 2151 | 18999 | 897 | -5.66 | 0.0% | 0.0% | 12.8% | 11 | 87.7% |
| qwen | 60 | 96.7% | 2.9 | 5.9 | 16431 | 388 | 5514 | 520 | -2.29 | 0.1% | 0.0% | 4.9% | 10 | n/a |
| **all** | 240 | 96.7% | 0.7 | 10.5 | 38189 | 1016 | 14995 | 752 | -8.83 | 2.7% | 0.0% | 8.7% | 10 | 89.8% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 100.0% | 0.0 | 7.3 | -21.62 | 20.6% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 7.7 | -8.33 | 24.6% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | -13.92 | 0.0% | 4.8% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 5.0 | -13.14 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 17.0 | 56.02 | 0.0% | 47.6% | 13 |
| find-off-by-one-loop | gemma4-think | 3 | 0.0% | 0.0 | 13.7 | 183.38 | 0.0% | 22.2% | 14 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.7 | 21.53 | 0.0% | 4.2% | 0 |
| find-off-by-one-loop | qwen | 3 | 66.7% | 57.7 | 6.7 | 8.37 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -511.17 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | 44.82 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -8.76 | 0.0% | 5.1% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 4.0 | 0.87 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 7.3 | -69.04 | 15.9% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 7.0 | 27.78 | 17.0% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | -46.91 | 0.0% | 9.7% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 4.3 | 19.53 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 6.3 | 21.23 | 4.2% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 7.3 | -22.16 | 4.8% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.3 | 28.03 | 0.0% | 13.7% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.3 | -44.48 | 0.0% | 22.2% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 32.7 | 6.34 | 1.3% | 19.4% | 3 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 38.3 | 0.81 | 2.4% | 30.6% | 3 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 21.14 | 0.0% | 18.1% | 6 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 6.3 | 35.61 | 0.0% | 8.3% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 23.7 | 5.99 | 9.0% | 0.0% | 3 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 20.0 | 30.95 | 7.1% | 0.0% | 4 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | -55.13 | 0.0% | 17.0% | 5 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.0 | -17.19 | 0.0% | 0.0% | 5 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 18.0 | -7.66 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 18.3 | -16.43 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -51.87 | 0.0% | 5.6% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 6.3 | 16.78 | 0.0% | 8.3% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 12.0 | 4.19 | 10.4% | 0.0% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 13.0 | 27.77 | 9.1% | 0.0% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 22.27 | 0.0% | 26.5% | 2 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 6.0 | -24.64 | 0.0% | 0.0% | 5 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 10.3 | -39.53 | 12.5% | 0.0% | 2 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -9.04 | 12.5% | 0.0% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 27.12 | 0.0% | 9.5% | 5 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 6.0 | 21.27 | 0.0% | 8.3% | 3 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 12.7 | -32.69 | 19.4% | 50.0% | 4 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 37.7 | -11.58 | 9.3% | 50.0% | 4 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | -38.15 | 0.0% | 11.4% | 4 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.7 | 20.88 | 0.0% | 0.0% | 3 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 39.0 | -29.52 | 0.0% | 65.2% | 12 |
| fix-path-join-double-slash | gemma4-think | 3 | 100.0% | 0.0 | 46.0 | -1.65 | 0.0% | 53.3% | 12 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 5.83 | 0.0% | 19.0% | 19 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 7.0 | -15.31 | 0.0% | 0.0% | 3 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 8.3 | 41.61 | 0.0% | 0.0% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 8.3 | -3.64 | 0.0% | 0.0% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 0.94 | 0.0% | 15.1% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 6.0 | 8.30 | 0.0% | 0.0% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 9.0 | -53.13 | 13.7% | 0.0% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 8.7 | 16.91 | 14.3% | 0.0% | 3 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 6.97 | 0.0% | 15.9% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 6.7 | -37.45 | 0.0% | 21.7% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 8.3 | -13.31 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 8.0 | -48.33 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | -60.78 | 0.0% | 9.5% | 3 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 6.3 | 14.62 | 0.0% | 25.0% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 8.7 | -5.67 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 8.7 | -14.60 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 27.98 | 0.0% | 18.4% | 3 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 6.7 | 16.15 | 0.0% | 0.0% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 18.0 | 21.09 | 0.0% | 0.0% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 19.7 | 58.71 | 0.0% | 0.0% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | -0.14 | 0.0% | 19.4% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.0 | 17.37 | 0.0% | 0.0% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 22.7 | -1.60 | 2.1% | 0.0% | 153 |
| long-change-timeout-everywhere | gemma4-think | 3 | 100.0% | 0.0 | 14.7 | 16.78 | 0.0% | 0.0% | 155 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 15.0 | 18.64 | 0.0% | 27.0% | 161 |
| long-change-timeout-everywhere | qwen | 3 | 66.7% | 57.7 | 10.3 | -16.67 | 2.8% | 4.8% | 157 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -34.89 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | -41.82 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | -23.69 | 0.0% | 5.6% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 4.0 | -40.27 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -50.19 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -64.13 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 4.0 | 5.67 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 3.0 | -16.47 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 56 |
| gemma4 | max_turns | 4 |
| gemma4-think | end_turn | 55 |
| gemma4-think | max_turns | 5 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 8 | 527 | 2 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | yes | end_turn | 7 | 457 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 7 | 499 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 8 | 484 | 2 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 8 | 600 | 2 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 7 | 388 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1079 | 0 | 1 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1052 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 908 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 5 | 482 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 7 | 645 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 3 | 473 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 7 | 787 | 0 | 0 | 16 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 24 | 947 | 0 | 5 | 11 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 20 | 744 | 0 | 5 | 12 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | no | end_turn | 27 | 1540 | 0 | 6 | 12 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | no | end_turn | 7 | 1899 | 0 | 0 | 14 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | no | end_turn | 7 | 589 | 0 | 0 | 16 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1326 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 15 | 965 | 0 | 1 | 0 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 11 | 827 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 6 | 475 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | no | end_turn | 7 | 581 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 7 | 516 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 1148 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 542 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 914 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 1032 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 907 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 1245 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 819 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 692 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 859 | 0 | 2 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 3 | 407 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 3 | 393 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 6 | 443 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 7 | 730 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 8 | 694 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 7 | 380 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 6 | 1806 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 8 | 1242 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 7 | 769 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 1071 | 0 | 2 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1004 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 851 | 0 | 1 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 6 | 468 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 4 | 418 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 3 | 428 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 9 | 437 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 5 | 458 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 5 | 538 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 8 | 407 | 1 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 9 | 422 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 5 | 416 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 716 | 0 | 1 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 758 | 0 | 1 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 1014 | 0 | 1 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 3 | 418 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 4 | 684 | 0 | 2 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 6 | 514 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 25 | 639 | 0 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | max_turns | 50 | 677 | 2 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/gemma4/seed2/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4 | 3 | yes | end_turn | 23 | 632 | 0 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/gemma4/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 1 | yes | max_turns | 50 | 654 | 2 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 32 | 630 | 0 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 33 | 654 | 1 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1023 | 0 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 783 | 0 | 1 | 7 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 749 | 0 | 1 | 7 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 5 | 521 | 0 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 7 | 523 | 0 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 7 | 675 | 0 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | max_turns | 50 | 693 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/gemma4/seed1/session.jsonl` (poo exited 11) |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 10 | 984 | 1 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 11 | 765 | 1 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 26 | 686 | 1 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 10 | 952 | 1 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 24 | 580 | 1 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1002 | 0 | 1 | 5 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 932 | 0 | 1 | 6 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 878 | 0 | 1 | 5 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 6 | 507 | 0 | 0 | 5 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 6 | 675 | 0 | 0 | 5 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 6 | 461 | 0 | 0 | 5 | `runs/run-da0c12f5a40e2d8f/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 18 | 561 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 18 | 544 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 18 | 529 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 19 | 1007 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 18 | 1410 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 18 | 1549 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 873 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1081 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 861 | 0 | 1 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 6 | 468 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 6 | 453 | 0 | 1 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 7 | 488 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 12 | 418 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 11 | 433 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 13 | 548 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 13 | 697 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 13 | 653 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 13 | 590 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 917 | 0 | 3 | 2 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 858 | 0 | 1 | 2 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 879 | 0 | 1 | 2 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 7 | 583 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 5 | 449 | 0 | 0 | 6 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 6 | 618 | 0 | 0 | 6 | `runs/run-da0c12f5a40e2d8f/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 10 | 523 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 10 | 648 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 11 | 617 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 10 | 524 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 10 | 594 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 10 | 413 | 1 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 734 | 0 | 0 | 5 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 650 | 0 | 1 | 4 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 1158 | 0 | 1 | 5 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 6 | 484 | 0 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 6 | 478 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 481 | 0 | 1 | 2 | `runs/run-da0c12f5a40e2d8f/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 13 | 808 | 2 | 2 | 4 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 12 | 796 | 2 | 2 | 4 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 13 | 816 | 2 | 2 | 4 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | max_turns | 50 | 770 | 2 | 2 | 4 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` (poo exited 11) |
| fix-off-by-one-slice | gemma4-think | 2 | yes | max_turns | 50 | 748 | 2 | 2 | 4 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` (poo exited 11) |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 13 | 606 | 2 | 2 | 4 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 881 | 0 | 1 | 4 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1058 | 0 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 984 | 0 | 1 | 5 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 6 | 597 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 7 | 470 | 0 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 7 | 481 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | max_turns | 50 | 734 | 0 | 43 | 17 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/gemma4/seed1/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4 | 2 | yes | max_turns | 50 | 756 | 0 | 2 | 9 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/gemma4/seed2/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4 | 3 | yes | end_turn | 17 | 1042 | 0 | 2 | 10 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/gemma4/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 1 | yes | end_turn | 38 | 835 | 0 | 1 | 10 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | yes | max_turns | 50 | 888 | 0 | 3 | 11 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4-think | 3 | yes | max_turns | 50 | 2644 | 0 | 4 | 15 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 881 | 0 | 1 | 22 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 890 | 0 | 2 | 23 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 964 | 0 | 2 | 13 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 8 | 547 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 6 | 502 | 0 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 7 | 799 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 8 | 1449 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 8 | 669 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 9 | 638 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 9 | 623 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 8 | 821 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 8 | 696 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 860 | 0 | 1 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1039 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1046 | 0 | 2 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 6 | 591 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 6 | 516 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 6 | 515 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 9 | 587 | 1 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 8 | 574 | 1 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 10 | 1204 | 1 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 8 | 650 | 1 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 10 | 1448 | 1 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 8 | 574 | 1 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 828 | 0 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 719 | 0 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1100 | 0 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 7 | 527 | 0 | 1 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 6 | 457 | 0 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 7 | 714 | 0 | 2 | 3 | `runs/run-da0c12f5a40e2d8f/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 9 | 391 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 8 | 409 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 8 | 400 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 8 | 720 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 8 | 363 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 8 | 430 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 758 | 0 | 1 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 914 | 0 | 1 | 4 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 973 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 6 | 456 | 0 | 2 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 8 | 479 | 0 | 1 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 5 | 444 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 9 | 537 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 8 | 774 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 9 | 668 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 9 | 562 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 8 | 580 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 9 | 586 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 881 | 0 | 1 | 4 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 767 | 0 | 2 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 849 | 0 | 1 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 7 | 466 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 7 | 456 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 6 | 456 | 0 | 0 | 2 | `runs/run-da0c12f5a40e2d8f/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 38 | 1308 | 0 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 8 | 2019 | 0 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 8 | 1895 | 0 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 9 | 1715 | 0 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 41 | 1469 | 0 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 9 | 2701 | 0 | 0 | 3 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 717 | 0 | 1 | 5 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 806 | 0 | 1 | 4 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 819 | 0 | 1 | 4 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 6 | 471 | 0 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 6 | 471 | 0 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 6 | 470 | 0 | 0 | 4 | `runs/run-da0c12f5a40e2d8f/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 17 | 1770 | 0 | 0 | 152 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 19 | 2439 | 1 | 0 | 156 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 32 | 1982 | 0 | 0 | 152 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 13 | 959 | 0 | 0 | 156 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 16 | 771 | 0 | 0 | 156 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | yes | end_turn | 15 | 613 | 0 | 0 | 152 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 13 | 1096 | 0 | 4 | 163 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 15 | 1057 | 0 | 4 | 156 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 17 | 964 | 0 | 7 | 163 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 11 | 882 | 0 | 0 | 156 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 11 | 634 | 1 | 1 | 156 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | no | end_turn | 9 | 877 | 0 | 0 | 160 | `runs/run-da0c12f5a40e2d8f/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 3 | 306 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 3 | 268 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 3 | 260 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 3 | 320 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 3 | 261 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 3 | 261 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 845 | 0 | 1 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 905 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 3 | 785 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 3 | 445 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 6 | 598 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 3 | 427 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 4 | 353 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 4 | 268 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 4 | 294 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 4 | 302 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 4 | 389 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 4 | 365 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 718 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 4 | 704 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 4 | 711 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 3 | 406 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 2 | 392 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 4 | 425 | 0 | 0 | 0 | `runs/run-da0c12f5a40e2d8f/long-find-error-in-log/qwen/seed3/session.jsonl` |
