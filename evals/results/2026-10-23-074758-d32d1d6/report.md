# poo eval report

- started: 2026-10-23T07:47:58.776449154Z
- finished: 2026-10-23T08:11:50.025084722Z
- git sha: d32d1d6
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 95.0% | 0.0 | 11.9 | 39799 | 753 | 12132 | 574 | -14.68 | 8.0% | 0.0% | 13.0% | 10 | 91.8% |
| gemma4-think | 60 | 93.3% | 2.9 | 11.4 | 39403 | 912 | 14146 | 625 | -14.34 | 8.2% | 0.0% | 12.9% | 11 | 92.4% |
| minimax-m3.1-flash | 60 | 98.3% | 2.9 | 7.7 | 32978 | 3143 | 27762 | 1048 | -6.32 | 0.3% | 0.0% | 14.5% | 11 | 89.0% |
| qwen | 60 | 100.0% | 0.0 | 6.2 | 20550 | 423 | 6465 | 566 | -0.04 | 0.1% | 0.0% | 2.2% | 26 | n/a |
| **all** | 240 | 96.7% | 0.7 | 9.3 | 33182 | 1308 | 15126 | 703 | -8.85 | 4.2% | 0.0% | 10.6% | 14 | 91.1% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 100.0% | 0.0 | 8.0 | 0.01 | 28.6% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 8.0 | -1.83 | 28.6% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | 109.29 | 0.0% | 8.1% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 3.7 | -33.36 | 0.0% | 0.0% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 22.7 | 6.48 | 0.0% | 71.7% | 12 |
| find-off-by-one-loop | gemma4-think | 3 | 0.0% | 0.0 | 21.7 | 6.01 | 0.0% | 71.7% | 12 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 15.7 | 34.07 | 0.0% | 17.3% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 4.7 | -1.77 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -21.41 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -38.22 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | -23.86 | 0.0% | 4.8% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 3.7 | -4.70 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 7.0 | -10.73 | 16.7% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 7.0 | -30.51 | 16.7% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 11.0 | -1.36 | 0.0% | 15.1% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 6.3 | -4.37 | 0.0% | 4.2% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 6.3 | -13.62 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 6.3 | -10.96 | 0.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.0 | 92.40 | 0.0% | 14.3% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.3 | 11.97 | 0.0% | 0.0% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 100.0% | 0.0 | 12.0 | 11.01 | 13.7% | 27.8% | 4 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 12.7 | 8.33 | 17.0% | 30.6% | 3 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.3 | 16.18 | 0.0% | 15.3% | 7 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 6.3 | 60.81 | 0.0% | 13.3% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 10.7 | -12.84 | 13.1% | 0.0% | 4 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 10.3 | -16.40 | 13.7% | 0.0% | 4 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -64.81 | 0.0% | 18.6% | 4 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.0 | -12.51 | 0.0% | 8.3% | 4 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 8.7 | -25.27 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | -13.43 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | 27.91 | 0.0% | 26.5% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 6.3 | -64.28 | 0.0% | 0.0% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 9.3 | 11.35 | 14.3% | 0.0% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -8.54 | 14.3% | 0.0% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -114.50 | 0.0% | 11.1% | 3 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 6.3 | 5.93 | 0.0% | 0.0% | 4 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 10.3 | -31.18 | 12.5% | 0.0% | 2 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -3.71 | 13.1% | 0.0% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | 1.98 | 0.0% | 14.5% | 4 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 6.0 | 23.13 | 0.0% | 8.3% | 3 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 14.7 | -12.37 | 15.8% | 50.0% | 4 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 15.7 | -11.33 | 15.4% | 50.0% | 4 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | -29.06 | 0.0% | 20.4% | 4 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.0 | 35.89 | 0.0% | 0.0% | 4 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 39.7 | -3.69 | 0.0% | 76.1% | 11 |
| fix-path-join-double-slash | gemma4-think | 3 | 66.7% | 57.7 | 37.7 | 28.76 | 0.0% | 71.5% | 20 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.3 | 18.55 | 5.2% | 23.2% | 20 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 7.3 | 32.89 | 0.0% | 0.0% | 5 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -16.20 | 12.5% | 0.0% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -31.96 | 12.5% | 0.0% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 66.7% | 57.7 | 6.3 | -177.75 | 0.0% | 13.3% | 1 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 6.3 | 7.52 | 0.0% | 0.0% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -13.42 | 13.1% | 0.0% | 3 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -13.74 | 14.3% | 0.0% | 3 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.7 | -4.47 | 0.0% | 4.8% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 6.0 | 6.69 | 0.0% | 0.0% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 9.0 | -12.31 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | -15.86 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.3 | -23.26 | 0.0% | 5.6% | 2 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 7.0 | 15.40 | 0.0% | 0.0% | 319 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 8.0 | -10.14 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 8.7 | -27.41 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 22.75 | 0.0% | 21.4% | 2 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 6.7 | -101.27 | 0.0% | 0.0% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 14.0 | -27.68 | 18.2% | 33.3% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 13.7 | -27.03 | 18.2% | 33.3% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | 44.40 | 0.0% | 6.7% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.0 | 0.62 | 0.0% | 0.0% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 27.0 | 18.36 | 1.3% | 0.0% | 155 |
| long-change-timeout-everywhere | gemma4-think | 3 | 100.0% | 0.0 | 17.7 | 3.04 | 0.0% | 0.0% | 155 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 14.7 | 15.55 | 1.4% | 32.6% | 160 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 15.3 | -6.15 | 2.0% | 10.4% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -38.10 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | -52.44 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | -53.35 | 0.0% | 15.9% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 3.7 | 9.79 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -91.95 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -29.62 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 4.3 | -16.95 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 5.0 | 16.93 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 58 |
| gemma4 | max_turns | 2 |
| gemma4-think | end_turn | 58 |
| gemma4-think | max_turns | 2 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 8 | 450 | 2 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | yes | end_turn | 8 | 473 | 2 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 8 | 422 | 2 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 8 | 438 | 2 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 8 | 412 | 2 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 8 | 415 | 2 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1118 | 0 | 1 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 953 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 958 | 0 | 1 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 4 | 697 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 4 | 644 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 3 | 443 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 18 | 510 | 0 | 4 | 12 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 20 | 625 | 0 | 5 | 12 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 30 | 851 | 0 | 10 | 12 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | no | end_turn | 28 | 800 | 0 | 9 | 11 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | no | end_turn | 18 | 689 | 0 | 4 | 13 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | no | end_turn | 19 | 588 | 0 | 4 | 12 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 13 | 1189 | 0 | 2 | 0 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 17 | 1199 | 0 | 2 | 0 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 17 | 1066 | 0 | 1 | 0 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 3 | 414 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 4 | 421 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 7 | 450 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 271 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 301 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 237 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 238 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 267 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 269 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 894 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 916 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1065 | 0 | 1 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 3 | 414 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 5 | 444 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 3 | 402 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 7 | 304 | 1 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 7 | 249 | 1 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 7 | 292 | 1 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 7 | 303 | 1 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 7 | 375 | 1 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 7 | 426 | 1 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 11 | 848 | 0 | 3 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 12 | 768 | 0 | 2 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 1004 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 6 | 666 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 5 | 627 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 8 | 504 | 0 | 1 | 0 | `runs/run-5047c2d01500d3e2/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 9 | 316 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 5 | 282 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 5 | 252 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 5 | 253 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 5 | 255 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 9 | 329 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 1077 | 0 | 1 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1408 | 0 | 1 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 979 | 0 | 1 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 4 | 435 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 4 | 428 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 5 | 474 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 12 | 815 | 1 | 1 | 4 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 12 | 934 | 2 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | yes | end_turn | 12 | 690 | 1 | 1 | 4 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/gemma4/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 13 | 785 | 2 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 13 | 841 | 2 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 12 | 678 | 1 | 1 | 4 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 937 | 0 | 0 | 7 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 12 | 1047 | 0 | 2 | 7 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 1063 | 0 | 1 | 7 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 7 | 628 | 0 | 2 | 3 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 6 | 695 | 0 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 6 | 488 | 0 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 11 | 767 | 1 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 10 | 831 | 1 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 11 | 771 | 1 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 10 | 800 | 1 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 10 | 806 | 1 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 11 | 757 | 1 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 889 | 0 | 1 | 2 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 1430 | 0 | 1 | 6 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 963 | 0 | 1 | 4 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 6 | 537 | 0 | 1 | 5 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 6 | 554 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 6 | 613 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 10 | 487 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 8 | 502 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 8 | 455 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 10 | 423 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 9 | 420 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 8 | 400 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1074 | 0 | 1 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 948 | 0 | 3 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 1156 | 0 | 1 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 6 | 751 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 7 | 658 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 6 | 634 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 9 | 842 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 9 | 854 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 10 | 781 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 10 | 727 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 10 | 764 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 10 | 729 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 820 | 0 | 1 | 2 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 910 | 0 | 1 | 4 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1017 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 7 | 459 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 6 | 456 | 0 | 0 | 6 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 6 | 587 | 0 | 0 | 5 | `runs/run-5047c2d01500d3e2/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 11 | 641 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 10 | 658 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 10 | 592 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 10 | 635 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 10 | 572 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 10 | 605 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 998 | 0 | 1 | 4 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1147 | 0 | 1 | 5 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1105 | 0 | 1 | 4 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 6 | 484 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 6 | 487 | 0 | 1 | 4 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 485 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 15 | 490 | 2 | 2 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 15 | 674 | 2 | 2 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 14 | 591 | 2 | 2 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 16 | 571 | 2 | 2 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 16 | 584 | 2 | 2 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 15 | 571 | 2 | 2 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 965 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 827 | 0 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 11 | 1217 | 0 | 4 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 6 | 468 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 6 | 487 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 6 | 532 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | end_turn | 19 | 856 | 0 | 2 | 16 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | max_turns | 50 | 1804 | 0 | 10 | 8 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/gemma4/seed2/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4 | 3 | yes | max_turns | 50 | 847 | 0 | 39 | 10 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4-think | 1 | yes | max_turns | 50 | 2765 | 0 | 5 | 12 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4-think | 2 | no | max_turns | 50 | 2261 | 0 | 27 | 39 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` (poo exited 11) |
| fix-path-join-double-slash | gemma4-think | 3 | yes | end_turn | 13 | 1637 | 0 | 2 | 10 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 1058 | 1 | 2 | 21 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 1160 | 1 | 1 | 18 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 12 | 866 | 0 | 5 | 20 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 6 | 518 | 0 | 0 | 7 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 9 | 582 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 7 | 490 | 0 | 0 | 7 | `runs/run-5047c2d01500d3e2/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 10 | 435 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 10 | 557 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 10 | 430 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 10 | 602 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 9 | 571 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 11 | 836 | 1 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | no | end_turn | 3 | 1003 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1260 | 0 | 1 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | yes | end_turn | 10 | 1267 | 0 | 2 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 7 | 808 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 6 | 509 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 6 | 500 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 10 | 634 | 1 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 10 | 634 | 1 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 10 | 617 | 1 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 10 | 611 | 1 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 10 | 630 | 1 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 10 | 610 | 1 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 1092 | 0 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 926 | 0 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1138 | 0 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 6 | 640 | 0 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 6 | 727 | 0 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 6 | 780 | 0 | 0 | 3 | `runs/run-5047c2d01500d3e2/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 9 | 303 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 9 | 398 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 9 | 412 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 9 | 358 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 9 | 458 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 9 | 440 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 1039 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1133 | 0 | 1 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 828 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 8 | 476 | 0 | 0 | 952 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 6 | 457 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 7 | 471 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 8 | 507 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 8 | 462 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 8 | 430 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 9 | 403 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 8 | 408 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 9 | 348 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 984 | 0 | 1 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 833 | 0 | 2 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 963 | 0 | 1 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 8 | 469 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 6 | 1173 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 6 | 943 | 0 | 0 | 2 | `runs/run-5047c2d01500d3e2/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 14 | 853 | 2 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 14 | 884 | 2 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 14 | 945 | 2 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 13 | 1037 | 2 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 14 | 935 | 2 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 14 | 690 | 2 | 1 | 3 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1255 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1113 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 1146 | 0 | 1 | 4 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 6 | 651 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 6 | 628 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 6 | 562 | 0 | 0 | 4 | `runs/run-5047c2d01500d3e2/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 19 | 752 | 0 | 0 | 156 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 33 | 719 | 0 | 0 | 152 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 29 | 835 | 1 | 0 | 156 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 19 | 769 | 0 | 0 | 156 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 15 | 781 | 0 | 0 | 152 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | yes | end_turn | 19 | 778 | 0 | 0 | 156 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 14 | 1447 | 1 | 6 | 161 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 13 | 1026 | 0 | 4 | 156 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 17 | 1060 | 0 | 15 | 163 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 16 | 754 | 0 | 1 | 156 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 15 | 758 | 1 | 1 | 156 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 15 | 614 | 0 | 1 | 156 | `runs/run-5047c2d01500d3e2/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 3 | 317 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 3 | 260 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 3 | 261 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 3 | 263 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 3 | 261 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 3 | 260 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 1168 | 0 | 1 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 1184 | 0 | 2 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1291 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 3 | 581 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 4 | 580 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 4 | 514 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 3 | 385 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 3 | 335 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 3 | 336 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 4 | 472 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 4 | 303 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 4 | 271 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 699 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 4 | 857 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 1142 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 5 | 461 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 4 | 433 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 6 | 440 | 0 | 0 | 0 | `runs/run-5047c2d01500d3e2/long-find-error-in-log/qwen/seed3/session.jsonl` |
