#!/usr/bin/env bash
# Every Go file must stay <=200 non-blank lines, excluding *_test.go and
# generated Wails bindings (frontend/wailsjs/**).
set -euo pipefail

LIMIT=200
fail=0

while IFS= read -r -d '' file; do
  case "$file" in
    *_test.go) continue ;;
    ./frontend/wailsjs/*) continue ;;
  esac

  count=$(grep -c -v '^[[:space:]]*$' "$file")
  if [ "$count" -gt "$LIMIT" ]; then
    echo "check-file-size: $file has $count non-blank lines (limit $LIMIT)"
    fail=1
  fi
done < <(find . -name '*.go' -not -path './build/*' -print0)

exit $fail
