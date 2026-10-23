# poo eval report

- started: 2026-10-22T05:43:23.893503618Z
- finished: 2026-10-22T05:44:45.152375883Z
- git sha: 5a5b510
- runs: 6

## By model

| model | runs | success | spread | turns | input tok | output tok | wall ms | ttft ms | ttft trend | tool err | parse err | reread | diff lines | cache hit |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| minimax-m3.1-flash | 6 | 100.0% | 0.0 | 7.5 | 20169 | 2404 | 22112 | 941 | -17.66 | 0.0% | 0.0% | 13.9% | 0 | 63.5% |
| **all** | 6 | 100.0% | 0.0 | 7.5 | 20169 | 2404 | 22112 | 941 | -17.66 | 0.0% | 0.0% | 13.9% | 0 | 63.5% |

## By task

| task | model | runs | success | spread | turns | ttft trend | tool err | reread | diff lines |
|---|---|---|---|---|---|---|---|---|---|
| find-slow-loop | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 8.7 | -30.68 | 0.0% | 19.4% | 0 |
| find-version-mismatch | minimax-m3.1-flash | 3 | 100.0% | 0.0 | 6.3 | -4.64 | 0.0% | 8.3% | 0 |

## Settle reasons

| model | reason | runs |
|---|---|---|
| minimax-m3.1-flash | end_turn | 6 |

## Runs

| task | model | seed | ok | settle | turns | ttft ms | tool errs | rereads | diff lines | log |
|---|---|---|---|---|---|---|---|---|---|---|
| find-slow-loop | minimax-m3.1-flash | 1 | yes | end_turn | 7 | 889 | 0 | 2 | 0 | `runs/run-af704e6b28c8c344/find-slow-loop/minimax-m3.1-flash/seed1/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 2 | yes | end_turn | 8 | 924 | 0 | 2 | 0 | `runs/run-af704e6b28c8c344/find-slow-loop/minimax-m3.1-flash/seed2/session.jsonl` |
| find-slow-loop | minimax-m3.1-flash | 3 | yes | end_turn | 11 | 1069 | 0 | 2 | 0 | `runs/run-af704e6b28c8c344/find-slow-loop/minimax-m3.1-flash/seed3/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 1 | yes | end_turn | 8 | 886 | 0 | 0 | 0 | `runs/run-af704e6b28c8c344/find-version-mismatch/minimax-m3.1-flash/seed1/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 2 | yes | end_turn | 6 | 914 | 0 | 1 | 0 | `runs/run-af704e6b28c8c344/find-version-mismatch/minimax-m3.1-flash/seed2/session.jsonl` |
| find-version-mismatch | minimax-m3.1-flash | 3 | yes | end_turn | 5 | 963 | 0 | 1 | 0 | `runs/run-af704e6b28c8c344/find-version-mismatch/minimax-m3.1-flash/seed3/session.jsonl` |
