# poo eval comparison

- baseline: ../../../../../poo-s8/evals/results/2026-10-21-003728-f6a832f
- candidate: ..
- regressions: 2
- improvements: 1

| model | metric | baseline | candidate | delta | floor | direction |
|---|---|---|---|---|---|---|
| gemma4-think | input_tokens | 2.587e+04 | 5.486e+04 | +2.898e+04 | 2.654e+04 | worse (lower is better) |
| qwen | input_tokens | 9989 | 1.643e+04 | +6441 | 3659 | worse (lower is better) |
| qwen | output_tokens | 687.3 | 388.3 | -299 | 137.5 | better (lower is better) |
