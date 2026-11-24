A deployment config sized so that reading it whole crosses the window
at a large context limit: the recall task for the big-window runs.

The config is generated, not committed: the `generate` script writes it
from the shared `evals/tasks/generate-services.py`, deterministically,
so every run reads the same file. The runner runs the script against
the fixture's snapshot; the committed source is the generator.
