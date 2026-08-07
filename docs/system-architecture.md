# System architecture

## Shape

Two binaries, one Go module, one business layer.

```
frontend/  ──(Wails bindings)──►  app.go  ──┐
                                            ├──►  internal/{config,profile,wrap,login,quota,doctor}
                          cmd/acs/main.go ──┘
```

`internal/` is pure Go with zero UI dependency, enforced by
`scripts/check-no-wails-in-internal.sh` in `make check`. CLI and UI are two
consumers of the same code paths, so they cannot drift: if the UI shows a different
quota than `acs quota`, that is a bug, not a UI feature.

`app.go` holds bindings only. Business rules such as "utilization above 80% is
`attention`" belong in `internal/quota`, never in a binding.

## Layout is a constraint, not a preference

`main.go` carries `//go:embed all:frontend/dist`. Go resolves embed paths relative
to the containing `.go` file and forbids `..`, so the Wails entry must sit at the
repo root with `frontend/` as its sibling. `cmd/acs-ui/main.go` cannot embed
`frontend/dist`. The CLI lives under `cmd/acs/` and builds without Node.

## Credential isolation

Claude Code derives its Keychain service name from the literal string in
`CLAUDE_CONFIG_DIR`:

```
service = "Claude Code-credentials"                     when unset (~/.claude)
service = "Claude Code-credentials-" + hex(sha256(NFC(literal)))[0:8]   otherwise
```

Verified empirically: `~/.ccs/instances/personal` hashes to `79a4535b`, matching the
real Keychain item Claude Code created for it.

`acs` therefore does not need to manage credentials at all. It gives each profile a
config dir, delegates authentication to `claude auth login`, and lets Claude Code
own the secret. `acs` reads Keychain attributes (`ls`, `doctor`), reads the secret
only for `quota` and `doctor --deep`, and deletes items on `rm`. It never writes
one.

### The literal is load-bearing

The hash input is the literal *as passed*, not a resolved path. `acs` canonicalizes
once at `acs add` (absolute, `filepath.Clean`, no trailing slash, NFC) and freezes
the result in `config.json`. It is never recomputed.

Normalizing is safe only because `acs` controls both ends: the string set into the
environment is the same string that gets hashed, whatever Claude Code does
internally.

Getting this wrong fails silently and misleadingly. A wrong literal means Claude
Code finds no credential, treats the dir as new, and prompts for login — producing
two credentials for one account, where `acs quota` reads one item while the running
session uses the other. The symptom presents as "I got randomly logged out", which
sends you debugging OAuth and networking for a day.

Three independent defences, none sufficient alone:

1. `profile.ConfigDirLiteral` is a struct with an unexported field, so bare
   conversion from a string is impossible. A `type X string` would not help —
   `X(rawString)` compiles from any package, and that is exactly what a hurried
   change reaches for.
2. The literal is persisted, never recomputed.
3. `acs doctor` reports a mismatch and finds orphaned Keychain items. It never
   repairs anything: a silent repair loses the old credential with nobody noticing.

## The config dir holds more than credentials

`CLAUDE_CONFIG_DIR` relocates the entire configuration directory, not just the
credential lookup. A profile created without further work therefore starts with no
skills, agents, hooks, plugins or settings — a bare Claude Code. "Switch account"
is not what anyone means by that.

The split is by ownership:

| Belongs to the account — per profile | Belongs to the tool — shared |
|---|---|
| `.claude.json` (holds `oauthAccount`) | `skills/` |
| `projects/`, `todos/`, `history.jsonl` | `agents/`, `commands/` |
| `sessions/`, `session-states/`, `shell-snapshots/` | `hooks/`, `rules/` |
| caches | `plugins/`, `settings.json` |

Shared assets are **symlinked** from the default config dir. One copy, no drift, and
editing either path edits the same file. Copying was never an option: skills alone
run to hundreds of megabytes, and a copy diverges the moment either side changes.

`profile.LinkShared` runs before the first login, which also stops
`claude auth login` from creating a real `settings.json` that would compete with the
link. `assertShareable` refuses to link anything on the per-profile list, because
sharing `projects/` would merge two accounts' transcripts and sharing `.claude.json`
would give both profiles one identity.

Nothing is ever deleted. Real content already in the way is reported and left alone;
`acs link --replace` renames it aside first.

### Two consequences worth knowing

**Sharing `settings.json` shares whatever is in it**, including any secret such as an
MCP `Authorization` header. For one person's own profiles that is usually the point.
For a profile meant to stay separate from personal tooling it may not be, and the
remedy is to keep a real `settings.json` in that profile — `acs link` will report it
as blocked and leave it alone.

**MCP servers added with `claude mcp add` land in `.claude.json`**, which is
per-profile and cannot be shared, because that same file holds `oauthAccount`. To
share an MCP server across profiles, declare it under `mcpServers` in
`settings.json` instead.

### Onboarding

A fresh config dir has no `hasCompletedOnboarding`, so the first run opens the
first-run wizard — whose opening step is "Select login method". An authenticated
profile then looks like a failed login, and the obvious response is to log in again
when nothing is wrong. Same family of mistake as rendering an unknown quota as `0%`.

`profile.MarkOnboarded` sets that one key. It is the only thing acs writes into
`.claude.json`; the file is Claude Code's and everything else in it is read-only to
acs. `acs doctor` reports the flag rather than assuming it, so a Claude Code change
that stops honouring it surfaces as a report instead of a silent no-op.

