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

/** What clicking a card's action should do. */
type CardAction = "login" | "retry" | "launch";

/**
 * How each non-ok state is presented, and what the user can do about it.
 *
 * `primary` defaults to "login", so only the states that need something else say so.
 */
const statePresentation: Record<
  FailedState,
  {
    variant: "info" | "attention" | "fault";
    action: string;
    primary?: CardAction;
    secondaryAction?: string;
    secondaryKind?: CardAction;
  }
> = {
  no_login: { variant: "info", action: "Log in" },
  // A genuine re-login, and the one case where it has to be done a specific way:
  // the Claude subscription, not the Console.
  missing_scope: { variant: "fault", action: "Log in again" },
  // An expired access token is self-healing -- Claude Code refreshes it on start --
  // so sending someone to `acs login` asks them to redo work they do not need to.
  // The login fallback stays one button over, for when the refresh token is dead too.
  expired: {
    variant: "attention",
    action: "Open Claude Code",
    primary: "launch",
    secondaryAction: "Log in again",
    secondaryKind: "login",
  },
  unavailable: { variant: "info", action: "Retry", primary: "retry" },
};

export interface QuotaCardHandlers {
  onLogin?: (profile: string) => void;
  onRetry?: (profile: string) => void;
  /** Starts Claude Code for a profile, which is what refreshes an expired token. */
  onLaunch?: (profile: string) => void;
  /**
   * Re-reads an ok card on demand.
   *
   * Returning the pending work lets the card own the button's in-flight state, so
   * the two halves of "disable, then restore" stay in one place. On success the card
   * is replaced and the button goes with it; on failure it has to come back, because
   * a button stuck at disabled has nothing on screen explaining why.
   */
  onRefresh?: (profile: string) => Promise<unknown>;
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

  if (quota.state === "ok") {
    // The age is always shown, not only when stale. Without it a number from ten
    // minutes ago is indistinguishable from one fetched a second ago, which is what
    // made the dashboard feel stuck even when it was working.
    const label = quota.stale
      ? `stale · updated ${formatAge(quota.fetchedAt)}`
      : `updated ${formatAge(quota.fetchedAt)}`;
    const age = chip(label, "neutral");
    // The age is rendered once and goes out of date until the next event, so keep
    // the exact moment reachable on hover.
    age.title = formatTime(quota.fetchedAt);
    header.append(age, refreshButton(quota.name, handlers));
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

/** The on-demand refresh control for an ok card. */
function refreshButton(
  profile: string,
  handlers: QuotaCardHandlers,
): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "btn btn--ghost quota-card__refresh";
  button.textContent = "Refresh";
  button.addEventListener("click", () => {
    const pending = handlers.onRefresh?.(profile);
    if (!pending) return;
    button.disabled = true;
    // The label is kept rather than swapped for an ellipsis, so the button holds on
    // to its accessible name while it is busy.
    button.setAttribute("aria-busy", "true");
    void pending.catch(() => {
      button.disabled = false;
      button.removeAttribute("aria-busy");
    });
  });
  return button;
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
  const hasSecondary = Boolean(
    presentation.secondaryAction && presentation.secondaryKind,
  );
  banner.append(
    actionButton(
      presentation.action,
      presentation.primary ?? "login",
      quota.name,
      handlers,
      // Only emphasised where there is a second action to be chosen over. A lone
      // button is already unambiguous.
      hasSecondary ? "primary" : "ghost",
    ),
  );
  if (presentation.secondaryAction && presentation.secondaryKind) {
    banner.append(
      actionButton(
        presentation.secondaryAction,
        presentation.secondaryKind,
        quota.name,
        handlers,
      ),
    );
  }
  return banner;
}

/**
 * One labelled action, dispatched by kind rather than by state.
 *
 * A card with two actions weights them: when both look the same, "primary" means
 * nothing but left-to-right order, and the whole point of offering "Open Claude Code"
 * ahead of "Log in again" is that the user picks the one that is not extra work.
 */
function actionButton(
  label: string,
  kind: CardAction,
  profile: string,
  handlers: QuotaCardHandlers,
  emphasis: "primary" | "ghost" = "ghost",
): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.className = `btn btn--${emphasis}`;
  button.textContent = label;
  button.addEventListener("click", () => {
    if (kind === "retry") {
      handlers.onRetry?.(profile);
    } else if (kind === "launch") {
      handlers.onLaunch?.(profile);
    } else {
      handlers.onLogin?.(profile);
    }
  });
  return button;
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

/**
 * Formats how long ago a reading was taken. "" means unknown, never the epoch.
 *
 * Relative rather than absolute because the question a user actually has is "can I
 * trust this number right now", and "4m ago" answers it without arithmetic.
 */
export function formatAge(value: string, now: Date = new Date()): string {
  if (!value) return "unknown";
  const when = new Date(value);
  if (Number.isNaN(when.getTime())) return "unknown";

  const seconds = Math.round((now.getTime() - when.getTime()) / 1000);
  // A clock skew or a timestamp from the future reads as current rather than as a
  // negative age.
  if (seconds < 10) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
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
