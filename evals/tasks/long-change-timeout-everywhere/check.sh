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

# Every request timeout must be exactly 45. The settings file holds the
# two decoys, so it is left out of the search: the timeouts there are
# not request timeouts.
#
# Every constant is checked, not just the ones that are still 30: a
# timeout set to 40 is as wrong as one left at 30. The timeouts are
# checked before the tests, so a timeout that is wrong is reported as
# the wrong timeout and not as a failing test.
bad=""
for file in $(grep -rl --include='*.py' "_TIMEOUT_SECONDS" shop 2>/dev/null | grep -v '^shop/settings.py$' | sort); do
  values=$(grep -oE "_TIMEOUT_SECONDS[[:space:]]*=[[:space:]]*[0-9]+" "$file" | grep -oE "[0-9]+$" | sort -u)
  for value in $values; do
    if [ "$value" != "45" ]; then
      bad="$bad $file($value)"
    fi
  done
done
if [ -n "$bad" ]; then
  echo "these request timeouts are not 45 seconds:" >&2
  for entry in $bad; do echo "  $entry" >&2; done
  exit 1
fi

# There has to be at least one request timeout, or the search above
# found nothing and passed by default.
found45=""
for file in $(grep -rl --include='*.py' -E "_TIMEOUT_SECONDS[[:space:]]*=[[:space:]]*45([^0-9]|$)" shop 2>/dev/null | grep -v '^shop/settings.py$' | sort); do
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

# The tests must pass.
python3 -m unittest discover -s tests -t . -v
status=$?
if [ "$status" -ne 0 ]; then
  echo "the tests failed" >&2
  exit "$status"
fi

echo "every request timeout is 45, the decoys are untouched, and the tests pass"
