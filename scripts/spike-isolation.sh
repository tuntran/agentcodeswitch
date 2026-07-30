#!/usr/bin/env bash
# Phase 1 go/no-go gate: prove that two CLAUDE_CONFIG_DIR values give two fully
# separate Keychain credentials.
#
#   ./scripts/spike-isolation.sh gate [--single-account] [--witness DIR] [profile...]
#   ./scripts/spike-isolation.sh relogin <profile>          # step 7g probe
#
# --witness DIR names an already-authenticated config dir belonging to a DIFFERENT
# account (for example ~/.ccs/instances/personal). It turns a one-login run into a
# full gate: the witness must still be logged in, and still be the same account,
# after every new login, and it satisfies the distinct-accounts assertion without a
# second interactive login.
#
# The logins are interleaved with the assertions on purpose, because the
# intermediate states are what actually prove binding:
#
#   before logging a profile in  -> it must report logged OUT
#                                   (if CLAUDE_CONFIG_DIR were ignored, or the
#                                   credential were global, it would already be in)
#   after logging a profile in   -> exactly one new Keychain item, named
#                                   sha256(literal)[0:8], and every previously
#                                   authenticated profile must STILL be logged in
#                                   (proves writes are namespaced too, not just reads)
#
# Those two checks need only ONE account. --single-account therefore runs the whole
# mechanism check and skips just the "N distinct accounts" assertion, which is the
# only part that genuinely needs a second login.
#
# Runs against the real profile dirs (~/.acs/profiles/{per,com}) with the canonical
# literal, so a passing gate leaves usable credentials behind.
#
# Never prints a full email address: assertions are booleans, display is redacted.
set -euo pipefail

ACS_HOME="${ACS_HOME:-$HOME/.acs}"
SERVICE_PREFIX="Claude Code-credentials"
REPORT_DIR="$(cd "$(dirname "$0")/.." && pwd)/plans/reports"

die() {
  echo "spike: $*" >&2
  exit 1
}

# Canonical literal: absolute, no trailing slash, as produced by `pwd -L`. This exact
# string is what gets hashed into the Keychain service name, so it must be built the
# same way here and in internal/profile.NewConfigDirLiteral.
#
# The directory has to exist, because `cd` is what resolves the literal. A missing one
# is reported here rather than letting a bare `cd:` error reach the user.
literal_for() {
  [ -d "$1" ] || die "no such config dir: $1"
  (cd "$1" && pwd)
}

hash8_for() {
  printf '%s' "$1" | shasum -a 256 | cut -c1-8
}

# Attributes only -- `dump-keychain` without -d never touches secrets, so this stays
# prompt-free.
list_services() {
  security dump-keychain 2>/dev/null |
    sed -n "s/.*\"svce\"<blob>=\"\($SERVICE_PREFIX[^\"]*\)\".*/\1/p" |
    sort -u
}

redact_email() {
  local e="$1"
  [ -n "$e" ] || { printf '(none)'; return; }
  printf '%s***@%s' "${e:0:1}" "${e#*@}"
}

claude_in() {
  local literal="$1"
  shift
  env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u CLAUDE_CODE_OAUTH_TOKEN \
    -u ANTHROPIC_BASE_URL -u CLAUDE_CODE_USE_BEDROCK -u CLAUDE_CODE_USE_VERTEX \
    -u CLAUDE_CODE_USE_FOUNDRY -u ANTHROPIC_BEDROCK_BASE_URL \
    -u ANTHROPIC_VERTEX_PROJECT_ID \
    CLAUDE_CONFIG_DIR="$literal" claude "$@"
}

logged_in_at() {
  local literal="$1" out
  out="$(claude_in "$literal" auth status --json 2>/dev/null || true)"
  [ -n "$out" ] || { printf 'false'; return; }
  jq -r '.loggedIn // false' <<<"$out" 2>/dev/null || printf 'false'
}

