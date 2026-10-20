#!/usr/bin/env bash
#
# The task's success condition: the tests pass, every request timeout is
# 45, and the two decoy timeouts are untouched.
#
# The check greps the tree rather than importing it, so a module that
# does not parse does not hide the answer.

set -uo pipefail

workdir="${POO_WORKDIR:-.}"
cd "$workdir" || exit 1

# The tests must pass.
python3 -m unittest discover -s tests -t . -v
status=$?
if [ "$status" -ne 0 ]; then
  echo "the tests failed" >&2
  exit "$status"
fi

# Every request timeout must be 45. The settings file holds the two
# decoys, so it is left out of the search.
bad=""
for file in $(grep -rl --include='*.py' "_TIMEOUT_SECONDS = 30" shop 2>/dev/null | grep -v '^shop/settings.py$' | sort); do
  bad="$bad $file"
done
if [ -n "$bad" ]; then
  echo "these files still use a timeout of 30 seconds:" >&2
  for file in $bad; do echo "  $file" >&2; done
  exit 1
fi

# Every request timeout must be 45 somewhere.
found45=""
for file in $(grep -rl --include='*.py' "_TIMEOUT_SECONDS = 45" shop 2>/dev/null | grep -v '^shop/settings.py$' | sort); do
  found45="yes"
done
if [ -z "$found45" ]; then
  echo "no request timeout was raised to 45 seconds" >&2
  exit 1
fi

# The decoys must not have moved.
if ! grep -qE "^PAYMENT_TIMEOUT_SECONDS = 30$" shop/settings.py; then
  echo "the payment timeout was changed" >&2
  exit 1
fi
if ! grep -qE "^INDEX_TIMEOUT_SECONDS = 30$" shop/settings.py; then
  echo "the index timeout was changed" >&2
  exit 1
fi

echo "every request timeout is 45, the decoys are untouched, and the tests pass"
