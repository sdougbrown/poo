#!/usr/bin/env python3
"""Check that a run's answer names every fact the task asked about.

The answer is the run's last message: the session log holds one JSON
object per line, and the last message whose origin is "model" holds it.

The expected answers are *derived* from the workdir, not written into
this script. A check that spells the answer out would hand it to any
model that reads the task directory, and the task would stop measuring
anything.

The derivation is the ground truth, so it runs against the workdir as
the model found it, not against a copy the harness made. A model that
edited the workdir to change the answer has not answered the question.
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


class Ident(str):
    """A name from the code, like a function or a method.

    A name that is an ordinary word ("get", "map", "chunks") is not
    found by the word alone: "you get slow behaviour" mentions the word
    without naming the method. It has to be written as a call, an
    attribute, a quoted name, or "the get method".
    """


def ident_forms(name):
    """Return the patterns that name a code identifier as one."""
    n = re.escape(name)
    quote = r"[`'\"]"
    return [
        r"\." + n + r"(?![\w])",
        r"(?<![\w])" + n + r"\s*\(",
        quote + n + quote,
        r"(?<![\w])" + n + r"\s+(?:method|function|helper|call)(?![\w])",
        r"(?<![\w])(?:method|function|helper|call)\s+" + n + r"(?![\w])",
    ]


def mentions(answer, needle):
    """Report whether the answer mentions a fact.

    A fact may be a compiled pattern, which is searched for as it is.

    The match is on word boundaries where the fact has any, so a short
    name is not found inside a longer one. A fact with no word
    characters (a path, an id) is matched as a substring.

    A code identifier that is an ordinary word is looked for in the
    forms that name it as code. One with an underscore or a digit in it
    is not an ordinary word, and the bare name is enough.
    """
    if not needle:
        return False
    if hasattr(needle, "search"):
        return bool(needle.search(answer))
    if isinstance(needle, Ident) and re.fullmatch(r"[A-Za-z]+", needle):
        return any(re.search(form, answer, re.I) for form in ident_forms(needle))
    if re.fullmatch(r"[\w./-]+", needle):
        return bool(re.search(r"(?<![\w])" + re.escape(needle) + r"(?![\w])", answer, re.I))
    return needle.lower() in answer.lower()


def unique_suffixes(workdir, relpath):
    """Return the path suffixes that name only this file in the workdir.

    An answer may say "loader.py" for "src/pipeline/loader.py" when no
    other file ends the same way.
    """
    files = []
    for root, dirs, names in os.walk(workdir):
        dirs[:] = [d for d in dirs if d != ".git"]
        for name in names:
            files.append(os.path.relpath(os.path.join(root, name), workdir).replace(os.sep, "/"))
    parts = relpath.replace(os.sep, "/").split("/")
    out = []
    for n in range(1, len(parts) + 1):
        suffix = "/".join(parts[-n:])
        matching = [f for f in files if f == suffix or f.endswith("/" + suffix)]
        if len(matching) == 1:
            out.append(suffix)
    return out or ["/".join(parts)]


def corrected_comparisons(condition):
    """Return the patterns for the comparison written the right way.

    A condition "i < len(items) - N" is off by one, and the right
    comparison is "i < len(items) - (N-1)", or "i <= len(items) - N".
    """
    m = re.fullmatch(r"(\w+)\s*<\s*len\((\w+)\)\s*-\s*(\d+)", condition.strip())
    if not m:
        return []
    var, seq, n = m.group(1), re.escape(m.group(2)), int(m.group(3))
    head = r"(?<![\w])" + re.escape(var) + r"\s*"
    out = [re.compile(head + r"<=\s*len\(" + seq + r"\)\s*-\s*" + str(n) + r"(?!\d)", re.I)]
    if n == 1:
        out.append(re.compile(head + r"<\s*len\(" + seq + r"\)(?!\s*-)", re.I))
    else:
        out.append(re.compile(head + r"<\s*len\(" + seq + r"\)\s*-\s*" + str(n - 1) + r"(?!\d)", re.I))
    return out


def derive(workdir, task):
    """Return the facts the task asked about, derived from the workdir.

    Each derivation reads the workdir and works out the answer the same
    way a careful model would, so the check verifies the model's answer
    against the ground truth rather than against a written-out string.

    A task's facts are a list of (name, values): the name says what
    the fact is, and the values are the strings that answer it. A fact
    with several accepted values has more than one right answer, and a
    fact whose values are empty cannot be derived, which is an error.
    """
    if task == "find-secret-in-config":
        # The function that reads the token, and the environment
        # variable it comes from.
        source = read(os.path.join(workdir, "app", "secrets.py"))
        match = re.search(r"environ\.get\(\s*[\"']([A-Z0-9_]+)[\"']", source)
        if not match:
            return []
        variable = match.group(1)
        # The function the read is inside, which is the one the task
        # asks for.
        func = ""
        for candidate in re.findall(r"^def (\w+)\(", source, re.M):
            body = source.split("def " + candidate, 1)[1]
            if variable in body:
                func = candidate
                break
        if not func:
            return []
        return [("the environment variable", [variable]), ("the function that reads it", [Ident(func)])]

    if task == "find-dead-code":
        # The handler no route points at, and the file that would have
        # to change for it to become reachable.
        handlers = set(re.findall(r"^def (handle_\w+)\(", read(os.path.join(workdir, "src", "handlers.py")), re.M))
        routes = set(re.findall(r"handlers\.(handle_\w+)", read(os.path.join(workdir, "src", "routes.py"))))
        unreachable = sorted(handlers - routes)
        if not unreachable:
            return []
        return [
            ("the unreachable handler", [Ident(unreachable[0])]),
            ("the file that would have to change", unique_suffixes(workdir, "src/routes.py")),
        ]

    if task == "find-off-by-one-loop":
        # The helper that chunks the list, and the comparison that is
        # off by one.
        source = read(os.path.join(workdir, "lib", "pipeline.py"))
        match = re.search(r"^def (\w+)\(", source, re.M)
        if not match:
            return []
        # The comparison in the loop that is off by one is the one
        # against the length of the list. The answer may write the
        # comparison out as the source writes it, or name the bound it
        # gets wrong.
        cond = re.search(r"while ([^\n:]+):", source)
        if not cond:
            return []
        condition = cond.group(1).strip()
        # The answer may quote the comparison as the source writes it,
        # or name its bound, or write the corrected comparison.
        comparisons = [condition, re.sub(r"\s+", " ", condition)]
        bound = re.search(r"len\(items\)\s*-\s*(\d+)", condition)
        if bound:
            comparisons.append(re.compile(r"len\(items\)\s*-\s*" + bound.group(1) + r"(?!\d)", re.I))
        comparisons += corrected_comparisons(condition)
        return [
            ("the helper that chunks the list", [Ident(match.group(1))]),
            ("the comparison that is off by one", comparisons),
        ]

    if task == "find-slow-loop":
        # The method that walks the records, and the data structure that
        # would replace the walk.
        source = read(os.path.join(workdir, "svc", "store.py"))
        # The method that calls the scan, which is the one the task
        # asks about: the constructor does not look anything up.
        method = ""
        for candidate in re.findall(r"^    def (\w+)\(", source, re.M):
            if candidate == "__init__":
                continue
            body = source.split("def " + candidate, 1)[1]
            if "find_all" in body or "self.records" in body:
                method = candidate
                break
        if not method:
            return []
        return [
            ("the method that walks the records", [Ident(method)]),
            ("the data structure that would replace the walk", ["dict", "dictionary", "map", "hash map", "hashtable", "hash table"]),
        ]

    if task == "find-version-mismatch":
        # The version the packaging metadata declares, the version the
        # package declares, and the file that is out of step with the
        # changelog.
        metadata = read(os.path.join(workdir, "pyproject.toml"))
        package = read(os.path.join(workdir, "src", "shop", "__init__.py"))
        changelog = read(os.path.join(workdir, "docs", "CHANGELOG.md"))

        meta = re.search(r'^\s*version\s*=\s*["\']([^"\']+)["\']', metadata, re.M)
        pkg = re.search(r'__version__\s*=\s*["\']([^"\']+)["\']', package)
        change = re.search(r"^##\s*([0-9][^\s]*)", changelog, re.M)
        if not meta or not pkg or not change:
            return []
        meta_version = meta.group(1)
        pkg_version = pkg.group(1)
        changelog_version = change.group(1)

        # The file whose declaration is out of step with the changelog.
        out_of_step = []
        if meta_version != changelog_version:
            out_of_step += unique_suffixes(workdir, "pyproject.toml")
        if pkg_version != changelog_version:
            out_of_step += unique_suffixes(workdir, "src/shop/__init__.py")
        if not out_of_step:
            return []
        return [
            ("the version the packaging metadata declares", [meta_version]),
            ("the version the package declares", [pkg_version]),
            ("the file that is out of step with the changelog", out_of_step),
        ]

    if task == "long-find-error-in-log":
        # The request id on the one ERROR line.
        source = read(os.path.join(workdir, "logs", "service.log"))
        for line in source.splitlines():
            if " ERROR " in line:
                match = re.search(r"request (\S+)", line)
                if match:
                    return [("the request id", [match.group(1)])]
        return []

    if task == "long-find-definition-in-tree":
        # The file that defines the symbol.
        for root, _, files in os.walk(workdir):
            for name in sorted(files):
                if not name.endswith(".py"):
                    continue
                path = os.path.join(root, name)
                if re.search(r"^def parse_manifest\(", read(path), re.M):
                    return [("the file that defines it", unique_suffixes(workdir, os.path.relpath(path, workdir)))]
        return []

    raise SystemExit(f"check_answer.py: no derivation for {task}")


def main():
    if len(sys.argv) != 4:
        print("usage: check_answer.py <session.jsonl> <workdir> <task>", file=sys.stderr)
        return 2
    log_path, workdir, task = sys.argv[1], sys.argv[2], sys.argv[3]

    facts = derive(workdir, task)
    if not facts:
        print(f"the check could not work out the answer for {task}", file=sys.stderr)
        return 1

    answer = last_answer(log_path)
    if not answer:
        print("the run recorded no answer", file=sys.stderr)
        return 1

    missing = []
    for name, values in facts:
        if not any(mentions(answer, v) for v in values if v):
            missing.append((name, values))
    if missing:
        print("the answer does not name every fact the task asked about:", file=sys.stderr)
        for name, values in missing:
            shown = [getattr(v, "pattern", v) for v in values]
            print(f"  {name}: expected one of {shown!r}", file=sys.stderr)
        print(answer[:2000], file=sys.stderr)
        return 1

    print("the answer names every fact the task asked about:")
    for name, _ in facts:
        print(f"  {name}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
