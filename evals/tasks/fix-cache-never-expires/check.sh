#!/usr/bin/env bash
#
# The task's success condition: the fixture's own tests pass.
#
# The check runs in the workdir, with no network. It uses the system
# python3, so a fixture needs nothing but a standard library.

set -uo pipefail

cd "${POO_WORKDIR:-.}" || exit 1

python3 -m unittest discover -s tests -t . -v
status=$?

if [ "$status" -ne 0 ]; then
  echo "the tests failed" >&2
fi
exit "$status"
