---
layout: home

hero:
  name: poo
  text: A modular Go agent harness
  tagline: Headless CLI and embeddable Go library for engineered agents. Compose at compile time, run feeds in a daemon, compact transparently.
  actions:
    - theme: brand
      text: Quickstart
      link: /getting-started/quickstart
    - theme: alt
      text: Why poo?
      link: /getting-started/why-poo
    - theme: alt
      text: GitHub
      link: https://github.com/sdougbrown/poo

features:
  - title: Engineered, Not Self-Mutating
    details: Compose tools, providers, and policy modules as ordinary Go packages at compile time. Stdlib-only core with an explicit finite state machine — what you leave out cannot be reached at runtime.
    link: /getting-started/why-poo
  - title: Feeds & Resident Daemon
    details: Run long-lived sessions under poo daemon with async command and MCP resource feeds, record filters, durable leases, and idle wake policies.
    link: /capabilities/feeds-and-daemon
  - title: Transparent Compaction
    details: Never silently rewrite history. Choose from mine, pointer, summary, handoff, or native strategies, and assign a dedicated roles.compaction model.
    link: /capabilities/compaction
  - title: Decision Model & CLI Registry
    details: Route permission asks, tool search, and feed filtering through roles.decider. Add drop-in JSON cli-registry tool manifests without rebuilding the binary.
    link: /capabilities/cli-registry
  - title: Defense-in-Depth Safety
    details: Combine declarative permission rules, ordered asker chains, doom-loop detection, protected paths, and a bwrap sandbox that never silently falls back.
    link: /operations/permissions-and-safety
  - title: Proven on Evals
    details: Validated with a 20-task poo-eval suite and a Harbor adapter for Terminal-Bench and SWE-bench so you can benchmark harnesses head-to-head on the same model.
    link: /reference/evals
---

## Quick Look

Install the release binary, check your roles, or run a single headless session against any OpenAI-compatible endpoint:

```bash
curl -fsSL https://raw.githubusercontent.com/sdougbrown/poo/main/install.sh | sh
poo run --endpoint http://localhost:4000/v1 --model-id gemma4 \
  --context-limit 262144 --max-turns 8 --log session.jsonl \
  "List the Go packages in this repository."
poo # launch the interactive REPL
```

Using an AI coding agent to read these docs? Point it at [`/llms.txt`](/llms.txt) (or [`/llms-full.txt`](/llms-full.txt) for the full concatenated reference).
