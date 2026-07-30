#!/usr/bin/env bash
# plans/reports/*.md must never contain a full email address (spike + gate
# reports redact to a***@company.com style).
set -euo pipefail

email_re='[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}'
fail=0

if [ -d ./plans/reports ]; then
  while IFS= read -r -d '' file; do
    if grep -nE "$email_re" "$file" >/dev/null 2>&1; then
      echo "check-no-pii-in-reports: full email found in $file"
      grep -nE "$email_re" "$file"
      fail=1
    fi
  done < <(find ./plans/reports -name '*.md' -print0)
fi

exit $fail
