# acs — multi-account Claude Code switcher

One machine, several Claude accounts, isolated credentials. `acs` does two things:

1. **Switch account instantly** — `acs per --dangerously-skip-permissions` launches
   `claude` with your personal auth; credentials never mix.
2. **Show real per-account quota** — know which account is about to burn its 5-hour
   or weekly window *before* it blocks you mid-task.

macOS only (credentials live in the login Keychain). Ships as a CLI plus a Wails
desktop app built from the same `internal/` packages.

## How isolation works

Claude Code namespaces its Keychain item by the *literal* string in
`CLAUDE_CONFIG_DIR`:

```
default  ~/.claude       → service "Claude Code-credentials"
custom   <literal>       → service "Claude Code-credentials-<sha256(literal)[0:8]>"
```

`acs` gives each profile its own config dir, so each profile gets its own Keychain
item. The literal is frozen at `acs add` time and never recomputed — four ways of
spelling the same directory produce four different hashes:

```
/Users/you/.acs/profiles/per       → 707f7e46
/Users/you/.acs/profiles/per/      → d918ec9f    (trailing slash)
~/.acs/profiles/per                → 1e90f476    (unexpanded)
/Users/you/.acs/profiles/./per     → aaf91607    (uncleaned)
```

A wrong literal means Claude Code cannot find the credential and asks you to log in
again — the symptom looks like a random logout, so `acs doctor` exists to catch it.

## Requirements

- macOS, Go 1.26+, Node 26+ (frontend only), `claude` CLI 2.1+, `jq`
- [Wails](https://wails.io) v2.13 for the desktop app; it installs to `~/go/bin`,
  which is not on `PATH` by default:

  ```sh
  export PATH="$PATH:$HOME/go/bin"   # add to ~/.zshrc
  ```

- `golangci-lint` v2 for `make lint`

## Layout

```
main.go        Wails entry — must stay at repo root (go:embed cannot use "..")
app.go         Wails bindings, thin: they only call internal/
frontend/      must be a sibling of main.go for the embed path to resolve
cmd/acs/       CLI
internal/      business logic, pure Go, zero UI dependency
scripts/       gate spike + make check helpers
```

`cmd/acs-ui/` is impossible: `//go:embed all:frontend/dist` is resolved relative to
the `.go` file and Go forbids `..` in embed paths.

## Build

```sh
go build ./cmd/acs      # CLI, no Node needed
wails build             # desktop .app into build/bin/
make check              # vet + lint + test + 4 repo checks
```

`make check` also runs as a pre-commit hook. The four scripts enforce invariants
the type system cannot: no Wails imports under `internal/`, ≤200 non-blank lines
per Go file, no tokens in the cache, no full email addresses in `plans/reports/`.

## Commands

| Command | What it does |
|---|---|
| `acs add <name> [--email addr]` | create profile, delegate to `claude auth login` |
| `acs login <name>` | re-authenticate an existing profile |
| `acs ls` | profiles, identity and cached quota — never hits the network |
| `acs <name> [claude args…]` | launch `claude` for that profile, args passed through |
| `acs quota [--json] [--force]` | live quota for every profile |
| `acs doctor [--deep] [--json]` | self-check; `--deep` reads secrets (Keychain prompt) |
| `acs rm <name> [--purge]` | remove profile; `--purge` also deletes history |
| `acs ui` | launch the desktop app |

Profile names are ASCII lowercase `[a-z0-9_-]`. These names are reserved:
`add login ls rm report quota ui doctor help version`.

## Your Claude Code history is irreplaceable

`~/.acs/profiles/<name>/projects/*.jsonl` is the **only** copy of your Claude Code
sessions and transcripts for that account. Nothing can regenerate it.

- `acs rm <name>` keeps that directory and prints where it lives.
- `acs rm <name> --purge` deletes it, and makes you retype the profile name first.
- Never `rm -rf ~/.acs/profiles`.

## Gate spike

`scripts/spike-isolation.sh gate` proves isolation end to end: each profile reports
logged out before its own login, exactly one new Keychain item appears under the name
computed from its literal, and every credential that was already there is still
authenticated as the same account afterwards. That last check is the write side —
reads are covered by a fresh config dir reporting `authMethod=none` while the default
dir is signed in.

A second account is not required. Point `--witness` at any config dir that is already
signed in to a different account and it satisfies the distinct-accounts assertion with
one interactive login:

```sh
./scripts/spike-isolation.sh gate --witness ~/.ccs/instances/personal per
./scripts/spike-isolation.sh gate --single-account per com   # no witness available
```

The script records booleans and `sha256(email)[0:8]` fingerprints, never an address.

## Notes

`/api/oauth/usage` is undocumented. When it fails, `acs` reports `unavailable` and
switching keeps working — it never shows `0%`, because a missing number is not zero.
`acs` reads and deletes Keychain items but never writes them: creating credentials
is `claude auth login`'s job.
