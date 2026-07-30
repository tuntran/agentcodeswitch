# acs design system

Adapted from Anchor DS. The tokens are copied here rather than referenced across
repositories: the original path is not stable, and acs needs its own additions.

## Principles acs inherits

**There is no "good" colour.** Green means interaction, never a value. A quota bar
at 3% is not "good" — it is 3%. Colour carries state, not approval.

**Absence is data, not zero.** This is the rule the whole app is built on. When the
usage endpoint fails, or the credential lacks the `user:profile` scope, acs does not
know the utilisation. Rendering that as `0%` says "your whole window is free" to
somebody about to start a long task, right before it blocks them. A missing number
renders as an em dash and a message.

**Technical data is first-class.** `configDirLiteral`, the Keychain hash, `orgId` —
these are what you paste into a bug report when credentials go missing. They get
monospace type and a copy button, not a tooltip.

## Tokens

```css
:root {
  /* Deep Teal — foundation */
  --teal-900: #01333A;
  --teal-800: #025864;   /* primary */
  --teal-700: #1F6E77;
  --teal-600: #3D848B;   /* quota bar fill */
  --teal-500: #5C9AA0;
  --teal-300: #9DC2C5;
  --teal-100: #D9E7E8;

  /* Signal Green — interaction only */
  --green-700: #00A362;  /* focus ring */
  --green-600: #00BD70;
  --green-500: #00D47E;
  --green-100: #CCF7E5;

  /* Neutrals */
  --ink-900: #0E1D22;
  --ink-700: #33454B;
  --ink-500: #5E7076;
  --ink-400: #8A989D;
  --gray-200: #D7DDE0;
  --gray-100: #E4E8EB;   /* app background */
  --gray-50:  #F2F4F6;
  --white:    #FFFFFF;

  /* State — there is no "success" token, by design */
  --state-neutral:      #5E7076;  --state-neutral-bg:   #F2F4F6;
  --state-attention:    #8A5B08;  --state-attention-bg: #FCF2DC;
  --state-attention-mark: #E8A317;
  --state-tracked:      #1A5FCC;  --state-tracked-bg:   #E4EFFE;
  --state-linked:       #025864;  --state-linked-bg:    #D9E7E8;
  --state-fault:        #C4123C;  --state-fault-bg:     #FDE7ED;
  --state-absent:       #8A989D;

  /* Typography */
  --font-sans: "Helvetica Neue", Helvetica, Arial, sans-serif;
  --font-mono: ui-monospace, "SF Mono", Menlo, "Cascadia Mono", "Roboto Mono", monospace;
  --fs-metric: 26px; --fs-h1: 28px; --fs-h2: 20px; --fs-h3: 16px;
  --fs-body: 15px; --fs-dense: 14px; --fs-small: 13px; --fs-caption: 11px;
  --fs-mono: 13px; --fs-mono-sm: 12px;

  /* Spacing (4px grid) */
  --sp-1: 4px;  --sp-2: 8px;  --sp-3: 12px; --sp-4: 16px;
  --sp-5: 20px; --sp-6: 24px; --sp-8: 32px; --sp-10: 40px; --sp-12: 48px;

  /* Density */
  --row-h: 36px; --row-pad-y: 10px; --row-pad-x: 12px;
  --card-pad: 20px;

  /* Radius */
  --r-sm: 8px; --r-md: 12px; --r-lg: 16px; --r-full: 999px;

  /* Elevation */
  --shadow-sm: 0 1px 2px rgba(14,29,34,.06);
  --shadow-md: 0 4px 12px rgba(14,29,34,.08);

  /* Motion */
  --ease: cubic-bezier(.4,0,.2,1);
  --t-fast: 120ms; --t-base: 200ms;

  /* Focus */
  --focus-ring: 2px solid var(--green-700);
  --focus-offset: 2px;
}
```

## State mapping

acs has five quota states. Four of them carry no numbers, and each maps to a
different visual treatment because each has a different fix.

