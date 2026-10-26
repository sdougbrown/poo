# Stage 12 step 2 eval notes (prompt wording)

There was one eval per wording commit, each cumulative on the ones before it. The baseline is `2026-10-24-041541-9dbf55f` (`stage/12-prompts`, the prompt byte-identical to step 1), with 97.1% success over 240 runs. Every run used `--seeds 3 --parallel 4 --sandbox bwrap --executor bwrap`.

| commit | group | success | gemma4 | gemma4-think | minimax | qwen | vs baseline |
|---|---|---|---|---|---|---|---|
| `827dc1c` | base, environment, jobs | 97.1% | 93.3% | 95.0% | 100% | 100% | no difference above the noise floor |
| `3220739` | the bash, edit, read and grep sections | 96.7% | 95.0% | 93.3% | 100% | 98.3% | no difference above the noise floor |
| `3c29554` | text-mode tool-call prose | 96.2% | 95.0% | 90.0% | 100% | 100% | no difference above the noise floor |
| `99dc10c` | tool and argument descriptions | 95.8% | 91.7% | 91.7% | 100% | 100% | no difference above the noise floor |

This directory holds the `99dc10c` run and its comparison. The other three runs aren't committed.

- The cli-registry group (`8118b4b`) wasn't evaluated: no eval task links cli-registry, so none of its text reaches the eval models.
- No model sent an empty tool call, the failure the 2026-10-23 wholesale rewrite caused in gemma4.
- In the `99dc10c` run, qwen used more input tokens (24.5k against 18.7k) and more turns (7.1 against 6.0). Both are below the noise floor. That commit changed only the `grep` glob description and the `cancel_job` id description.
