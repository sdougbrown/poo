#!/usr/bin/env bash
#
# Enforce the poo dependency rules for non-test imports.
#
#   - core (github.com/sdougbrown/poo) may import the standard library plus
#     poo/prompt and poo/decide
#   - poo/prompt, poo/decide and poo/wire/protocol may import the standard
#     library only
#
# The check walks the import graph with "go list -deps" (the transitive closure
# of non-test imports) and also reads each package's direct imports, so a
# violation is reported against the package that actually made the import.
#
# Packages that do not exist yet are skipped, not errors.
#
# Exits non-zero with a message naming the package and the offending import.

set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

MODULE="$(go list -m -f '{{.Path}}' 2>/dev/null || true)"
if [ -z "$MODULE" ]; then
  echo "check-imports: cannot determine the module path from go.mod" >&2
  exit 1
fi

CORE="$MODULE"

failures=0

# A standard library path never has a dot in its first path element.
is_stdlib() {
  local first="${1%%/*}"
  case "$first" in
    *.* | "") return 1 ;;
    *) return 0 ;;
  esac
}

# is_allowed <import path> <mode> -- true when the import is permitted.
#
# mode "core"   -- standard library plus poo/prompt and poo/decide
# mode "stdlib" -- standard library only
is_allowed() {
  local import_path="$1"
  local mode="$2"
  case "$mode" in
    core)
      case "$import_path" in
        "$MODULE/prompt" | "$MODULE/decide") return 0 ;;
      esac
      ;;
    stdlib) ;;
  esac
  return 1
}

# rule_text <mode> -- the rule that applies, for the failure message.
rule_text() {
  case "$1" in
    core)
      echo "core may import only the standard library, $MODULE/prompt and $MODULE/decide"
      ;;
    stdlib)
      echo "$2 may import only the standard library"
      ;;
  esac
}

# package_exists <import path> -- true when the package has a directory in
# this module.
package_exists() {
  local rel="${1#"$MODULE"}"
  rel="${rel#/}"
  [ -d "$ROOT/$rel" ]
}

# check_package <import path> <mode>
check_package() {
  local pkg="$1"
  local mode="$2"

  if ! package_exists "$pkg"; then
    echo "skip: $pkg (does not exist yet)" >&2
    return 0
  fi

  local deps imports
  if ! deps="$(go list -deps "$pkg" 2>&1)"; then
    echo "error: cannot resolve dependencies of $pkg:" >&2
    printf '%s\n' "$deps" >&2
    failures=$((failures + 1))
    return 1
  fi
  if ! imports="$(go list -f '{{join .Imports "\n"}}' "$pkg" 2>&1)"; then
    echo "error: cannot list imports of $pkg:" >&2
    printf '%s\n' "$imports" >&2
    failures=$((failures + 1))
    return 1
  fi

  # Direct imports: name the package that made the import.
  local import_path
  while IFS= read -r import_path; do
    [ -n "$import_path" ] || continue
    if is_stdlib "$import_path"; then
      continue
    fi
    if is_allowed "$import_path" "$mode"; then
      continue
    fi
    echo "check-imports: $pkg imports $import_path" >&2
    echo "  $(rule_text "$mode" "$pkg")" >&2
    failures=$((failures + 1))
  done <<< "$imports"

  # Transitive closure: an allowed package must not drag in anything else.
  local dep
  while IFS= read -r dep; do
    [ -n "$dep" ] || continue
    if [ "$dep" = "$pkg" ]; then
      continue
    fi
    if is_stdlib "$dep"; then
      continue
    fi
    if is_allowed "$dep" "$mode"; then
      continue
    fi
    echo "check-imports: $pkg depends on $dep" >&2
    echo "  $(rule_text "$mode" "$pkg")" >&2
    failures=$((failures + 1))
  done <<< "$deps"
}

check_package "$CORE" core
check_package "$MODULE/prompt" stdlib
check_package "$MODULE/decide" stdlib
check_package "$MODULE/wire/protocol" stdlib

if [ "$failures" -ne 0 ]; then
  echo "check-imports: $failures import violation(s) found" >&2
  exit 1
fi

echo "check-imports: ok"
