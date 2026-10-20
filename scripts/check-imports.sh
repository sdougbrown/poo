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
# What counts as standard library is the toolchain's own answer (.Standard),
# not a heuristic about dots in a path.
#
# Packages that do not exist yet, and directories that hold no non-test Go
# files, are skipped, not errors. Every "go list" call uses "-e", so a package
# that cannot be resolved is counted and reported but never aborts the run:
# the other packages are still checked and the summary is still printed.
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

violations=0
unchecked=0
unresolved_imports=0

# attributed holds the unresolved imports reported against a checked package
# that imports them directly. closure_errors holds every unresolved entry of a
# checked package's dependency closure, as "path|package". Both hold one entry
# per line.
attributed=""
closure_errors=""

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

# package_dir <import path> -- the directory of the package, inside this
# module. It need not exist.
package_dir() {
  local rel="${1#"$MODULE"}"
  echo "$ROOT/${rel#/}"
}

# package_name <import path> -- the Go package name, or the empty string when
# go list reports none: a directory that does not exist yet, one that holds
# nothing but documentation, or one whose Go files do not parse.
package_name() {
  go list -e -f '{{.Name}}' "$1" 2>/dev/null || true
}

# The transitive closure of a package's non-test imports, classified by the
# toolchain: std for standard library, dep for a dependency, err for one that
# could not be resolved. One "go list" call, one line per package.
CLOSURE_FORMAT='{{.ImportPath}}|{{if .Standard}}std{{else}}{{if .Error}}err{{else}}dep{{end}}{{end}}'

# in_set <path> <set> -- true when the path is one of the entries of the set.
# The set holds one path per line, and the path is matched between newlines,
# so it never matches a longer one.
in_set() {
  local needle="$1"
  local set="$2"
  case "
$set
" in
    *"
"$needle"
"*) return 0 ;;
  esac
  return 1
}

# check_package <import path> <mode>
check_package() {
  local pkg="$1"
  local mode="$2"

  if [ -z "$(package_name "$pkg")" ]; then
    local dir
    dir="$(package_dir "$pkg")"
    if [ ! -d "$dir" ]; then
      echo "skip: $pkg (does not exist yet)" >&2
      return 0
    fi
    # Go files that go list cannot name do not parse, and go list reports why.
    # A directory without non-test Go files has no such error to report.
    local parse_error=""
    if [ -n "$(find "$dir" -maxdepth 1 -name '*.go' ! -name '*_test.go' -print -quit)" ]; then
      parse_error="$(go list -e -f '{{if .Error}}{{.Error.Err}}{{end}}' "$pkg" 2>/dev/null || true)"
    fi
    if [ -n "$parse_error" ]; then
      echo "error: cannot check $pkg: $parse_error" >&2
      unchecked=$((unchecked + 1))
    else
      echo "skip: $pkg (no non-test Go files)" >&2
    fi
    return 0
  fi

  local closure
  closure="$(go list -e -deps -f "$CLOSURE_FORMAT" "$pkg" 2>/dev/null | sed '/^$/d')"

  # The dependencies, and the ones that could not be resolved. A dependency
  # that could not be resolved has unknown imports of its own, so it is
  # reported as an error instead of a violation.
  local deps unresolved
  deps="$(printf '%s\n' "$closure" | awk -F'|' '$2 == "dep" { print $1 }')"
  unresolved="$(printf '%s\n' "$closure" | awk -F'|' -v self="$pkg" '$2 == "err" && $1 != self { print $1 }')"

  # A package that cannot be listed at all is an error: its own imports are
  # unknown, so the rule could not be applied to it.
  local own_error
  own_error="$(go list -e -f '{{if .Error}}{{.Error.Err}}{{end}}' "$pkg" 2>/dev/null)"
  if [ -n "$own_error" ]; then
    echo "error: cannot check $pkg: $own_error" >&2
    unchecked=$((unchecked + 1))
    return 0
  fi

  # Direct imports: name the package that made the import.
  local imports
  imports="$(go list -e -f '{{join .Imports "\n"}}' "$pkg" 2>/dev/null | sed '/^$/d')"

  local import_path
  while IFS= read -r import_path; do
    [ -n "$import_path" ] || continue
    # A standard library import is not in the dependency set, and an
    # unresolvable one is reported below.
    if ! in_set "$import_path" "$deps"; then
      continue
    fi
    if in_set "$import_path" "$unresolved"; then
      continue
    fi
    if is_allowed "$import_path" "$mode"; then
      continue
    fi
    echo "check-imports: $pkg imports $import_path" >&2
    echo "  $(rule_text "$mode" "$pkg")" >&2
    violations=$((violations + 1))
  done <<< "$imports"

  # Transitive closure: an allowed package must not drag in anything else.
  local dep
  while IFS= read -r dep; do
    [ -n "$dep" ] || continue
    if [ "$dep" = "$pkg" ]; then
      continue
    fi
    if in_set "$dep" "$imports"; then
      continue
    fi
    if in_set "$dep" "$unresolved"; then
      continue
    fi
    if is_allowed "$dep" "$mode"; then
      continue
    fi
    echo "check-imports: $pkg depends on $dep" >&2
    echo "  $(rule_text "$mode" "$pkg")" >&2
    violations=$((violations + 1))
  done <<< "$deps"

  # A dependency that could not be resolved is an error, not a violation: the
  # package could not be checked, so the run continues with the others. Only
  # a direct import is reported here. Any other entry is reported after all
  # packages are checked, unless a checked package imports it directly.
  while IFS= read -r import_path; do
    [ -n "$import_path" ] || continue
    closure_errors="$closure_errors$import_path|$pkg"$'\n'
    in_set "$import_path" "$imports" || continue
    echo "error: cannot resolve $import_path (imported by $pkg)" >&2
    unresolved_imports=$((unresolved_imports + 1))
    attributed="$attributed$import_path"$'\n'
  done <<< "$unresolved"
}

check_package "$CORE" core
check_package "$MODULE/prompt" stdlib
check_package "$MODULE/decide" stdlib
check_package "$MODULE/wire/protocol" stdlib

# Each unresolved dependency that no checked package imports directly is
# reported once, against the first checked package whose closure holds it.
reported=""
while IFS='|' read -r import_path owner; do
  [ -n "$import_path" ] || continue
  if in_set "$import_path" "$attributed" || in_set "$import_path" "$reported"; then
    continue
  fi
  echo "error: cannot resolve $import_path (in the dependency closure of $owner)" >&2
  unresolved_imports=$((unresolved_imports + 1))
  reported="$reported$import_path"$'\n'
done <<< "$closure_errors"

if [ "$violations" -ne 0 ]; then
  echo "check-imports: $violations import violation(s) found" >&2
fi
if [ "$unchecked" -ne 0 ]; then
  echo "check-imports: $unchecked package(s) could not be checked" >&2
fi
if [ "$unresolved_imports" -ne 0 ]; then
  echo "check-imports: $unresolved_imports import(s) could not be resolved" >&2
fi

if [ "$violations" -ne 0 ] || [ "$unchecked" -ne 0 ] || [ "$unresolved_imports" -ne 0 ]; then
  exit 1
fi

echo "check-imports: ok"
