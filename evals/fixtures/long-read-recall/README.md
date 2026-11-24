A long deployment config for the read-recall task: the answer sits
inside a file too big for one read at a small context window.

The config is generated, not committed: the `generate` script writes it
from the shared `evals/tasks/generate-services.py`, deterministically,
so every run reads the same file. The runner runs the script against
the fixture's snapshot; the committed source is the generator.
