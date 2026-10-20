#!/usr/bin/env python3
"""Check that a run's answer names the thing the task asked about.

The answer is the run's last message: the session log holds one JSON
object per line, and the last message whose origin is "model" holds it.
"""
import json
import sys


def main():
    if len(sys.argv) != 3:
        print("usage: check_answer.py <session.jsonl> <keyword>", file=sys.stderr)
        return 2
    log_path, keyword = sys.argv[1], sys.argv[2]

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

    if not answer:
        print("the run recorded no answer", file=sys.stderr)
        return 1

    if keyword.lower() not in answer.lower():
        print(f"the answer does not mention {keyword!r}:", file=sys.stderr)
        print(answer[:2000], file=sys.stderr)
        return 1

    print("the answer names the thing the task asked about")
    return 0


if __name__ == "__main__":
    sys.exit(main())
