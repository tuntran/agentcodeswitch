import type { QuotaState, QuotaView, WindowView } from "../types";

/** One of the four states that carry no numbers. */
type FailedState = Exclude<QuotaState, "ok">;

// The quota card is the one place where getting it wrong actively harms the user.
//
// The usage endpoint answers HTTP 200 with an empty body when the token lacks the
// user:profile scope. A card that maps "no number" to 0% then tells someone their
// whole window is free, right before it blocks them mid-task. So the bar element is
// built only for a non-null window on an `ok` result -- not "a bar with width 0".
// Zero is unrepresentable here rather than merely avoided.
//
// This module imports nothing from Wails on purpose: it stays a pure DOM function so
// the tests can render every state without the generated bindings.

/** How each non-ok state is presented, and what the user can do about it. */
const statePresentation: Record<
  FailedState,
  { variant: "info" | "attention" | "fault"; action: string }
> = {
  no_login: { variant: "info", action: "Log in" },
  missing_scope: { variant: "fault", action: "Log in again" },
  expired: { variant: "attention", action: "Log in again" },
  unavailable: { variant: "info", action: "Retry" },
};

export interface QuotaCardHandlers {
  onLogin?: (profile: string) => void;
  onRetry?: (profile: string) => void;
}

/** Builds the card for one profile. */
export function renderQuotaCard(
  quota: QuotaView,
  handlers: QuotaCardHandlers = {},
): HTMLElement {
  const card = document.createElement("article");
  card.className = "card quota-card";
  card.dataset.profile = quota.name;
  card.dataset.state = quota.state;

  const header = document.createElement("header");
  header.className = "quota-card__header";

  const title = document.createElement("h3");
  title.textContent = quota.name;
  header.append(title);

  if (quota.state === "ok" && quota.stale) {
    // A stale number shown as current is a lie of omission. Label it and say when.
    header.append(
      chip(`stale · fetched ${formatTime(quota.fetchedAt)}`, "neutral"),
    );
  }
  card.append(header);

  if (quota.state === "ok") {
    card.append(
      windowRow("5h window", quota.fiveHour),
      windowRow("weekly", quota.sevenDay),
    );
    return card;
  }

  card.append(renderFailure(quota, quota.state, handlers));
  return card;
}

/**
 * Renders the banner and action for a state that carries no numbers.
 *
 * The narrowed state is passed separately: TypeScript narrows the `quota.state`
 * expression after the guard above, but QuotaView is not a discriminated union, so
 * the object itself stays wide.
 */
function renderFailure(
  quota: QuotaView,
  state: FailedState,
  handlers: QuotaCardHandlers,
): HTMLElement {
  const presentation = statePresentation[state];
  const banner = document.createElement("div");
  banner.className = `banner banner--${presentation.variant}`;

  const text = document.createElement("p");
  text.className = "banner__text";
  text.textContent = quota.message;
  banner.append(text);

  if (quota.retryAfter) {
    banner.append(chip(`retry at ${formatTime(quota.retryAfter)}`, "neutral"));
  }

  // The previous reading, if acs ever got one. It goes in the banner rather than as
  // a bar: knowing the weekly window was at 91% ten minutes ago is useful, but it
  // must not look like a live measurement.
  if (quota.lastKnown) {
    const previous = document.createElement("p");
    previous.className = "banner__text last-known";
    previous.textContent =
      `Last known ${formatTime(quota.lastKnown.fetchedAt)}: ` +
      `5h ${snapshotValue(quota.lastKnown.fiveHour)}, ` +
      `weekly ${snapshotValue(quota.lastKnown.sevenDay)}`;
    banner.append(previous);
  }

  // Every failure gets a labelled action. "Error" on its own is a dead end.
  const button = document.createElement("button");
  button.type = "button";
  button.className = "btn btn--ghost";
  button.textContent = presentation.action;
  button.addEventListener("click", () => {
    if (state === "unavailable") {
      handlers.onRetry?.(quota.name);
    } else {
      handlers.onLogin?.(quota.name);
    }
  });
  banner.append(button);
  return banner;
}

/**
 * One labelled bar. A null window renders an absence marker and no bar at all.
 */
function windowRow(label: string, window: WindowView | null): HTMLElement {
  const row = document.createElement("div");
  row.className = "bar-row";
  row.dataset.window = label;

  const name = document.createElement("span");
  name.className = "bar-row__label";
  name.textContent = label;
  row.append(name);

  if (window === null) {
    const absent = document.createElement("span");
    absent.className = "absent";
    absent.textContent = "—";
    absent.title = "not measured";
    row.append(absent);
    return row;
  }

  const track = document.createElement("div");
  track.className = "bar-row__track";
  const fill = document.createElement("div");
  fill.className = "bar-row__fill";
  if (window.severity === "attention") {
    fill.classList.add("bar-row__fill--attention");
  }
  // Minimum 2px so 1% is still visible.
  fill.style.width = `max(2px, ${clampPercent(window.utilization)}%)`;
  track.append(fill);
  row.append(track);

  const value = document.createElement("span");
  value.className = "bar-row__value";
  value.textContent = `${clampPercent(window.utilization)}%`;
  row.append(value);

  if (window.resetsAt) {
    const resets = document.createElement("span");
    resets.className = "bar-row__resets";
    resets.textContent = `resets ${formatTime(window.resetsAt)}`;
    row.append(resets);
  }
  return row;
}

/** Renders a snapshot figure as text. Absent stays an em dash, never a zero. */
function snapshotValue(window: WindowView | null): string {
  if (window === null) return "—";
  return `${clampPercent(window.utilization)}%`;
}

function chip(text: string, tone: "neutral" | "attention"): HTMLElement {
  const el = document.createElement("span");
  el.className = `chip chip--${tone}`;
  el.textContent = text;
  return el;
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.min(100, Math.max(0, Math.round(value)));
}

/** Formats an RFC 3339 timestamp. "" means unknown, never the epoch. */
export function formatTime(value: string): string {
  if (!value) return "unknown";
  const when = new Date(value);
  if (Number.isNaN(when.getTime())) return "unknown";
  return when.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}
