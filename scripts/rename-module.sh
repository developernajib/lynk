#!/usr/bin/env bash
# Renames the boilerplate's module namespace to YOUR project in one pass:
#
#   scripts/rename-module.sh github.com/you/yourapp
#
# Rewrites every go.mod module line and every import across all services,
# then tidies. Requires a CLEAN git tree so the change is one reviewable,
# revertable diff.
set -euo pipefail

OLD="github.com/developernajib/lynk"
NEW="${1:?usage: rename-module.sh <new module base, e.g. github.com/you/yourapp>}"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "error: working tree not clean; commit or discard changes first" >&2
  exit 1
fi

echo "renaming $OLD -> $NEW"

# Restrict the rewrite to tracked source/config files.
git ls-files '*.go' '*.mod' '*.yml' '*.yaml' '*.proto' | while read -r file; do
  if grep -q "$OLD" "$file"; then
    sed -i "s|$OLD|$NEW|g" "$file"
    echo "  rewrote $file"
  fi
done

for service in services/*/; do
  if [[ -f "$service/go.mod" ]]; then
    echo "== go mod tidy $service"
    (cd "$service" && go mod tidy)
  fi
done

echo "done — review with: git diff"
