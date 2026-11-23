#!/usr/bin/env python3
"""Generate the deployment config the read-recall tasks read.

The corpus is generated, not committed: the committed source is this
generator, and the file it writes is the fixture's own artifact,
deterministic in its index arithmetic, so every run reads the same
file.

Exactly one service - the needle - has a retry budget that is not a
whole multiple of 500, and the incident id its comment names is what
the task's check derives from the workdir. The needle sits a fifth of
the way in, far enough from either end that finding it takes reading.
"""
import os
import sys

NOUNS = ["cron", "email", "federate", "kafka", "webhook", "jobber", "yield",
         "auth", "assets", "render", "presence", "delta", "search", "billing",
         "queue", "metrics", "notify", "cache", "ingest", "ledger"]
ROLES = ["sync", "core", "mirror", "gate", "batch"]
REGIONS = ["eu", "us", "apac", "sa"]
PHRASES = [
    "raised after the backlog review",
    "set when the service shipped",
    "lowered after the timeout audit",
    "doubled for the sale window",
]


def main():
    if len(sys.argv) != 3:
        print(f"usage: {sys.argv[0]} <dest> <count>", file=sys.stderr)
        return 2
    dest, count = sys.argv[1], int(sys.argv[2])
    os.makedirs(os.path.dirname(dest), exist_ok=True)

    lines = [
        "# Deployment config: every service this team runs, with its rate",
        "# limits and its retry budget. One incident id names the change",
        "# that set a budget; the ids come from the incident tracker.",
        "services:",
    ]
    needle = count // 5
    for i in range(count):
        if i == needle:
            lines += [
                "  - name: archival",
                "    rate_limit_rps: 640",
                "    retry_budget_ms: 41200",
                "    # INC-2214 the archive sweep overran its budget.",
            ]
            continue
        # The name's four parts cycle on strides that keep the whole
        # name unique for the corpus's size: the noun repeats every 20
        # services, the role every 100, the region every 400, and the
        # suffix every 40000.
        name = (NOUNS[i % len(NOUNS)] + "-" + ROLES[(i // len(NOUNS)) % len(ROLES)]
                + "-" + REGIONS[(i // (len(NOUNS) * len(ROLES))) % len(REGIONS)]
                + str((i // (len(NOUNS) * len(ROLES) * len(REGIONS))) % 100).zfill(2))
        rate = 300 + (i * 7919) % 3600
        budget = 6000 + (i * 1303) % 17000
        budget -= budget % 500
        inc = 3000 + (i * 367) % 10007
        lines += [
            f"  - name: {name}",
            f"    rate_limit_rps: {rate}",
            f"    retry_budget_ms: {budget}",
            f"    # INC-{inc} {PHRASES[i % len(PHRASES)]}",
        ]
    with open(dest, "w") as f:
        f.write("\n".join(lines) + "\n")


if __name__ == "__main__":
    sys.exit(main())
