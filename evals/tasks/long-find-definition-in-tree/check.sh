#!/usr/bin/env bash
#
# The task's success condition: the run changed nothing, and its answer
# names the thing the task asked about.
#
# The answer is the run's last message, which the session log holds. The
# expected answer is derived from the workdir, so this script does not
# hand the answer to a model that reads it.

set -uo pipefail

log="${POO_SESSION_LOG:-}"
workdir="${POO_WORKDIR:-.}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

fail() {
  echo "$1" >&2
  exit 1
}

# The run must not have changed anything.
changed="${POO_CHANGED_FILES:-}"
if [ -n "$changed" ]; then
  echo "the run changed these files, and the task asked it to change none:" >&2
  echo "$changed" >&2
  exit 1
fi

[ -n "$log" ] || fail "the session log is missing"
[ -f "$log" ] || fail "the session log is missing: $log"

python3 "$script_dir/check_answer.py" "$log" "$workdir" "long-find-definition-in-tree"
