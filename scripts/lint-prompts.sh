#!/usr/bin/env bash
#
# Lint the prompt prose as agent instructions.
#
# The paths are where a session's prompt text lives: the prompt package's
# base and environment sections, each tool's own section, the catalog, the
# command's own sections, the jobs section, and the read-only conformance
# fixture. A prompt file added under one of them is linted without a
# workflow edit.
#
# Each file is linted on its own so the log names the file a finding
# belongs to; writetighter's report does not.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# The version is pinned so a rule change arrives with a change to this
# script, not with a new release.
version=v0.14.3

mapfile -t files < <(git ls-files -- \
  'prompt/*.md' \
  'tool/*/*.md' \
  'cli/*.md' \
  'cmd/poo/*.md' \
  'jobs.md' \
  'wire/conformance/testdata/readonly/prompts/*.md')

if [ "${#files[@]}" -eq 0 ]; then
  echo "lint-prompts: no prompt files found" >&2
  exit 2
fi

status=0
for file in "${files[@]}"; do
  echo "== ${file}"
  if ! go run "github.com/sdougbrown/writetighter/cmd/writetighter@${version}" \
    lint --kind agent-instruction --fail-on warning "${file}"; then
    status=1
  fi
done
exit "${status}"