| Quota state | Token | Renders as |
|---|---|---|
| `ok`, severity neutral | `state-neutral` | bar, `teal-600` fill |
| `ok`, severity attention | `state-attention` | bar with `state-attention-mark` fill + `attention` chip |
| `ok` but stale | `state-neutral` | bar, plus a `stale` chip and the fetch time |
| `no_login` | `state-absent` | em dash + "not logged in" + action button |
| `missing_scope` | `state-fault` | em dash + fault banner + action |
| `expired` | `state-attention` | em dash + attention banner + action |
| `unavailable` | `state-absent` | em dash + info banner + retry time when known |

`severity` from the API always wins over the local 80% threshold — same rule as
`internal/quota`. The UI never sets a threshold of its own.

## Components used

### Bar list (quota bar)

There are no pie, donut, or line charts. A pie implies the parts sum to a whole; a
line needs a time series the endpoint does not return. Drawing either would be
making things up.

Label 140px `--fs-dense` · bar 8px high, `--r-full`, `--teal-600`, width proportional
to 100% · value right-aligned, `tabular-nums`. Minimum bar width 2px so 1% stays
visible.

**The bar element only exists when `state === "ok"` and the window is non-null.** Not
"a bar at width 0" — no bar at all. That is what makes 0% unrepresentable rather
than merely avoided.

### Banner

Full width, `--r-md`, 12×16px padding, 16px icon, `--fs-small`, 3px left border.
Info uses `state-tracked`, attention uses `state-attention-mark`, fault uses
`state-fault`. Banners are not dismissible: they explain the data below them.

### Empty state

Centred, max 400px. Line 1 `--fs-h3` states the fact. Line 2 `--fs-small`
`--ink-500` names the possibilities when the state is ambiguous. Ghost button for a
recovery action. No illustration, no cheerful voice.

acs applies the ambiguity rule to quota: "no quota cached yet" and "the endpoint is
down" are nearly opposite meanings and must not collapse into one blank cell.

### Copy field

```
/Users/you/.acs/profiles/per  ⧉
```

`--font-mono` `--fs-mono` `--ink-700`, ghost 24px copy button immediately after,
full value in `title`. After copying, the icon becomes ✓ for 1200ms and an
`aria-live="polite"` region announces `Copied`. The button is always visible —
hover-only breaks keyboard and touch.

Used for `configDirLiteral`, `orgId`, and the Keychain hash. Never for a token, and
never for a full email address in anything that gets logged.

Long paths truncate at the **end** — the leading directories are what identify them.
(Anchor truncates ULIDs in the middle because their distinguishing bits are at the
end; acs has no ULIDs.)

### Table

36px rows, `--gray-50` dividers, `--ink-400` caption-style header with a
`--gray-200` bottom border. Numeric columns right-aligned and `tabular-nums`.
Monospace cells use the copy field.

No sortable headers in v1: with two or three profiles, sorting solves nothing.

### Metric qualifier

Every number that has a caveat carries it in place: a `5h window` or `weekly` chip
beside the value, a `stale` chip with the fetch time, `retry at HH:MM` while backing
off. Pushing the caveat into a footer is a broken design — the person reading the
number will not scroll.

## Rules for this UI specifically

1. **Never render `0%` when a number is unknown.** Four states, four components. Not
   `if (error) showGeneric()`. There is a test asserting the string `0%` is absent
   from the DOM whenever state is not `ok`.
2. **Every error state answers "what do I do now?"** A message plus an action label.
   The word "Error" alone is a dead end.
3. **`severity` from the API beats the local threshold.**
4. **Stale numbers are labelled.** Old value, `stale` chip, fetch time. Never
   presented as current.
5. **No profiles yet is an empty state**, not a blank table.
6. **Purge asks for the profile name retyped.** `<dir>/projects/` is the only copy of
   that account's Claude Code history. `internal/profile.Remove` enforces this, so
   the UI cannot weaken it to a one-button dialog even by accident.

## Not in this design system

**Dark mode.** Anchor records it as absent, so every light token would need a dark
counterpart defined and checked — real work, not a line of CSS. It is the one item
cut from v1. The app ships light-only.

Also absent: pie/donut/line charts (see Bar list), sortable table headers,
illustrations, toast notifications.
