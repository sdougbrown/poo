#!/usr/bin/env bash
#
# The task's success condition: the run changed nothing, and its answer
# names the service and the incident id the config plants, whatever the compactions cut.
#
# The answer is the run's last message, which the session log holds. The
# expected answers are derived from the workdir by the shared
# check_answer.py, so this script does not hand the answer to a model
# that reads it.

set -uo pipefail

log="${POO_SESSION_LOG:-}"
workdir="${POO_WORKDIR:-.}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
task_dir="$(cd "$script_dir/.." && pwd)"

fail() {
  echo "$1" >&2
  exit 1
}

changed="${POO_CHANGED_FILES:-}"
if [ -n "$changed" ]; then
  echo "the run changed these files, and the task asked it to change none:" >&2
  echo "$changed" >&2
  exit 1
fi

[ -n "$log" ] || fail "the session log is missing"
[ -f "$log" ] || fail "the session log is missing: $log"

python3 "$task_dir/check_answer.py" "$log" "$workdir" "$(basename "$script_dir")"
