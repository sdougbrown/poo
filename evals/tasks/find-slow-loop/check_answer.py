#!/usr/bin/env python3
"""Check that a run's answer names the thing the task asked about.

The answer is the run's last message: the session log holds one JSON
object per line, and the last message whose origin is "model" holds it.

The expected answer is *derived* from the workdir, not written into this
script. A check that spells the answer out would hand it to any model
that reads the task directory, and the task would stop measuring
anything.
"""
import json
import os
import re
import sys


def last_answer(log_path):
    """Return the run's last message."""
    answer = ""
    with open(log_path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                record = json.loads(line)
            except json.JSONDecodeError:
                continue
            if record.get("record") != "message":
                continue
            if record.get("origin") != "model":
                continue
            text = ""
            for block in record.get("content") or []:
                if block.get("kind") == "text":
                    text += block.get("text") or ""
            answer = text
    return answer


def read(path):
    """Return a file's content, or the empty string."""
    try:
        with open(path) as f:
            return f.read()
    except OSError:
        return ""


def derive(workdir, task):
    """Return the answer the task asked about.

    Each derivation reads the workdir and works out the answer the same
    way a careful model would, so the check verifies the model's answer
    against the ground truth rather than against a written-out string.
    """
    if task == "find-secret-in-config":
        # The environment variable the token is read from.
        source = read(os.path.join(workdir, "app", "secrets.py"))
        match = re.search(r'environ\.get\(\s*["\']([A-Z0-9_]+)["\']', source)
        return match.group(1) if match else ""

    if task == "find-dead-code":
        # The handler no route points at.
        handlers = set(re.findall(r"^def (handle_\w+)\(", read(os.path.join(workdir, "src", "handlers.py")), re.M))
        routes = set(re.findall(r"handlers\.(handle_\w+)", read(os.path.join(workdir, "src", "routes.py"))))
        unreachable = sorted(handlers - routes)
        return unreachable[0] if unreachable else ""

    if task == "find-off-by-one-loop":
        # The function that drops the last item.
        source = read(os.path.join(workdir, "lib", "pipeline.py"))
        match = re.search(r"^def (chunks\w*)\(", source, re.M)
        return match.group(1) if match else ""

    if task == "find-slow-loop":
        # The method that scans the whole list.
        source = read(os.path.join(workdir, "svc", "store.py"))
        match = re.search(r"^    def (get\w*)\(", source, re.M)
        return match.group(1) if match else ""

    if task == "find-version-mismatch":
        # The version the package declares, against the one the
        # packaging metadata declares.
        source = read(os.path.join(workdir, "src", "shop", "__init__.py"))
        match = re.search(r'__version__\s*=\s*["\']([^"\']+)["\']', source)
        return match.group(1) if match else ""

    if task == "long-find-error-in-log":
        # The request id on the one ERROR line.
        source = read(os.path.join(workdir, "logs", "service.log"))
        for line in source.splitlines():
            if " ERROR " in line:
                match = re.search(r"request (\S+)", line)
                if match:
                    return match.group(1)
        return ""

    if task == "long-find-definition-in-tree":
        # The file that defines the symbol.
        for root, _, files in os.walk(workdir):
            for name in sorted(files):
                if not name.endswith(".py"):
                    continue
                path = os.path.join(root, name)
                if re.search(r"^def parse_manifest\(", read(path), re.M):
                    return os.path.relpath(path, workdir)
        return ""

    raise SystemExit(f"check_answer.py: no derivation for {task}")


def main():
    if len(sys.argv) != 4:
        print("usage: check_answer.py <session.jsonl> <workdir> <task>", file=sys.stderr)
        return 2
    log_path, workdir, task = sys.argv[1], sys.argv[2], sys.argv[3]

    expected = derive(workdir, task)
    if not expected:
        print(f"the check could not work out the answer for {task}", file=sys.stderr)
        return 1

    answer = last_answer(log_path)
    if not answer:
        print("the run recorded no answer", file=sys.stderr)
        return 1

    if expected.lower() not in answer.lower():
        print(f"the answer does not mention {expected!r}:", file=sys.stderr)
        print(answer[:2000], file=sys.stderr)
        return 1

    print(f"the answer names {expected!r}, which is what the task asked about")
    return 0


if __name__ == "__main__":
    sys.exit(main())
