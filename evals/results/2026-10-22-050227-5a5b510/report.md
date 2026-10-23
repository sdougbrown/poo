# poo eval report

- started: 2026-10-22T05:02:27.295883434Z
- finished: 2026-10-22T05:42:33.847575747Z
- git sha: 5a5b510
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 91.7% | 2.9 | 11.3 | 23259 | 571 | 10655 | 558 | 3.02 | 2.2% | 0.0% | 25.3% | 10 | 81.3% |
| gemma4-think | 60 | 86.7% | 5.8 | 9.9 | 22115 | 1425 | 18607 | 265 | 13.31 | 1.2% | 0.0% | 19.0% | 10 | 79.0% |
| minimax-m3.1-flash | 60 | 91.7% | 2.9 | 8.0 | 24647 | 2772 | 33040 | 1883 | -90.79 | 6.2% | 0.0% | 16.0% | 11 | 67.1% |
| qwen | 60 | 100.0% | 0.0 | 5.7 | 10568 | 689 | 43290 | 3452 | 136.75 | 0.6% | 0.0% | 14.1% | 10 | n/a |
| **all** | 240 | 92.5% | 2.2 | 8.7 | 20147 | 1364 | 26398 | 1539 | 15.57 | 2.5% | 0.0% | 18.6% | 10 | 75.8% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 66.7% | 57.7 | 4.3 | -39.36 | 5.6% | 11.1% | 0 |
| find-dead-code | gemma4-think | 3 | 33.3% | 57.7 | 3.7 | 34.69 | 0.0% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | 5.60 | 0.0% | 10.0% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 6.0 | -33.74 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 4.0 | 167.92 | 0.0% | 0.0% | 14 |
| find-off-by-one-loop | gemma4-think | 3 | 33.3% | 57.7 | 4.7 | 71.45 | 0.0% | 0.0% | 9 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.0 | 1.99 | 0.0% | 3.7% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 5.0 | -232.69 | 5.6% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -32.89 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | 8.76 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.3 | 173.64 | 0.0% | 8.9% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 4.3 | 39.36 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 5.0 | -46.54 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | 34.05 | 0.0% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 33.3% | 57.7 | 8.0 | -442.63 | 0.0% | 10.4% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 4.0 | 1294.29 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 7.3 | -1.58 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 66.7% | 57.7 | 3.7 | 4.70 | 0.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 33.3% | 57.7 | 4.3 | -1312.19 | 0.0% | 9.5% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.3 | -42.11 | 0.0% | 0.0% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 12.0 | 85.90 | 9.1% | 20.0% | 4 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 22.7 | 6.49 | 13.7% | 53.4% | 13 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | 139.15 | 9.4% | 29.0% | 7 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 7.0 | -105.37 | 5.6% | 41.1% | 4 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 8.0 | -7.12 | 0.0% | 0.0% | 2 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 10.7 | 6.68 | 0.0% | 35.6% | 2 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | -397.88 | 10.7% | 9.3% | 3 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.3 | 252.19 | 0.0% | 53.3% | 3 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 9.0 | -7.14 | 0.0% | 33.3% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 11.3 | 13.57 | 0.0% | 16.7% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 77.85 | 10.4% | 17.8% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 5.3 | 2142.80 | 0.0% | 11.1% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 13.0 | 3.16 | 0.0% | 23.3% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 12.3 | 1.00 | 2.6% | 27.8% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 66.12 | 12.5% | 14.0% | 4 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 5.7 | -3418.09 | 0.0% | 19.4% | 2 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 13.0 | 3.61 | 2.1% | 30.6% | 5 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 10.7 | 6.65 | 0.0% | 19.4% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 12.18 | 11.2% | 10.3% | 4 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 6.3 | 808.21 | 0.0% | 19.4% | 4 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 13.0 | -0.41 | 0.0% | 60.5% | 2 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 13.3 | 4.10 | 5.9% | 45.0% | 2 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.3 | -23.25 | 7.0% | 22.6% | 5 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.7 | 1063.88 | 0.0% | 42.2% | 5 |
| fix-path-join-double-slash | gemma4 | 3 | 66.7% | 57.7 | 24.7 | 29.43 | 0.0% | 44.2% | 6 |
| fix-path-join-double-slash | gemma4-think | 3 | 100.0% | 0.0 | 16.0 | 6.87 | 2.8% | 41.7% | 6 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 66.7% | 57.7 | 11.0 | -143.85 | 5.6% | 23.0% | 25 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 5.7 | -48.22 | 0.0% | 8.3% | 6 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 14.0 | 6.18 | 0.0% | 25.0% | 3 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 12.7 | 5.08 | 0.0% | 8.3% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | 60.41 | 11.2% | 14.8% | 2 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 4.7 | 47.69 | 0.0% | 0.0% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 16.0 | 0.14 | 0.0% | 60.7% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 33.3% | 57.7 | 7.3 | 8.33 | 0.0% | 13.3% | 1 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -26.79 | 11.6% | 15.4% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 7.0 | 48.59 | 0.0% | 22.5% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 13.7 | 13.69 | 1.8% | 41.3% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 66.7% | 57.7 | 9.0 | 1.10 | 0.0% | 11.1% | 1 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -4.64 | 11.1% | 17.5% | 2 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 6.0 | 54.01 | 0.0% | 8.3% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 13.7 | -4.96 | 0.0% | 60.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 9.7 | 9.52 | 0.0% | 11.1% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | -1.50 | 10.4% | 12.7% | 2 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 5.3 | 37.62 | 0.0% | 19.4% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 20.0 | 6.96 | 5.3% | 63.5% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 12.7 | 5.62 | 0.0% | 43.3% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | 25.85 | 12.0% | 15.1% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.3 | 5.83 | 0.0% | 19.4% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 20.7 | 6.92 | 3.2% | 0.0% | 152 |
| long-change-timeout-everywhere | gemma4-think | 3 | 100.0% | 0.0 | 23.0 | -3.94 | 0.0% | 52.8% | 153 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 16.7 | -11.13 | 1.2% | 33.0% | 156 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 11.7 | 16.60 | 0.0% | 17.2% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 7.0 | -40.95 | 16.7% | 33.3% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | -42.32 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | 31.29 | 0.0% | 22.2% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 4.7 | 34.99 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -82.56 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | 83.73 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 4.3 | -45.99 | 0.0% | 20.8% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 2.3 | 769.16 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 59 |
| gemma4 | max_turns | 1 |
| gemma4-think | end_turn | 58 |
| gemma4-think | output_truncated | 2 |
| minimax-m3.1-flash | end_turn | 55 |
| minimax-m3.1-flash | output_truncated | 1 |
| minimax-m3.1-flash | provider_error | 4 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 7 | 566 | 1 | 1 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 5 | 409 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | no | end_turn | 3 | 254 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | no | end_turn | 3 | 222 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 5 | 324 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 938 | 0 | 3 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 974 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 852 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 6 | 578 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 6 | 854 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 6 | 336 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 1 | 199 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 6 | 935 | 0 | 0 | 24 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 5 | 775 | 0 | 0 | 17 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | yes | end_turn | 4 | 278 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | no | end_turn | 5 | 331 | 0 | 0 | 14 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | no | end_turn | 5 | 322 | 0 | 0 | 13 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 1056 | 0 | 1 | 0 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 1025 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 852 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 7 | 454 | 1 | 0 | 0 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 3 | 754 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 5 | 330 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 285 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 235 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 232 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 192 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 180 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 143 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1502 | 0 | 1 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 2815 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 2194 | 0 | 1 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 5 | 364 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 4 | 316 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 4 | 292 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 5 | 428 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 5 | 418 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 5 | 319 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 4 | 345 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 4 | 313 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 4 | 238 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | no | provider_error | 12 | 4780 | 0 | 2 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` (poo exited 20) |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 5681 | 0 | 1 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | no | provider_error | 3 | 4419 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` (poo exited 20) |
| find-slow-loop | qwen | 1 | yes | end_turn | 5 | 1072 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 3 | 3422 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 4 | 1726 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 5 | 279 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 9 | 322 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 8 | 295 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 5 | 184 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | no | end_turn | 1 | 148 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 5 | 157 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 4472 | 0 | 1 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | no | provider_error | 6 | 4734 | 0 | 1 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` (poo exited 20) |
| find-version-mismatch | minimax-m3.1-flash | 3 | no | provider_error | 2 | 4325 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` (poo exited 20) |
| find-version-mismatch | qwen | 1 | yes | end_turn | 4 | 310 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 5 | 523 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 4 | 441 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 12 | 2363 | 1 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 12 | 2314 | 1 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | yes | end_turn | 12 | 2487 | 1 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/gemma4/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 36 | 362 | 1 | 23 | 20 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 16 | 297 | 4 | 1 | 17 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 16 | 373 | 2 | 3 | 3 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 4284 | 1 | 2 | 8 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 3821 | 1 | 4 | 7 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 3438 | 1 | 2 | 7 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 8 | 415 | 0 | 2 | 3 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 7 | 1289 | 0 | 2 | 5 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 6 | 1268 | 1 | 1 | 3 | `runs/run-eadc582d1161be4a/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 8 | 444 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 8 | 334 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 8 | 360 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 10 | 270 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 13 | 290 | 0 | 2 | 2 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 9 | 309 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 4754 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 4598 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 3165 | 1 | 0 | 4 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 4 | 1043 | 0 | 2 | 2 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 8 | 1995 | 0 | 2 | 2 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 7 | 1433 | 0 | 3 | 4 | `runs/run-eadc582d1161be4a/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 9 | 402 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 9 | 321 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 9 | 359 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 14 | 309 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 11 | 250 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 9 | 283 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 2349 | 1 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 2845 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1607 | 1 | 2 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 5 | 2785 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 6 | 11655 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 5 | 12732 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 15 | 430 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 12 | 458 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 12 | 451 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 14 | 298 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 12 | 292 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 11 | 270 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 993 | 1 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1083 | 1 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 849 | 1 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 7 | 21586 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 5 | 34083 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 5 | 13944 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 12 | 402 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 11 | 343 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 16 | 363 | 1 | 2 | 11 | `runs/run-eadc582d1161be4a/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 11 | 252 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 11 | 259 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 10 | 202 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1060 | 1 | 0 | 5 | `runs/run-eadc582d1161be4a/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 877 | 1 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 902 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 6 | 15110 | 0 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 6 | 16575 | 0 | 0 | 5 | `runs/run-eadc582d1161be4a/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 7 | 21819 | 0 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 12 | 448 | 0 | 2 | 2 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 15 | 348 | 0 | 5 | 2 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 12 | 348 | 0 | 3 | 2 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 14 | 263 | 1 | 3 | 2 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 15 | 258 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | output_truncated | 11 | 174 | 1 | 2 | 2 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` (poo exited 10) |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 10 | 1202 | 1 | 2 | 4 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 1202 | 1 | 3 | 4 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 1121 | 1 | 2 | 6 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 8 | 12141 | 0 | 2 | 6 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 6 | 11752 | 0 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 6 | 523 | 0 | 3 | 4 | `runs/run-eadc582d1161be4a/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | no | end_turn | 1 | 0 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | max_turns | 50 | 605 | 0 | 3 | 2 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/gemma4/seed2/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4 | 3 | yes | end_turn | 23 | 1748 | 0 | 8 | 16 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/gemma4/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 1 | yes | output_truncated | 14 | 321 | 0 | 3 | 2 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` (poo exited 10) |
| fix-path-join-double-slash | gemma4-think | 2 | yes | end_turn | 21 | 325 | 0 | 1 | 7 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 3 | yes | end_turn | 13 | 206 | 1 | 2 | 8 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1724 | 1 | 1 | 13 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | no | output_truncated | 6 | 4013 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` (poo exited 10) |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 20 | 1400 | 1 | 11 | 62 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 6 | 406 | 0 | 1 | 11 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 6 | 725 | 0 | 0 | 6 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 5 | 1009 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 17 | 386 | 0 | 2 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 15 | 521 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 10 | 723 | 0 | 0 | 4 | `runs/run-eadc582d1161be4a/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 13 | 333 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 13 | 287 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 12 | 204 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 1105 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1052 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1033 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 5 | 347 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 5 | 338 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 4 | 351 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 15 | 470 | 0 | 4 | 3 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 16 | 578 | 0 | 5 | 3 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 17 | 569 | 0 | 5 | 3 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | no | end_turn | 5 | 266 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | no | end_turn | 5 | 225 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 12 | 215 | 0 | 2 | 3 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1098 | 1 | 1 | 3 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1032 | 1 | 1 | 3 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 959 | 1 | 2 | 3 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 5 | 331 | 0 | 1 | 3 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 9 | 399 | 0 | 2 | 3 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 7 | 396 | 0 | 1 | 3 | `runs/run-eadc582d1161be4a/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 13 | 574 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 17 | 528 | 1 | 4 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 11 | 454 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 11 | 317 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 11 | 343 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | no | end_turn | 5 | 180 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 983 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 915 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 823 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 7 | 415 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 5 | 394 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 6 | 370 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 14 | 391 | 0 | 3 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 14 | 406 | 0 | 3 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 13 | 327 | 0 | 3 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 11 | 263 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 8 | 175 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 10 | 165 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 864 | 1 | 2 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 862 | 1 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 941 | 1 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 5 | 339 | 0 | 0 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 5 | 370 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 6 | 343 | 0 | 1 | 2 | `runs/run-eadc582d1161be4a/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 19 | 551 | 1 | 4 | 3 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 20 | 710 | 1 | 6 | 3 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 21 | 653 | 1 | 6 | 3 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 16 | 359 | 0 | 4 | 3 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 11 | 320 | 0 | 2 | 3 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 11 | 220 | 0 | 2 | 3 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1010 | 1 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 939 | 1 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 886 | 1 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 4 | 451 | 0 | 0 | 4 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 8 | 430 | 0 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 7 | 406 | 0 | 1 | 4 | `runs/run-eadc582d1161be4a/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 18 | 821 | 0 | 0 | 152 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 22 | 683 | 2 | 0 | 152 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 22 | 568 | 0 | 0 | 152 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 13 | 386 | 0 | 1 | 152 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 17 | 376 | 0 | 2 | 152 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | yes | end_turn | 39 | 377 | 0 | 3 | 156 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 15 | 1801 | 0 | 8 | 156 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 19 | 1472 | 1 | 9 | 156 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 16 | 1305 | 0 | 5 | 156 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 15 | 569 | 0 | 0 | 156 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 10 | 586 | 0 | 2 | 156 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 10 | 545 | 0 | 3 | 156 | `runs/run-eadc582d1161be4a/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 7 | 444 | 1 | 1 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 7 | 517 | 1 | 1 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 7 | 329 | 1 | 1 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 5 | 253 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 2 | 270 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 2 | 163 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 979 | 0 | 2 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1312 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 12 | 899 | 0 | 4 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 5 | 527 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 5 | 523 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 4 | 622 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 3 | 466 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 3 | 337 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 3 | 435 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 3 | 332 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 3 | 156 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 3 | 213 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 1023 | 0 | 3 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 4 | 892 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 900 | 0 | 1 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 3 | 295 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 2 | 290 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 2 | 1419 | 0 | 0 | 0 | `runs/run-eadc582d1161be4a/long-find-error-in-log/qwen/seed3/session.jsonl` |
