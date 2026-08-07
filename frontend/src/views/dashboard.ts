import { renderQuotaCard } from "../components/quota-card";
import { emptyState } from "../components/empty-state";
import {
  cachedQuota,
  getQuota,
  launchLoginTerminal,
  launchProfileTerminal,
  listProfiles,
  onQuotaUpdated,
  subscribeQuota,
} from "../bindings";
import type { ProfileView, QuotaView } from "../types";

// The dashboard paints from cache immediately, then lets the Go side refresh in the
// background. That ordering is safe here and not in the CLI: this process lives long
// enough for a background fetch to finish and report back.

// Module-level, not per-render. The event listener is registered exactly once for
// the lifetime of the window: Wails has no EventsOff-by-handler, so re-registering
// on every navigation would leave every old handler attached and repaint the same
// card N times.
let listenerWired = false;

/** The grid currently on screen, or null when another view is showing. */
let activeGrid: HTMLElement | null = null;
const cards = new Map<string, HTMLElement>();

export function renderDashboard(container: HTMLElement): void {
  container.replaceChildren(heading());

  const grid = document.createElement("div");
  grid.className = "quota-grid";
  container.append(grid);

  // A fresh mount owns the grid; anything the old one held is gone from the DOM.
  activeGrid = grid;
  cards.clear();

  wireListener();
  void load(container, grid);
}

function wireListener(): void {
  if (listenerWired) return;
  listenerWired = true;
  onQuotaUpdated((quota) => replaceCard(quota));
}

function heading(): HTMLElement {
  const header = document.createElement("header");
  header.className = "page-header";

  const title = document.createElement("h1");
  title.textContent = "Quota";
  header.append(title);

  const note = document.createElement("p");
  note.className = "page-header__note";
  note.textContent =
    "Utilisation per account. A dash means acs does not know the number — never that the window is empty.";
  header.append(note);
  return header;
}

async function load(container: HTMLElement, grid: HTMLElement): Promise<void> {
  let profiles: ProfileView[];
  try {
    profiles = await listProfiles();
  } catch (err) {
    grid.replaceChildren(errorNote(err));
    return;
  }

  if (profiles.length === 0) {
    container.append(
      emptyState({
        headline: "No accounts yet.",
        detail:
          "Add one to switch between Claude accounts with separate credentials.",
        actionLabel: "Go to Accounts",
        onAction: () => {
          location.hash = "#accounts";
        },
      }),
    );
    return;
  }

  for (const profile of profiles) {
    const quota = await cachedQuota(profile.name);
    // A slower navigation could have replaced the grid while this was awaiting.
    if (activeGrid !== grid) return;
    const card = buildCard(quota);
    cards.set(profile.name, card);
    grid.append(card);
  }

  try {
    // Restarts the Go-side loop rather than adding one, so navigating back and
    // forth does not multiply the request rate.
    await subscribeQuota();
  } catch (err) {
    if (activeGrid === grid) grid.append(errorNote(err));
  }
}

/** Swaps one card in place, ignoring results for a grid no longer on screen. */
function replaceCard(quota: QuotaView): void {
  if (!activeGrid) return;
  const existing = cards.get(quota.name);
  if (!existing || !existing.isConnected) return;
  const replacement = buildCard(quota);
  existing.replaceWith(replacement);
  cards.set(quota.name, replacement);
}

function buildCard(quota: QuotaView): HTMLElement {
  return renderQuotaCard(quota, {
    onLogin: (name) => {
      void launchLoginTerminal(name).catch((err) => {
        activeGrid?.append(errorNote(err));
      });
    },
    onLaunch: (name) => {
      void launchProfileTerminal(name).catch((err) => {
        activeGrid?.append(errorNote(err));
      });
    },
    onRetry: (name) => {
      void getQuota(name, true).then(replaceCard, (err) => {
        activeGrid?.append(errorNote(err));
      });
    },
    // Returned rather than swallowed: the card uses it to drive the button's
    // in-flight state and to restore it if this rejects.
    onRefresh: (name) =>
      getQuota(name, true).then(replaceCard, (err) => {
        activeGrid?.append(errorNote(err));
        throw err;
      }),
  });
}

function errorNote(err: unknown): HTMLElement {
  const banner = document.createElement("div");
  banner.className = "banner banner--fault";
  const text = document.createElement("p");
  text.className = "banner__text";
  text.textContent = String(err);
  banner.append(text);
  return banner;
}
