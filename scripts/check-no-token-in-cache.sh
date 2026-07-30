#!/usr/bin/env bash
# Cache files (live and fixtures) must only ever hold derived numbers, never
# the OAuth token itself (LBD #7 in plan.md).
set -euo pipefail

pattern='accessToken|refreshToken|claudeAiOauth'
fail=0

if [ -d "$HOME/.acs/cache" ] && grep -rlE "$pattern" "$HOME/.acs/cache" >/dev/null 2>&1; then
  echo "check-no-token-in-cache: token-like field found in \$HOME/.acs/cache/"
  fail=1
fi

if [ -d ./testdata ] || find . -type d -name testdata -not -path './build/*' | grep -q .; then
  while IFS= read -r -d '' file; do
    if grep -lE "$pattern" "$file" >/dev/null 2>&1; then
      echo "check-no-token-in-cache: token-like field found in $file"
      fail=1
    fi
  done < <(find . -path '*/testdata/*golden-quota*' -not -path './build/*' -print0 2>/dev/null)
fi

exit $fail