email_at() {
  local literal="$1" out
  out="$(claude_in "$literal" auth status --json 2>/dev/null || true)"
  [ -n "$out" ] || return 0
  jq -r '.email // empty' <<<"$out" 2>/dev/null || true
}

# fingerprint_of identifies an account without recording it. Enough to tell two
# accounts apart or spot one being swapped, and safe to paste into a report.
fingerprint_of() {
  printf '%s' "$1" | shasum -a 256 | cut -c1-8
}

cmd_gate() {
  local single=0 witness="" want_witness=0
  local -a profiles=()
  local arg
  for arg in "$@"; do
    if [ "$want_witness" -eq 1 ]; then
      witness="$arg"
      want_witness=0
      continue
    fi
    case "$arg" in
      --single-account) single=1 ;;
      --witness) want_witness=1 ;;
      -*) die "unknown option $arg" ;;
      *) profiles+=("$arg") ;;
    esac
  done
  [ "$want_witness" -eq 0 ] || die "--witness needs a directory"
  [ ${#profiles[@]} -gt 0 ] || profiles=(per com)

  command -v claude >/dev/null || die "claude not on PATH"
  command -v jq >/dev/null || die "jq not on PATH"

  # The witness is an existing credential that must survive untouched. It is the
  # write-side half of isolation: reads are covered by a fresh dir reporting logged
  # out, but only an existing credential surviving a new login proves that writes are
  # namespaced too.
  local witness_literal="" witness_fp=""
  if [ -n "$witness" ]; then
    [ -d "$witness" ] || die "--witness $witness is not a directory"
    witness_literal="$(literal_for "$witness")"
    if [ "$(logged_in_at "$witness_literal")" != "true" ]; then
      # The default dir is the trap here. Leaving CLAUDE_CONFIG_DIR unset uses the
      # unsuffixed service name; setting it to that same path takes the hashed one,
      # which does not exist. So ~/.claude reads as logged out and can never witness.
      if [ "$witness_literal" = "$HOME/.claude" ]; then
        die "--witness cannot be ~/.claude. Claude Code only uses the unsuffixed
keychain item when CLAUDE_CONFIG_DIR is UNSET; setting it to that same path selects
the hashed name instead, which does not exist. Use a dir that was set up with
CLAUDE_CONFIG_DIR, such as ~/.ccs/instances/<name>."
      fi
      die "--witness $witness_literal is not logged in; it cannot witness anything"
    fi
    witness_fp="$(fingerprint_of "$(email_at "$witness_literal")")"
    echo "witness  $witness_literal"
    echo "  keychain    $SERVICE_PREFIX-$(hash8_for "$witness_literal")"
    echo "  account     fingerprint $witness_fp (value not recorded)"
    echo
  fi

  echo "== keychain before =="
  local before
  before="$(list_services)"
  echo "${before:-(none)}"
  echo

  local -a literals=() expected=() emails=()
  local p dir literal
  for p in "${profiles[@]}"; do
    dir="$ACS_HOME/profiles/$p"
    mkdir -p "$dir"
    literal="$(literal_for "$dir")"
    literals+=("$literal")
    expected+=("$SERVICE_PREFIX-$(hash8_for "$literal")")
    echo "profile $p"
    echo "  literal  $literal"
    echo "  expected $SERVICE_PREFIX-$(hash8_for "$literal")"
  done
  echo

  if [ "$single" -eq 1 ]; then
    echo "Mode: single account. Use the SAME account for every login below."
    echo "The distinct-accounts assertion is skipped; everything else still runs."
  else
    echo "Mode: multi account. Use a DIFFERENT account for each login below."
  fi
  echo

  # A dir that is already authenticated makes the logged-out precondition
  # meaningless, so refuse rather than quietly weakening the gate.
  local i
  for i in "${!profiles[@]}"; do
    if [ "$(logged_in_at "${literals[$i]}")" = "true" ]; then
      die "profile ${profiles[$i]} is already logged in. Reset it with:
  CLAUDE_CONFIG_DIR='${literals[$i]}' claude auth logout
then run the gate again."
    fi
  done

  local fail=0 seen
  seen="$before"

  for i in "${!profiles[@]}"; do
    p="${profiles[$i]}"
    literal="${literals[$i]}"

    # ── Precondition: not logged in yet. ────────────────────────────────────────
    # For the first profile this is trivial. For every later one it is the core
    # isolation assertion: an earlier login must not have made this dir usable.
    if [ "$(logged_in_at "$literal")" = "true" ]; then
      echo "FAIL $p: reports logged in BEFORE its own login -- credentials are shared"
      fail=1
    else
      echo "PASS $p: reports logged out before its own login"
    fi

    echo
    echo "== login $p =="
    # --claudeai is mandatory: --console yields API-billing auth with no user:profile
    # scope, which makes the quota endpoint return an empty 200.
    claude_in "$literal" auth login --claudeai || die "login failed for $p"
    echo

    # ── One new item, under the name computed from the literal. ─────────────────
    local after new_items new_count
    after="$(list_services)"
    new_items="$(comm -13 <(echo "$seen") <(echo "$after") || true)"
    new_count="$(printf '%s' "$new_items" | grep -c . || true)"

    if [ "$new_count" -eq 1 ] && [ "$new_items" = "${expected[$i]}" ]; then
      echo "PASS $p: exactly 1 new keychain item, ${expected[$i]}"
    else
      echo "FAIL $p: expected exactly 1 new item named ${expected[$i]}, got $new_count:"
      echo "${new_items:-(none)}"
      fail=1
    fi
    seen="$after"

    # ── No clobber: every earlier profile must still be authenticated. ──────────
    # This is what proves writes are namespaced, not only reads.
    local j
    for ((j = 0; j < i; j++)); do
      if [ "$(logged_in_at "${literals[$j]}")" = "true" ]; then
        echo "PASS ${profiles[$j]}: still logged in after logging in $p"
      else
        echo "FAIL ${profiles[$j]}: logged OUT by logging in $p -- credentials collide"
        fail=1
      fi
    done

    # The witness has to survive as the same account, not merely as "logged in":
    # a silent swap would leave loggedIn true while the credential was replaced.
    if [ -n "$witness_literal" ]; then
      if [ "$(logged_in_at "$witness_literal")" != "true" ]; then
        echo "FAIL witness: logged OUT by logging in $p -- credentials collide"
        fail=1
      elif [ "$(fingerprint_of "$(email_at "$witness_literal")")" != "$witness_fp" ]; then
        echo "FAIL witness: still logged in but the account changed -- credential was overwritten"
        fail=1
      else
        echo "PASS witness: same account still logged in after logging in $p"
      fi
    fi

    emails+=("$(email_at "$literal")")

    # <dir>/.claude.json must carry this profile's own identity.
    local acct
    acct="$(jq -r '.oauthAccount.emailAddress // empty' \
      "$literal/.claude.json" 2>/dev/null || true)"
    if [ -n "$acct" ] && [ "$acct" = "${emails[$i]}" ]; then
      echo "PASS $p: .claude.json oauthAccount matches auth status ($(redact_email "$acct"))"
    else
      echo "FAIL $p: .claude.json oauthAccount missing or does not match auth status"
      fail=1
    fi
    echo
  done

  # Attributes-only lookup must not raise a dialog.
  for i in "${!profiles[@]}"; do
    if security find-generic-password -s "${expected[$i]}" >/dev/null 2>&1; then
      echo "PASS ${profiles[$i]}: attributes-only lookup works without a prompt"
    else
      echo "FAIL ${profiles[$i]}: find-generic-password (attributes) failed"
      fail=1
    fi
  done

  # ── Distinct accounts.
  #
  # Tracked separately from `fail`, because the two mean different things and demand
  # different actions. A mechanism failure above means the design is wrong: stop.
  # Not having used two accounts means the cross-account case was never exercised:
  # everything proven still holds, and the coverage gap closes whenever a second
  # account is convenient. Reporting both as "GATE FAIL" would send someone back to
  # re-brainstorm a design that just passed every check.
  local -a fingerprints=()
  for i in "${!emails[@]}"; do
    fingerprints+=("$(fingerprint_of "${emails[$i]}")")
  done
  [ -z "$witness_fp" ] || fingerprints+=("$witness_fp")

  local want_distinct uniq_accounts uncovered=0
  want_distinct=${#fingerprints[@]}
  uniq_accounts="$(printf '%s\n' "${fingerprints[@]}" | sort -u | grep -c . || true)"

  if [ "$uniq_accounts" -ge 2 ] && [ "$uniq_accounts" -eq "$want_distinct" ]; then
    echo "PASS: $uniq_accounts distinct accounts across $want_distinct credentials"
  else
    uncovered=1
    echo "NOT COVERED: only $uniq_accounts distinct account across $want_distinct credentials"
    echo "  The same account was used more than once, so the cross-account case was"
    echo "  never exercised. This is not a mechanism failure -- see the summary below."
  fi

  echo
  echo "== keychain after =="
  list_services
  echo

  if [ "$fail" -ne 0 ]; then
    echo "GATE FAIL -- a mechanism check failed. Do not build on this."
    echo "See plan.md phase 1 risk table; the answer is to stop, not to work around it."
    return 1
  fi

  echo "GATE PASS (mechanism) -- every isolation check passed."
  echo "  literal -> keychain name, no fallback to another credential, one new item per"
  echo "  login, and existing credentials survive a new login unchanged."
  echo "Keep $ACS_HOME/profiles as-is for phase 2/3."

  if [ "$uncovered" -eq 1 ]; then
    echo
    echo "OPEN: the cross-account case is untested. Nothing above depends on it -- the"
    echo "  keychain name is derived from the literal, and a token is opaque to that"
    echo "  derivation -- but it stays open until two accounts have been seen at once."
    echo "  To close it: log a second profile in with a different account, e.g."
    echo "    ./scripts/spike-isolation.sh gate com"
    echo "  Note: \`acs ls\` warning that profiles share an account is correct behaviour."
  fi
  echo "Write findings to $REPORT_DIR (redact emails)."
  return 0
}

# Step 7g: what does `claude auth login` do on an already-authenticated dir?
# Phase 3 picks one of three branches (re-login / logout-first / no-op) from this
# observation instead of guessing.
cmd_relogin() {
  local p="${1:-}"
  [ -n "$p" ] || die "usage: spike-isolation.sh relogin <profile>"

  local dir="$ACS_HOME/profiles/$p"
  # This probe only means something on a profile that is already authenticated, which
  # is what the gate produces. Run in the wrong order it would just be an ordinary
  # first login.
  [ -d "$dir" ] || die "profile $p does not exist yet ($dir).
Run the gate first, which creates and authenticates it:
  ./scripts/spike-isolation.sh gate --witness \"\$HOME/.ccs/instances/personal\" $p"

  local literal
  literal="$(literal_for "$dir")"
  if [ "$(logged_in_at "$literal")" != "true" ]; then
    die "profile $p exists but is not logged in, so there is nothing to re-login.
Run the gate first:
  ./scripts/spike-isolation.sh gate --witness \"\$HOME/.ccs/instances/personal\" $p"
  fi

  echo "profile $p is already logged in; running auth login again."
  echo "Record: does it re-login over the top, demand logout first, or no-op?"
  echo
  set +e
  claude_in "$literal" auth login --claudeai
  echo "exit code: $?"
  set -e
}

case "${1:-gate}" in
  gate)
    shift || true
    cmd_gate "$@"
    ;;
  relogin)
    shift
    cmd_relogin "$@"
    ;;
  *) die "unknown command ${1}; use gate|relogin" ;;
esac
