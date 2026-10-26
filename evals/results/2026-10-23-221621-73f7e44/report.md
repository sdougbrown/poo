# poo eval report

- started: 2026-10-23T22:16:21.091866499Z
- finished: 2026-10-23T22:34:39.316063719Z
- git sha: 73f7e44
- runs: 240

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gemma4 | 60 | 93.3% | 2.9 | 11.0 | 36544 | 652 | 11888 | 654 | -44.10 | 7.3% | 0.0% | 10.5% | 10 | 90.6% |
| gemma4-think | 60 | 95.0% | 0.0 | 10.2 | 30049 | 653 | 11604 | 670 | -29.99 | 7.3% | 0.0% | 11.4% | 10 | 91.3% |
| minimax-m3.1-flash | 60 | 98.3% | 2.9 | 7.5 | 31269 | 2087 | 16945 | 799 | -3.87 | 0.4% | 0.0% | 15.1% | 11 | 88.9% |
| qwen | 60 | 100.0% | 0.0 | 6.1 | 17328 | 409 | 9198 | 682 | -21.01 | 0.2% | 0.0% | 5.0% | 10 | n/a |
| **all** | 240 | 96.7% | 1.4 | 8.7 | 28798 | 950 | 12409 | 701 | -24.74 | 3.8% | 0.0% | 10.5% | 10 | 90.3% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 3 | 100.0% | 0.0 | 8.0 | -20.07 | 28.6% | 0.0% | 0 |
| find-dead-code | gemma4-think | 3 | 100.0% | 0.0 | 8.0 | -16.06 | 28.6% | 0.0% | 0 |
| find-dead-code | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | -15.18 | 0.0% | 8.9% | 0 |
| find-dead-code | qwen | 3 | 100.0% | 0.0 | 5.0 | -114.43 | 0.0% | 6.7% | 0 |
| find-off-by-one-loop | gemma4 | 3 | 0.0% | 0.0 | 19.3 | 4.37 | 0.0% | 69.8% | 13 |
| find-off-by-one-loop | gemma4-think | 3 | 0.0% | 0.0 | 18.0 | 4.68 | 0.0% | 66.7% | 13 |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | 0.37 | 0.0% | 7.9% | 0 |
| find-off-by-one-loop | qwen | 3 | 100.0% | 0.0 | 8.7 | 47.18 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -24.12 | 0.0% | 0.0% | 0 |
| find-secret-in-config | gemma4-think | 3 | 100.0% | 0.0 | 4.0 | -49.38 | 0.0% | 0.0% | 0 |
| find-secret-in-config | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.7 | 21.71 | 0.0% | 11.1% | 0 |
| find-secret-in-config | qwen | 3 | 100.0% | 0.0 | 4.3 | 10.58 | 0.0% | 0.0% | 0 |
| find-slow-loop | gemma4 | 3 | 100.0% | 0.0 | 7.0 | -4.61 | 16.7% | 0.0% | 0 |
| find-slow-loop | gemma4-think | 3 | 100.0% | 0.0 | 7.0 | -35.21 | 16.7% | 0.0% | 0 |
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 10.3 | -24.55 | 0.0% | 8.5% | 0 |
| find-slow-loop | qwen | 3 | 100.0% | 0.0 | 5.7 | 5.15 | 4.2% | 0.0% | 0 |
| find-version-mismatch | gemma4 | 3 | 100.0% | 0.0 | 6.3 | 10.71 | 0.0% | 0.0% | 0 |
| find-version-mismatch | gemma4-think | 3 | 100.0% | 0.0 | 7.7 | -201.07 | 0.0% | 0.0% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.3 | -43.96 | 0.0% | 8.9% | 0 |
| find-version-mismatch | qwen | 3 | 100.0% | 0.0 | 4.3 | -55.49 | 0.0% | 8.3% | 0 |
| fix-cache-never-expires | gemma4 | 3 | 66.7% | 57.7 | 25.3 | -6.87 | 13.4% | 19.4% | 2 |
| fix-cache-never-expires | gemma4-think | 3 | 100.0% | 0.0 | 12.3 | 14.58 | 17.0% | 27.8% | 3 |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.0 | 18.65 | 0.0% | 15.9% | 4 |
| fix-cache-never-expires | qwen | 3 | 100.0% | 0.0 | 6.3 | -15.75 | 0.0% | 8.3% | 3 |
| fix-csv-skips-header | gemma4 | 3 | 100.0% | 0.0 | 10.3 | -6.59 | 13.1% | 0.0% | 4 |
| fix-csv-skips-header | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -21.78 | 13.1% | 0.0% | 4 |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.3 | 61.40 | 0.0% | 15.3% | 5 |
| fix-csv-skips-header | qwen | 3 | 100.0% | 0.0 | 6.0 | -42.40 | 0.0% | 0.0% | 3 |
| fix-date-format-pad | gemma4 | 3 | 100.0% | 0.0 | 9.7 | -15.72 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | gemma4-think | 3 | 100.0% | 0.0 | 9.0 | -20.63 | 0.0% | 0.0% | 2 |
| fix-date-format-pad | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -2.41 | 0.0% | 16.7% | 2 |
| fix-date-format-pad | qwen | 3 | 100.0% | 0.0 | 5.7 | -21.86 | 0.0% | 0.0% | 2 |
| fix-division-by-zero | gemma4 | 3 | 100.0% | 0.0 | 9.7 | 14.18 | 14.3% | 0.0% | 2 |
| fix-division-by-zero | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -12.52 | 14.3% | 0.0% | 2 |
| fix-division-by-zero | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | -4.91 | 0.0% | 19.4% | 3 |
| fix-division-by-zero | qwen | 3 | 100.0% | 0.0 | 6.7 | -30.87 | 0.0% | 0.0% | 4 |
| fix-json-missing-key | gemma4 | 3 | 100.0% | 0.0 | 9.7 | -17.58 | 12.5% | 0.0% | 2 |
| fix-json-missing-key | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -35.25 | 13.7% | 0.0% | 2 |
| fix-json-missing-key | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | -8.97 | 0.0% | 15.1% | 3 |
| fix-json-missing-key | qwen | 3 | 100.0% | 0.0 | 6.7 | -21.12 | 0.0% | 8.3% | 3 |
| fix-off-by-one-slice | gemma4 | 3 | 100.0% | 0.0 | 15.7 | -12.60 | 15.0% | 50.0% | 4 |
| fix-off-by-one-slice | gemma4-think | 3 | 100.0% | 0.0 | 14.7 | -14.41 | 17.3% | 50.0% | 4 |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.0 | -20.38 | 0.0% | 27.2% | 12 |
| fix-off-by-one-slice | qwen | 3 | 100.0% | 0.0 | 6.7 | -24.82 | 0.0% | 6.7% | 3 |
| fix-path-join-double-slash | gemma4 | 3 | 100.0% | 0.0 | 19.3 | -7.56 | 0.0% | 60.0% | 11 |
| fix-path-join-double-slash | gemma4-think | 3 | 100.0% | 0.0 | 20.3 | -52.34 | 0.0% | 58.9% | 12 |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 9.0 | 16.68 | 3.0% | 17.2% | 17 |
| fix-path-join-double-slash | qwen | 3 | 100.0% | 0.0 | 8.0 | -0.26 | 0.0% | 13.9% | 5 |
| fix-retry-backoff | gemma4 | 3 | 100.0% | 0.0 | 10.0 | -12.10 | 12.5% | 0.0% | 2 |
| fix-retry-backoff | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | -29.23 | 12.5% | 0.0% | 2 |
| fix-retry-backoff | minimax-m3.1-flash | 3 | 66.7% | 57.7 | 6.0 | -29.36 | 0.0% | 9.5% | 1 |
| fix-retry-backoff | qwen | 3 | 100.0% | 0.0 | 6.7 | -185.60 | 0.0% | 0.0% | 2 |
| fix-sort-mutates-input | gemma4 | 3 | 100.0% | 0.0 | 9.7 | -5.37 | 13.7% | 0.0% | 4 |
| fix-sort-mutates-input | gemma4-think | 3 | 100.0% | 0.0 | 10.0 | 20.82 | 12.5% | 0.0% | 3 |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.0 | -4.79 | 0.0% | 13.3% | 3 |
| fix-sort-mutates-input | qwen | 3 | 100.0% | 0.0 | 6.3 | 11.48 | 0.0% | 8.3% | 3 |
| fix-stack-push-order | gemma4 | 3 | 100.0% | 0.0 | 8.7 | 2.31 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | gemma4-think | 3 | 100.0% | 0.0 | 8.3 | -40.71 | 0.0% | 0.0% | 2 |
| fix-stack-push-order | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 5.7 | 47.43 | 0.0% | 10.3% | 3 |
| fix-stack-push-order | qwen | 3 | 100.0% | 0.0 | 6.0 | 29.08 | 0.0% | 19.4% | 2 |
| fix-unicode-decode-error | gemma4 | 3 | 100.0% | 0.0 | 8.7 | -18.58 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | gemma4-think | 3 | 100.0% | 0.0 | 8.0 | -81.17 | 0.0% | 0.0% | 2 |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.3 | -0.19 | 0.0% | 21.3% | 3 |
| fix-unicode-decode-error | qwen | 3 | 100.0% | 0.0 | 6.0 | 14.89 | 0.0% | 0.0% | 2 |
| fix-word-count-ignores-case | gemma4 | 3 | 100.0% | 0.0 | 10.3 | -72.32 | 6.1% | 11.1% | 3 |
| fix-word-count-ignores-case | gemma4-think | 3 | 100.0% | 0.0 | 8.3 | 80.03 | 0.0% | 0.0% | 3 |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.0 | -20.22 | 5.1% | 23.9% | 4 |
| fix-word-count-ignores-case | qwen | 3 | 100.0% | 0.0 | 6.0 | 14.18 | 0.0% | 0.0% | 4 |
| long-change-timeout-everywhere | gemma4 | 3 | 100.0% | 0.0 | 21.7 | -0.44 | 0.0% | 0.0% | 153 |
| long-change-timeout-everywhere | gemma4-think | 3 | 100.0% | 0.0 | 23.0 | -8.51 | 0.0% | 25.0% | 156 |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 15.0 | -2.71 | 0.0% | 29.6% | 156 |
| long-change-timeout-everywhere | qwen | 3 | 100.0% | 0.0 | 9.7 | -27.27 | 0.0% | 20.8% | 156 |
| long-find-definition-in-tree | gemma4 | 3 | 100.0% | 0.0 | 3.0 | -38.48 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | gemma4-think | 3 | 100.0% | 0.0 | 3.0 | -53.86 | 0.0% | 0.0% | 0 |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 7.7 | -38.27 | 0.0% | 13.9% | 0 |
| long-find-definition-in-tree | qwen | 3 | 100.0% | 0.0 | 4.0 | 188.60 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4 | 3 | 100.0% | 0.0 | 4.0 | -650.49 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | gemma4-think | 3 | 100.0% | 0.0 | 3.3 | -47.78 | 0.0% | 0.0% | 0 |
| long-find-error-in-log | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 4.3 | -27.76 | 0.0% | 8.3% | 0 |
| long-find-error-in-log | qwen | 3 | 100.0% | 0.0 | 3.0 | -201.56 | 0.0% | 0.0% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| gemma4 | end_turn | 59 |
| gemma4 | max_turns | 1 |
| gemma4-think | end_turn | 60 |
| minimax-m3.1-flash | end_turn | 60 |
| qwen | end_turn | 60 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-dead-code | gemma4 | 1 | yes | end_turn | 8 | 424 | 2 | 0 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/gemma4/seed1/session.jsonl` |
| find-dead-code | gemma4 | 2 | yes | end_turn | 8 | 553 | 2 | 0 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/gemma4/seed2/session.jsonl` |
| find-dead-code | gemma4 | 3 | yes | end_turn | 8 | 432 | 2 | 0 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/gemma4/seed3/session.jsonl` |
| find-dead-code | gemma4-think | 1 | yes | end_turn | 8 | 280 | 2 | 0 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/gemma4-think/seed1/session.jsonl` |
| find-dead-code | gemma4-think | 2 | yes | end_turn | 8 | 580 | 2 | 0 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/gemma4-think/seed2/session.jsonl` |
| find-dead-code | gemma4-think | 3 | yes | end_turn | 8 | 290 | 2 | 0 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/gemma4-think/seed3/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 749 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/minimax-m3.1-flash/seed1/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 758 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/minimax-m3.1-flash/seed2/session.jsonl` |
| find-dead-code | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 725 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/minimax-m3.1-flash/seed3/session.jsonl` |
| find-dead-code | qwen | 1 | yes | end_turn | 5 | 659 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/qwen/seed1/session.jsonl` |
| find-dead-code | qwen | 2 | yes | end_turn | 5 | 598 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/qwen/seed2/session.jsonl` |
| find-dead-code | qwen | 3 | yes | end_turn | 5 | 886 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-dead-code/qwen/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4 | 1 | no | end_turn | 20 | 572 | 0 | 5 | 12 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/gemma4/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4 | 2 | no | end_turn | 20 | 646 | 0 | 5 | 12 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/gemma4/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4 | 3 | no | end_turn | 18 | 667 | 0 | 4 | 14 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/gemma4/seed3/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 1 | no | end_turn | 18 | 671 | 0 | 4 | 12 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/gemma4-think/seed1/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 2 | no | end_turn | 18 | 656 | 0 | 4 | 12 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/gemma4-think/seed2/session.jsonl` |
| find-off-by-one-loop | gemma4-think | 3 | no | end_turn | 18 | 686 | 0 | 4 | 14 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/gemma4-think/seed3/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 780 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 744 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-off-by-one-loop | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 823 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-off-by-one-loop | qwen | 1 | yes | end_turn | 11 | 560 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/qwen/seed1/session.jsonl` |
| find-off-by-one-loop | qwen | 2 | yes | end_turn | 6 | 523 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/qwen/seed2/session.jsonl` |
| find-off-by-one-loop | qwen | 3 | yes | end_turn | 9 | 825 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-off-by-one-loop/qwen/seed3/session.jsonl` |
| find-secret-in-config | gemma4 | 1 | yes | end_turn | 4 | 277 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/gemma4/seed1/session.jsonl` |
| find-secret-in-config | gemma4 | 2 | yes | end_turn | 4 | 236 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/gemma4/seed2/session.jsonl` |
| find-secret-in-config | gemma4 | 3 | yes | end_turn | 4 | 298 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/gemma4/seed3/session.jsonl` |
| find-secret-in-config | gemma4-think | 1 | yes | end_turn | 4 | 237 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/gemma4-think/seed1/session.jsonl` |
| find-secret-in-config | gemma4-think | 2 | yes | end_turn | 4 | 278 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/gemma4-think/seed2/session.jsonl` |
| find-secret-in-config | gemma4-think | 3 | yes | end_turn | 4 | 315 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/gemma4-think/seed3/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 732 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/minimax-m3.1-flash/seed1/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 775 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/minimax-m3.1-flash/seed2/session.jsonl` |
| find-secret-in-config | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 758 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/minimax-m3.1-flash/seed3/session.jsonl` |
| find-secret-in-config | qwen | 1 | yes | end_turn | 5 | 485 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/qwen/seed1/session.jsonl` |
| find-secret-in-config | qwen | 2 | yes | end_turn | 3 | 430 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/qwen/seed2/session.jsonl` |
| find-secret-in-config | qwen | 3 | yes | end_turn | 5 | 483 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-secret-in-config/qwen/seed3/session.jsonl` |
| find-slow-loop | gemma4 | 1 | yes | end_turn | 7 | 301 | 1 | 0 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/gemma4/seed1/session.jsonl` |
| find-slow-loop | gemma4 | 2 | yes | end_turn | 7 | 290 | 1 | 0 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/gemma4/seed2/session.jsonl` |
| find-slow-loop | gemma4 | 3 | yes | end_turn | 7 | 291 | 1 | 0 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/gemma4/seed3/session.jsonl` |
| find-slow-loop | gemma4-think | 1 | yes | end_turn | 7 | 396 | 1 | 0 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/gemma4-think/seed1/session.jsonl` |
| find-slow-loop | gemma4-think | 2 | yes | end_turn | 7 | 367 | 1 | 0 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/gemma4-think/seed2/session.jsonl` |
| find-slow-loop | gemma4-think | 3 | yes | end_turn | 7 | 291 | 1 | 0 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/gemma4-think/seed3/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 766 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 11 | 862 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 13 | 716 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-slow-loop | qwen | 1 | yes | end_turn | 5 | 500 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/qwen/seed1/session.jsonl` |
| find-slow-loop | qwen | 2 | yes | end_turn | 6 | 512 | 1 | 0 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/qwen/seed2/session.jsonl` |
| find-slow-loop | qwen | 3 | yes | end_turn | 6 | 496 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-slow-loop/qwen/seed3/session.jsonl` |
| find-version-mismatch | gemma4 | 1 | yes | end_turn | 7 | 374 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/gemma4/seed1/session.jsonl` |
| find-version-mismatch | gemma4 | 2 | yes | end_turn | 7 | 338 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/gemma4/seed2/session.jsonl` |
| find-version-mismatch | gemma4 | 3 | yes | end_turn | 5 | 672 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/gemma4/seed3/session.jsonl` |
| find-version-mismatch | gemma4-think | 1 | yes | end_turn | 9 | 873 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/gemma4-think/seed1/session.jsonl` |
| find-version-mismatch | gemma4-think | 2 | yes | end_turn | 5 | 1084 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/gemma4-think/seed2/session.jsonl` |
| find-version-mismatch | gemma4-think | 3 | yes | end_turn | 9 | 832 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/gemma4-think/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 5 | 776 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 756 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 706 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | qwen | 1 | yes | end_turn | 5 | 619 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/qwen/seed1/session.jsonl` |
| find-version-mismatch | qwen | 2 | yes | end_turn | 4 | 471 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/qwen/seed2/session.jsonl` |
| find-version-mismatch | qwen | 3 | yes | end_turn | 4 | 470 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/find-version-mismatch/qwen/seed3/session.jsonl` |
| fix-cache-never-expires | gemma4 | 1 | yes | end_turn | 13 | 1936 | 2 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/gemma4/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4 | 2 | yes | end_turn | 13 | 1238 | 2 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/gemma4/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4 | 3 | no | max_turns | 50 | 844 | 1 | 0 | 0 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/gemma4/seed3/session.jsonl` (poo exited 11) |
| fix-cache-never-expires | gemma4-think | 1 | yes | end_turn | 13 | 919 | 2 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/gemma4-think/seed1/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 2 | yes | end_turn | 12 | 960 | 2 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/gemma4-think/seed2/session.jsonl` |
| fix-cache-never-expires | gemma4-think | 3 | yes | end_turn | 12 | 1094 | 1 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/gemma4-think/seed3/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 684 | 0 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 2 | yes | end_turn | 10 | 750 | 0 | 1 | 7 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-cache-never-expires | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 1003 | 0 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-cache-never-expires | qwen | 1 | yes | end_turn | 6 | 618 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/qwen/seed1/session.jsonl` |
| fix-cache-never-expires | qwen | 2 | yes | end_turn | 6 | 755 | 0 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/qwen/seed2/session.jsonl` |
| fix-cache-never-expires | qwen | 3 | yes | end_turn | 7 | 951 | 0 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-cache-never-expires/qwen/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4 | 1 | yes | end_turn | 10 | 662 | 1 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/gemma4/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4 | 2 | yes | end_turn | 11 | 813 | 1 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/gemma4/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4 | 3 | yes | end_turn | 10 | 834 | 1 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/gemma4/seed3/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 1 | yes | end_turn | 10 | 664 | 1 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/gemma4-think/seed1/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 2 | yes | end_turn | 10 | 798 | 1 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/gemma4-think/seed2/session.jsonl` |
| fix-csv-skips-header | gemma4-think | 3 | yes | end_turn | 10 | 909 | 1 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/gemma4-think/seed3/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 731 | 0 | 1 | 4 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 732 | 0 | 1 | 5 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-csv-skips-header | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 962 | 0 | 1 | 5 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-csv-skips-header | qwen | 1 | yes | end_turn | 6 | 945 | 0 | 0 | 1 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/qwen/seed1/session.jsonl` |
| fix-csv-skips-header | qwen | 2 | yes | end_turn | 6 | 819 | 0 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/qwen/seed2/session.jsonl` |
| fix-csv-skips-header | qwen | 3 | yes | end_turn | 6 | 526 | 0 | 0 | 5 | `runs/run-34f1bf5368f98000/fix-csv-skips-header/qwen/seed3/session.jsonl` |
| fix-date-format-pad | gemma4 | 1 | yes | end_turn | 10 | 371 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/gemma4/seed1/session.jsonl` |
| fix-date-format-pad | gemma4 | 2 | yes | end_turn | 10 | 415 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/gemma4/seed2/session.jsonl` |
| fix-date-format-pad | gemma4 | 3 | yes | end_turn | 9 | 470 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/gemma4/seed3/session.jsonl` |
| fix-date-format-pad | gemma4-think | 1 | yes | end_turn | 10 | 421 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/gemma4-think/seed1/session.jsonl` |
| fix-date-format-pad | gemma4-think | 2 | yes | end_turn | 9 | 447 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/gemma4-think/seed2/session.jsonl` |
| fix-date-format-pad | gemma4-think | 3 | yes | end_turn | 8 | 420 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/gemma4-think/seed3/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 762 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 748 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-date-format-pad | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 755 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-date-format-pad | qwen | 1 | yes | end_turn | 5 | 654 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/qwen/seed1/session.jsonl` |
| fix-date-format-pad | qwen | 2 | yes | end_turn | 6 | 598 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/qwen/seed2/session.jsonl` |
| fix-date-format-pad | qwen | 3 | yes | end_turn | 6 | 512 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-date-format-pad/qwen/seed3/session.jsonl` |
| fix-division-by-zero | gemma4 | 1 | yes | end_turn | 9 | 683 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-division-by-zero/gemma4/seed1/session.jsonl` |
| fix-division-by-zero | gemma4 | 2 | yes | end_turn | 10 | 738 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-division-by-zero/gemma4/seed2/session.jsonl` |
| fix-division-by-zero | gemma4 | 3 | yes | end_turn | 10 | 735 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-division-by-zero/gemma4/seed3/session.jsonl` |
| fix-division-by-zero | gemma4-think | 1 | yes | end_turn | 10 | 649 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-division-by-zero/gemma4-think/seed1/session.jsonl` |
| fix-division-by-zero | gemma4-think | 2 | yes | end_turn | 10 | 805 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-division-by-zero/gemma4-think/seed2/session.jsonl` |
| fix-division-by-zero | gemma4-think | 3 | yes | end_turn | 10 | 627 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-division-by-zero/gemma4-think/seed3/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 777 | 0 | 2 | 2 | `runs/run-34f1bf5368f98000/fix-division-by-zero/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 859 | 0 | 1 | 5 | `runs/run-34f1bf5368f98000/fix-division-by-zero/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-division-by-zero | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 759 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-division-by-zero/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-division-by-zero | qwen | 1 | yes | end_turn | 6 | 742 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-division-by-zero/qwen/seed1/session.jsonl` |
| fix-division-by-zero | qwen | 2 | yes | end_turn | 8 | 649 | 0 | 0 | 6 | `runs/run-34f1bf5368f98000/fix-division-by-zero/qwen/seed2/session.jsonl` |
| fix-division-by-zero | qwen | 3 | yes | end_turn | 6 | 632 | 0 | 0 | 5 | `runs/run-34f1bf5368f98000/fix-division-by-zero/qwen/seed3/session.jsonl` |
| fix-json-missing-key | gemma4 | 1 | yes | end_turn | 10 | 624 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-json-missing-key/gemma4/seed1/session.jsonl` |
| fix-json-missing-key | gemma4 | 2 | yes | end_turn | 10 | 727 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-json-missing-key/gemma4/seed2/session.jsonl` |
| fix-json-missing-key | gemma4 | 3 | yes | end_turn | 9 | 626 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-json-missing-key/gemma4/seed3/session.jsonl` |
| fix-json-missing-key | gemma4-think | 1 | yes | end_turn | 10 | 595 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-json-missing-key/gemma4-think/seed1/session.jsonl` |
| fix-json-missing-key | gemma4-think | 2 | yes | end_turn | 10 | 737 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-json-missing-key/gemma4-think/seed2/session.jsonl` |
| fix-json-missing-key | gemma4-think | 3 | yes | end_turn | 10 | 477 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-json-missing-key/gemma4-think/seed3/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 804 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-json-missing-key/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 782 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-json-missing-key/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-json-missing-key | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 780 | 0 | 1 | 4 | `runs/run-34f1bf5368f98000/fix-json-missing-key/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-json-missing-key | qwen | 1 | yes | end_turn | 6 | 711 | 0 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-json-missing-key/qwen/seed1/session.jsonl` |
| fix-json-missing-key | qwen | 2 | yes | end_turn | 8 | 1001 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-json-missing-key/qwen/seed2/session.jsonl` |
| fix-json-missing-key | qwen | 3 | yes | end_turn | 6 | 1075 | 0 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-json-missing-key/qwen/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 1 | yes | end_turn | 16 | 507 | 2 | 2 | 4 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/gemma4/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 2 | yes | end_turn | 15 | 566 | 2 | 2 | 4 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/gemma4/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4 | 3 | yes | end_turn | 16 | 562 | 2 | 2 | 4 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/gemma4/seed3/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 1 | yes | end_turn | 16 | 557 | 2 | 2 | 4 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/gemma4-think/seed1/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 2 | yes | end_turn | 15 | 462 | 2 | 2 | 4 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/gemma4-think/seed2/session.jsonl` |
| fix-off-by-one-slice | gemma4-think | 3 | yes | end_turn | 13 | 472 | 2 | 2 | 4 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/gemma4-think/seed3/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 831 | 0 | 2 | 14 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 808 | 0 | 1 | 15 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-off-by-one-slice | minimax-m3.1-flash | 3 | yes | end_turn | 8 | 765 | 0 | 1 | 6 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-off-by-one-slice | qwen | 1 | yes | end_turn | 7 | 565 | 0 | 1 | 4 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/qwen/seed1/session.jsonl` |
| fix-off-by-one-slice | qwen | 2 | yes | end_turn | 7 | 846 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/qwen/seed2/session.jsonl` |
| fix-off-by-one-slice | qwen | 3 | yes | end_turn | 6 | 583 | 0 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-off-by-one-slice/qwen/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 1 | yes | end_turn | 13 | 699 | 0 | 2 | 8 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/gemma4/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 2 | yes | end_turn | 22 | 1265 | 0 | 8 | 14 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/gemma4/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4 | 3 | yes | end_turn | 23 | 984 | 0 | 2 | 11 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/gemma4/seed3/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 1 | yes | end_turn | 23 | 907 | 0 | 4 | 16 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/gemma4-think/seed1/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 2 | yes | end_turn | 19 | 1593 | 0 | 2 | 11 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/gemma4-think/seed2/session.jsonl` |
| fix-path-join-double-slash | gemma4-think | 3 | yes | end_turn | 19 | 1080 | 0 | 3 | 8 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/gemma4-think/seed3/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 1 | yes | end_turn | 9 | 945 | 0 | 2 | 13 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 778 | 1 | 1 | 24 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-path-join-double-slash | minimax-m3.1-flash | 3 | yes | end_turn | 9 | 861 | 0 | 1 | 14 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-path-join-double-slash | qwen | 1 | yes | end_turn | 6 | 715 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/qwen/seed1/session.jsonl` |
| fix-path-join-double-slash | qwen | 2 | yes | end_turn | 6 | 624 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/qwen/seed2/session.jsonl` |
| fix-path-join-double-slash | qwen | 3 | yes | end_turn | 12 | 774 | 0 | 2 | 10 | `runs/run-34f1bf5368f98000/fix-path-join-double-slash/qwen/seed3/session.jsonl` |
| fix-retry-backoff | gemma4 | 1 | yes | end_turn | 10 | 429 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/gemma4/seed1/session.jsonl` |
| fix-retry-backoff | gemma4 | 2 | yes | end_turn | 10 | 426 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/gemma4/seed2/session.jsonl` |
| fix-retry-backoff | gemma4 | 3 | yes | end_turn | 10 | 427 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/gemma4/seed3/session.jsonl` |
| fix-retry-backoff | gemma4-think | 1 | yes | end_turn | 10 | 1055 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/gemma4-think/seed1/session.jsonl` |
| fix-retry-backoff | gemma4-think | 2 | yes | end_turn | 10 | 1080 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/gemma4-think/seed2/session.jsonl` |
| fix-retry-backoff | gemma4-think | 3 | yes | end_turn | 10 | 491 | 1 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/gemma4-think/seed3/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 1032 | 0 | 2 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 753 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-retry-backoff | minimax-m3.1-flash | 3 | no | end_turn | 3 | 718 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/fix-retry-backoff/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-retry-backoff | qwen | 1 | yes | end_turn | 6 | 1447 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/qwen/seed1/session.jsonl` |
| fix-retry-backoff | qwen | 2 | yes | end_turn | 8 | 1077 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/qwen/seed2/session.jsonl` |
| fix-retry-backoff | qwen | 3 | yes | end_turn | 6 | 913 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-retry-backoff/qwen/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 1 | yes | end_turn | 10 | 507 | 1 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/gemma4/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 2 | yes | end_turn | 9 | 744 | 1 | 0 | 5 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/gemma4/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4 | 3 | yes | end_turn | 10 | 653 | 1 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/gemma4/seed3/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 1 | yes | end_turn | 10 | 661 | 1 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/gemma4-think/seed1/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 2 | yes | end_turn | 10 | 634 | 1 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/gemma4-think/seed2/session.jsonl` |
| fix-sort-mutates-input | gemma4-think | 3 | yes | end_turn | 10 | 996 | 1 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/gemma4-think/seed3/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 772 | 0 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 862 | 0 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-sort-mutates-input | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 776 | 0 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-sort-mutates-input | qwen | 1 | yes | end_turn | 6 | 506 | 0 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/qwen/seed1/session.jsonl` |
| fix-sort-mutates-input | qwen | 2 | yes | end_turn | 6 | 502 | 0 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/qwen/seed2/session.jsonl` |
| fix-sort-mutates-input | qwen | 3 | yes | end_turn | 7 | 510 | 0 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-sort-mutates-input/qwen/seed3/session.jsonl` |
| fix-stack-push-order | gemma4 | 1 | yes | end_turn | 9 | 1201 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/gemma4/seed1/session.jsonl` |
| fix-stack-push-order | gemma4 | 2 | yes | end_turn | 8 | 440 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/gemma4/seed2/session.jsonl` |
| fix-stack-push-order | gemma4 | 3 | yes | end_turn | 9 | 499 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/gemma4/seed3/session.jsonl` |
| fix-stack-push-order | gemma4-think | 1 | yes | end_turn | 8 | 538 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/gemma4-think/seed1/session.jsonl` |
| fix-stack-push-order | gemma4-think | 2 | yes | end_turn | 8 | 535 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/gemma4-think/seed2/session.jsonl` |
| fix-stack-push-order | gemma4-think | 3 | yes | end_turn | 9 | 340 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/gemma4-think/seed3/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 679 | 0 | 1 | 4 | `runs/run-34f1bf5368f98000/fix-stack-push-order/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 1240 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-stack-push-order | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 932 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-stack-push-order | qwen | 1 | yes | end_turn | 5 | 649 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/qwen/seed1/session.jsonl` |
| fix-stack-push-order | qwen | 2 | yes | end_turn | 7 | 660 | 0 | 2 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/qwen/seed2/session.jsonl` |
| fix-stack-push-order | qwen | 3 | yes | end_turn | 6 | 511 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-stack-push-order/qwen/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 1 | yes | end_turn | 8 | 498 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/gemma4/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 2 | yes | end_turn | 9 | 483 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/gemma4/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4 | 3 | yes | end_turn | 9 | 1074 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/gemma4/seed3/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 1 | yes | end_turn | 8 | 711 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/gemma4-think/seed1/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 2 | yes | end_turn | 8 | 705 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/gemma4-think/seed2/session.jsonl` |
| fix-unicode-decode-error | gemma4-think | 3 | yes | end_turn | 8 | 1271 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/gemma4-think/seed3/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 815 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 2 | yes | end_turn | 12 | 794 | 0 | 3 | 4 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-unicode-decode-error | minimax-m3.1-flash | 3 | yes | end_turn | 7 | 809 | 0 | 1 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-unicode-decode-error | qwen | 1 | yes | end_turn | 6 | 505 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/qwen/seed1/session.jsonl` |
| fix-unicode-decode-error | qwen | 2 | yes | end_turn | 6 | 498 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/qwen/seed2/session.jsonl` |
| fix-unicode-decode-error | qwen | 3 | yes | end_turn | 6 | 484 | 0 | 0 | 2 | `runs/run-34f1bf5368f98000/fix-unicode-decode-error/qwen/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 1 | yes | end_turn | 14 | 1430 | 2 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/gemma4/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 2 | yes | end_turn | 8 | 1448 | 0 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/gemma4/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4 | 3 | yes | end_turn | 9 | 677 | 0 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/gemma4/seed3/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 1 | yes | end_turn | 8 | 1797 | 0 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/gemma4-think/seed1/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 2 | yes | end_turn | 8 | 737 | 0 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/gemma4-think/seed2/session.jsonl` |
| fix-word-count-ignores-case | gemma4-think | 3 | yes | end_turn | 9 | 683 | 0 | 0 | 3 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/gemma4-think/seed3/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 1 | yes | end_turn | 6 | 730 | 0 | 1 | 4 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/minimax-m3.1-flash/seed1/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 2 | yes | end_turn | 9 | 871 | 2 | 3 | 4 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/minimax-m3.1-flash/seed2/session.jsonl` |
| fix-word-count-ignores-case | minimax-m3.1-flash | 3 | yes | end_turn | 6 | 740 | 0 | 1 | 3 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/minimax-m3.1-flash/seed3/session.jsonl` |
| fix-word-count-ignores-case | qwen | 1 | yes | end_turn | 6 | 522 | 0 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/qwen/seed1/session.jsonl` |
| fix-word-count-ignores-case | qwen | 2 | yes | end_turn | 7 | 529 | 0 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/qwen/seed2/session.jsonl` |
| fix-word-count-ignores-case | qwen | 3 | yes | end_turn | 5 | 986 | 0 | 0 | 4 | `runs/run-34f1bf5368f98000/fix-word-count-ignores-case/qwen/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 1 | yes | end_turn | 32 | 659 | 0 | 0 | 152 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/gemma4/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 2 | yes | end_turn | 15 | 870 | 0 | 0 | 152 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/gemma4/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4 | 3 | yes | end_turn | 18 | 590 | 0 | 0 | 156 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/gemma4/seed3/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 1 | yes | end_turn | 31 | 1046 | 0 | 3 | 157 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/gemma4-think/seed1/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 2 | yes | end_turn | 19 | 843 | 0 | 1 | 156 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/gemma4-think/seed2/session.jsonl` |
| long-change-timeout-everywhere | gemma4-think | 3 | yes | end_turn | 19 | 864 | 0 | 0 | 156 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/gemma4-think/seed3/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 1 | yes | end_turn | 15 | 909 | 0 | 6 | 156 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/minimax-m3.1-flash/seed1/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 2 | yes | end_turn | 16 | 845 | 0 | 7 | 156 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/minimax-m3.1-flash/seed2/session.jsonl` |
| long-change-timeout-everywhere | minimax-m3.1-flash | 3 | yes | end_turn | 14 | 828 | 0 | 4 | 156 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/minimax-m3.1-flash/seed3/session.jsonl` |
| long-change-timeout-everywhere | qwen | 1 | yes | end_turn | 11 | 756 | 0 | 3 | 156 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/qwen/seed1/session.jsonl` |
| long-change-timeout-everywhere | qwen | 2 | yes | end_turn | 10 | 886 | 0 | 1 | 156 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/qwen/seed2/session.jsonl` |
| long-change-timeout-everywhere | qwen | 3 | yes | end_turn | 8 | 1134 | 0 | 0 | 156 | `runs/run-34f1bf5368f98000/long-change-timeout-everywhere/qwen/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 1 | yes | end_turn | 3 | 316 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/gemma4/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 2 | yes | end_turn | 3 | 261 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/gemma4/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4 | 3 | yes | end_turn | 3 | 260 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/gemma4/seed3/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 1 | yes | end_turn | 3 | 259 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/gemma4-think/seed1/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 2 | yes | end_turn | 3 | 259 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/gemma4-think/seed2/session.jsonl` |
| long-find-definition-in-tree | gemma4-think | 3 | yes | end_turn | 3 | 259 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/gemma4-think/seed3/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 1 | yes | end_turn | 12 | 777 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 2 | yes | end_turn | 7 | 809 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-definition-in-tree | minimax-m3.1-flash | 3 | yes | end_turn | 4 | 834 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-definition-in-tree | qwen | 1 | yes | end_turn | 3 | 750 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/qwen/seed1/session.jsonl` |
| long-find-definition-in-tree | qwen | 2 | yes | end_turn | 3 | 918 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/qwen/seed2/session.jsonl` |
| long-find-definition-in-tree | qwen | 3 | yes | end_turn | 6 | 672 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-definition-in-tree/qwen/seed3/session.jsonl` |
| long-find-error-in-log | gemma4 | 1 | yes | end_turn | 4 | 518 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/gemma4/seed1/session.jsonl` |
| long-find-error-in-log | gemma4 | 2 | yes | end_turn | 4 | 1078 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/gemma4/seed2/session.jsonl` |
| long-find-error-in-log | gemma4 | 3 | yes | end_turn | 4 | 1092 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/gemma4/seed3/session.jsonl` |
| long-find-error-in-log | gemma4-think | 1 | yes | end_turn | 4 | 465 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/gemma4-think/seed1/session.jsonl` |
| long-find-error-in-log | gemma4-think | 2 | yes | end_turn | 3 | 296 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/gemma4-think/seed2/session.jsonl` |
| long-find-error-in-log | gemma4-think | 3 | yes | end_turn | 3 | 253 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/gemma4-think/seed3/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 1 | yes | end_turn | 4 | 645 | 0 | 1 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/minimax-m3.1-flash/seed1/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 2 | yes | end_turn | 5 | 681 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/minimax-m3.1-flash/seed2/session.jsonl` |
| long-find-error-in-log | minimax-m3.1-flash | 3 | yes | end_turn | 4 | 756 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/minimax-m3.1-flash/seed3/session.jsonl` |
| long-find-error-in-log | qwen | 1 | yes | end_turn | 4 | 656 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/qwen/seed1/session.jsonl` |
| long-find-error-in-log | qwen | 2 | yes | end_turn | 3 | 448 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/qwen/seed2/session.jsonl` |
| long-find-error-in-log | qwen | 3 | yes | end_turn | 2 | 620 | 0 | 0 | 0 | `runs/run-34f1bf5368f98000/long-find-error-in-log/qwen/seed3/session.jsonl` |
