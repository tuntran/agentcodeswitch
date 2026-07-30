#!/usr/bin/env bash
# Stands in for the `claude` CLI so the login flow can be tested without a
# browser. Behaviour is driven by env vars the test sets:
#
#   FAKE_LOGIN_EXIT        exit code for `auth login`         (default 0)
#   FAKE_LOGOUT_EXIT       exit code for `auth logout`        (default 0)
#   FAKE_STATUS_JSON       body for `auth status --json`      (default: logged in)
#   FAKE_STATUS_EXIT       exit code for `auth status`        (default 0)
#   FAKE_ARGS_FILE         append every invocation's args here
#   FAKE_CONFIG_DIR_FILE   append CLAUDE_CONFIG_DIR per invocation here
#   FAKE_STORE_FILE        the Go fake credential store's backing file
#   FAKE_CREATED_SERVICE   on a successful login, append this service name to
#                          FAKE_STORE_FILE -- this is how the test simulates
#                          Claude Code creating the Keychain item
set -uo pipefail

verb="${1:-} ${2:-}"

if [ -n "${FAKE_ARGS_FILE:-}" ]; then
  printf '%s\n' "$*" >>"$FAKE_ARGS_FILE"
fi

# The config dir must arrive exactly as acs set it: that literal string is what
# Claude Code hashes into the Keychain service name.
if [ -n "${FAKE_CONFIG_DIR_FILE:-}" ]; then
  printf '%s\n' "${CLAUDE_CONFIG_DIR:-<unset>}" >>"$FAKE_CONFIG_DIR_FILE"
fi

default_status='{"loggedIn":true,"authMethod":"claudeai","apiProvider":"anthropic","email":"alice@example.com","orgId":"org-1","orgName":"Org","subscriptionType":"max"}'

case "$verb" in
  "auth login")
    exit_code="${FAKE_LOGIN_EXIT:-0}"
    if [ "$exit_code" = "0" ] && [ -n "${FAKE_STORE_FILE:-}" ] && [ -n "${FAKE_CREATED_SERVICE:-}" ]; then
      printf '%s\n' "$FAKE_CREATED_SERVICE" >>"$FAKE_STORE_FILE"
    fi
    exit "$exit_code"
    ;;
  "auth status")
    printf '%s' "${FAKE_STATUS_JSON:-$default_status}"
    exit "${FAKE_STATUS_EXIT:-0}"
    ;;
  "auth logout")
    exit "${FAKE_LOGOUT_EXIT:-0}"
    ;;
  *)
    echo "fake-claude: unexpected args: $*" >&2
    exit 64
    ;;
esac
