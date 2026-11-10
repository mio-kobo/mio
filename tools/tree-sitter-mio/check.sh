#!/usr/bin/env bash
# Compare Tree-sitter verdicts with the official Mio parser over a set of files.
# Usage: ./check.sh [dir ...]   (default: the repository root)
# Exit status 1 when the two disagree on any file. Requires `npm ci` and Go.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
ts="$here/node_modules/.bin/tree-sitter"
[ -x "$ts" ] || { echo "run 'npm ci' in $here first" >&2; exit 2; }
roots=("$@"); [ ${#roots[@]} -gt 0 ] || roots=("$repo")
mapfile -t files < <(find "${roots[@]}" -name '*.mio' -not -path '*/node_modules/*' | sort)
official="$(cd "$repo" && go run ./tools/tree-sitter-mio/validate "${files[@]}")"
mismatch=0
while read -r verdict path; do
  if "$ts" parse -q "$path" >/dev/null 2>&1; then mine=OK; else mine=REJECT; fi
  if [ "$verdict" != "$mine" ]; then
    echo "MISMATCH official=$verdict tree-sitter=$mine $path"; mismatch=$((mismatch+1))
  fi
done <<<"$official"
echo "files=${#files[@]} mismatches=$mismatch"
[ "$mismatch" -eq 0 ]
