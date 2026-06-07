#!/usr/bin/env bash
# Runs gofmt and go vet in each Go MODULE that owns a changed file. The repo
# has one module per service (no go.work), so tools must run inside the right
# directory; this script finds each file's nearest go.mod and dedupes.
set -euo pipefail

declare -A modules=()

for file in "$@"; do
  dir=$(dirname "$file")
  while [[ "$dir" != "." && "$dir" != "/" ]]; do
    if [[ -f "$dir/go.mod" ]]; then
      modules["$dir"]=1
      break
    fi
    dir=$(dirname "$dir")
  done
done

status=0
for mod in "${!modules[@]}"; do
  echo "== checking module $mod"
  unformatted=$(cd "$mod" && gofmt -l .)
  if [[ -n "$unformatted" ]]; then
    echo "gofmt needed:" >&2
    echo "$unformatted" >&2
    status=1
  fi
  (cd "$mod" && go vet ./...) || status=1
done

exit $status
