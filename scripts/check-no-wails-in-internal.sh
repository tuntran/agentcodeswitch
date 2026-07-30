#!/usr/bin/env bash
# internal/ must stay a pure-Go business layer with zero UI dependency.
# CLI (cmd/acs) and Wails UI (app.go) are the only two consumers.
set -euo pipefail

if [ ! -d ./internal ]; then
  echo "check-no-wails-in-internal: no internal/ yet, skipping"
  exit 0
fi

! go list -deps ./internal/... | grep -q 'wailsapp'