## State on disk

```
~/.acs/
  config.json               profiles, frozen literals, cached identity
  config.lock               flock target
  cache/quota-<profile>.json  derived numbers only, never a token
  profiles/<name>/          the value of CLAUDE_CONFIG_DIR
```

`config.json` stores `dir` relative to `~/.acs` for file access, and
`configDirLiteral` as the frozen absolute string for hashing. Moving `~/.acs` keeps
`dir` valid while `configDirLiteral` goes stale — doctor catches that and tells you
to log in again.

Every `config.json` write takes `flock` on `config.lock` and does load-modify-save
inside the lock. Atomic rename alone prevents corruption but not lost updates, and
running the UI and CLI at once is the normal case. Per-profile cache files are
separate, so their races are benign and need no lock.

## Quota

```
GET https://api.anthropic.com/api/oauth/usage
Authorization: Bearer <accessToken>
anthropic-beta: oauth-2025-04-20
```

Undocumented, and there is no official alternative: `claude auth status --json`
carries no quota. When it breaks, `acs` degrades to an account switcher rather than
showing wrong numbers.

**Absence is data, never `0`.** The endpoint returns `200` with empty payload when
the token lacks the `user:profile` scope — rendering that as `0%` would tell the user
they have full quota right before they get blocked. Five distinct states, each
answering "what do I do now?":

| State | Switching still works? |
|---|---|
| `no_login` | no — there is no credential yet |
| `missing_scope` | yes |
| `expired` | yes — Claude Code refreshes on next run |
| `unavailable` | yes |
| `ok` | yes |

Tokens never reach disk: they live in memory for the duration of one fetch.
`acs` does not refresh tokens; a bad writeback could break a session that is
working fine. It does not need to: Claude Code refreshes its own token when it
starts, so the remedy for `expired` is to start Claude Code, which is what the
card's primary action does. Logging in again stays available beside it for the case
where the refresh token is dead too.

### The JSON contract, in two lines

`acs quota --json` and the Wails bindings share one shape, and two invariants make it
safe to consume:

```
fiveHour/sevenDay are non-null  <=>  state == "ok"
lastKnown is non-null            =>  state != "ok"
```

So "draw a bar" is exactly `state == "ok"` — one condition, not a combination. The
previous reading is still worth having when a refresh fails, since the weekly window
being at 91% ten minutes ago changes what you do next, but it lives under `lastKnown`
where nothing can mistake it for a live measurement. Reaching it means asking for it
by name.

When `state` is `ok` and `stale` is also true, the numbers came from cache and
`fetchedAt` gives their age. That is the `acs ls` case: showing the cached number is
the whole point, and the label is what keeps it honest.

### Caching differs by consumer

| Consumer | Within TTL | Past TTL |
|---|---|---|
| `acs quota` | cache | **synchronous** fetch; on failure print the old value labelled stale |
| `acs ls` | cache | cache, labelled stale — never any network call |
| UI | cache first for the initial paint, then **fetch anyway** — the ticker owns the cadence | same |

Stale-while-revalidate in the CLI would be a bug that no obvious test catches: a
CLI process lives a few hundred milliseconds, so the background goroutine dies
before it can write, and the cache stays stale forever unless the user discovers
`--force`.

The UI is the one consumer the TTL does not apply to, and that is deliberate. Its
ticker already paces the requests, so checking the TTL as well meant a reading had to
outlive both the interval and the TTL before it was replaced — which made the numbers
on screen up to twice the interval old, and let a second account be skipped for a
whole cycle whenever its previous fetch happened to land a moment late. Two guards
remain on that path: the endpoint's own `Retry-After`, and a floor of 30s so
remounting the dashboard repeatedly cannot turn into a burst of full passes.

An explicit `--force`, and every button on a quota card, skips both the TTL and the
backoff. A person looking at a stalled card and clicking retry means it.

## Process model

`internal/wrap.Exec` uses `syscall.Exec` to replace the current process, which is
how signals, TTY behaviour and exit codes pass through unchanged. That makes it
CLI-only: calling it from the UI would kill the app. `go list` cannot detect that
direction, so the doc comment on `Exec` is the only guard.

Before exec, nine inherited variables are stripped, because they take precedence
over stored credentials and would silently defeat the profile switch:

```
ANTHROPIC_API_KEY  ANTHROPIC_AUTH_TOKEN  CLAUDE_CODE_OAUTH_TOKEN
ANTHROPIC_BASE_URL  ANTHROPIC_BEDROCK_BASE_URL  ANTHROPIC_VERTEX_PROJECT_ID
CLAUDE_CODE_USE_BEDROCK  CLAUDE_CODE_USE_VERTEX  CLAUDE_CODE_USE_FOUNDRY
```

Login is different: `internal/login` uses `exec.Command` with inherited stdio,
because `acs` must keep running afterwards to verify the result. That also means
the UI cannot call it — a Wails `.app` launched from Finder has no TTY, so an
interactive login would hang. The UI opens Terminal.app running `acs login <name>`
and polls for completion instead.

## Platform

macOS only in v1, behind a narrow `credStore` interface with a `_darwin.go`
implementation that shells out to `security`. Porting means one new file, not a
redesign. No Linux implementation ships in v1.
