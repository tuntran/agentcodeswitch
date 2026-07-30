import { renderCopyField } from "../components/copy-field";
import { emptyState } from "../components/empty-state";
import {
  cancelAdd,
  createProfileEntry,
  inspectPurge,
  launchLoginTerminal,
  listProfiles,
  pollLoginDone,
  removeProfile,
} from "../bindings";
import type { ProfileView } from "../types";

const POLL_INTERVAL_MS = 2000;
const POLL_TIMEOUT_MS = 5 * 60 * 1000;

// The table's container is held explicitly rather than looked up as
// container.lastElementChild. Once a banner has been appended, lastElementChild is
// that banner, and the next reload would render the table inside it.
let tableBody: HTMLElement | null = null;

export function renderAccounts(container: HTMLElement): void {
  const header = document.createElement("header");
  header.className = "page-header";
  const title = document.createElement("h1");
  title.textContent = "Accounts";
  header.append(title);

  const addButton = document.createElement("button");
  addButton.type = "button";
  addButton.className = "btn btn--primary";
  addButton.textContent = "Add account";
  header.append(addButton);

  const body = document.createElement("div");
  tableBody = body;
  container.replaceChildren(header, body);

  addButton.addEventListener("click", () => showAddForm(body, container));
  void reload(container);
}

async function reload(container: HTMLElement): Promise<void> {
  const body = tableBody;
  if (!body || !body.isConnected) return;
  let profiles: ProfileView[];
  try {
    profiles = await listProfiles();
  } catch (err) {
    body.replaceChildren(banner(String(err), "fault"));
    return;
  }
  if (profiles.length === 0) {
    body.replaceChildren(
      emptyState({
        headline: "No accounts yet.",
        detail:
          "Each account gets its own config directory and its own Keychain credential, so logins never mix.",
      }),
    );
    return;
  }
  body.replaceChildren(table(profiles, container));
}

function table(profiles: ProfileView[], container: HTMLElement): HTMLElement {
  const table = document.createElement("table");
  table.className = "table";
  table.innerHTML =
    "<thead><tr><th>Profile</th><th>Label</th><th>Account</th><th>Plan</th>" +
    "<th>Keychain</th><th>Config dir</th><th>Org</th><th></th></tr></thead>";

  const tbody = document.createElement("tbody");
  for (const profile of profiles) {
    tbody.append(row(profile, container));
  }
  table.append(tbody);
  return table;
}

function row(profile: ProfileView, container: HTMLElement): HTMLElement {
  const tr = document.createElement("tr");
  tr.dataset.profile = profile.name;

  tr.append(
    cell(profile.name),
    cell(profile.label || "—"),
    cell(profile.email || "—"),
    cell(profile.plan || "—"),
  );

  const keychain = document.createElement("td");
  keychain.append(
    profile.loggedIn
      ? renderCopyField(profile.keychainHash)
      : absent("not logged in"),
  );
  tr.append(keychain);

  const configDir = document.createElement("td");
  configDir.append(renderCopyField(profile.configDirLiteral));
  tr.append(configDir);

  const org = document.createElement("td");
  org.append(renderCopyField(profile.orgId, { placeholder: "—" }));
  tr.append(org);

  const actions = document.createElement("td");
  actions.className = "table__actions";
  actions.append(
    button("Remove", "btn--ghost", () =>
      showRemove(profile, container, false),
    ),
    button("Delete history…", "btn--danger", () =>
      showRemove(profile, container, true),
    ),
  );
  tr.append(actions);
  return tr;
}

/**
 * Removal dialog.
 *
 * A purge requires the profile name retyped. That is not politeness:
 * <dir>/projects/ is the only copy of that account's Claude Code transcripts, and
 * internal/profile.Remove refuses to purge without a matching name, so this UI
 * cannot degrade the confirmation into a single button even by mistake.
 */
async function showRemove(
  profile: ProfileView,
  container: HTMLElement,
  purge: boolean,
): Promise<void> {
  const panel = document.createElement("div");
  panel.className = `banner banner--${purge ? "fault" : "info"}`;

  const text = document.createElement("p");
  text.className = "banner__text";
  let typed = "";

  if (!purge) {
    text.textContent =
      `Remove ${profile.name}? Its Keychain credential and config entry go away. ` +
      `The Claude Code history stays on disk.`;
  } else {
    const plan = await inspectPurge(profile.name).catch(() => null);
    const range =
      plan && plan.oldest ? ` (${plan.oldest} to ${plan.newest})` : "";
    text.textContent =
      `Permanently delete ${plan?.dir ?? profile.name} including ` +
      `${plan?.jsonlCount ?? 0} Claude Code transcript file(s)${range}. ` +
      `This is the only copy; nothing can regenerate it. Retype the profile name to confirm.`;
  }
  panel.append(text);

  if (purge) {
    const input = document.createElement("input");
    input.className = "input";
    input.placeholder = profile.name;
    input.setAttribute("aria-label", `Retype ${profile.name} to confirm`);
    input.addEventListener("input", () => {
      typed = input.value;
    });
    panel.append(input);
  }

  panel.append(
    button("Cancel", "btn--ghost", () => panel.remove()),
    button(purge ? "Delete permanently" : "Remove", "btn--danger", () => {
      void removeProfile(profile.name, purge, purge ? typed : "").then(
        (historyKept) => {
          panel.remove();
          void reload(container).then(() => {
            if (historyKept) {
              container.append(
                banner(`History kept at ${historyKept}`, "info"),
              );
            }
          });
        },
        (err) => {
          text.textContent = String(err);
        },
      );
    }),
  );

  container.append(panel);
}

/**
 * Add-account flow.
 *
 * The profile entry is created here, but the login runs in Terminal.app. A Wails
 * .app launched from Finder has no TTY, so an interactive `claude auth login` inside
 * this process would hang at "waiting for authentication" forever.
 */
function showAddForm(body: HTMLElement, container: HTMLElement): void {
  const panel = document.createElement("div");
  panel.className = "banner banner--info";

  const label = document.createElement("p");
  label.className = "banner__text";
  label.textContent =
    "Profile name: ASCII lowercase letters, digits, - and _. A terminal window opens for the login.";
  panel.append(label);

  const nameInput = document.createElement("input");
  nameInput.className = "input";
  nameInput.placeholder = "per";
  nameInput.setAttribute("aria-label", "Profile name");

  const labelInput = document.createElement("input");
  labelInput.className = "input";
  labelInput.placeholder = "Personal (optional label)";
  labelInput.setAttribute("aria-label", "Label");
  panel.append(nameInput, labelInput);

  const status = document.createElement("p");
  status.className = "banner__text";
  panel.append(status);

  let cancelled = false;
  const cancelButton = button("Cancel", "btn--ghost", () => {
    cancelled = true;
    panel.remove();
  });

  panel.append(
    button("Create and log in", "btn--primary", () => {
      const name = nameInput.value.trim();
      status.textContent = "Creating profile…";
      void createProfileEntry(name, labelInput.value.trim()).then(
        async () => {
          try {
            await launchLoginTerminal(name);
          } catch (err) {
            status.textContent = String(err);
            await cancelAdd(name);
            return;
          }
          status.textContent =
            "Finish the login in the terminal window. Waiting…";
          const done = await waitForLogin(name, () => cancelled);
          if (cancelled) {
            await cancelAdd(name);
            return;
          }
          if (!done) {
            // Deliberately no rollback. Timing out means acs stopped watching, not
            // that the login failed -- the terminal may still be mid-authentication,
            // and deleting the directory underneath it would be destructive on a
            // guess. The profile stays; "Remove" is there if it really did fail.
            status.textContent =
              `Still waiting after 5 minutes, so acs stopped watching. Profile "${name}" is kept. ` +
              `If the login finished, reopen this tab to see it. If it failed, remove the profile.`;
            void reload(container);
            return;
          }
          panel.remove();
          void reload(container);
        },
        (err) => {
          status.textContent = String(err);
        },
      );
    }),
    cancelButton,
  );

  body.prepend(panel);
  nameInput.focus();
}

async function waitForLogin(
  name: string,
  isCancelled: () => boolean,
): Promise<boolean> {
  const deadline = Date.now() + POLL_TIMEOUT_MS;
  while (Date.now() < deadline) {
    if (isCancelled()) return false;
    if (await pollLoginDone(name).catch(() => false)) return true;
    await sleep(POLL_INTERVAL_MS);
  }
  return false;
}

const sleep = (ms: number): Promise<void> =>
  new Promise((resolve) => window.setTimeout(resolve, ms));

function cell(text: string): HTMLElement {
  const td = document.createElement("td");
  td.textContent = text;
  return td;
}

function absent(text: string): HTMLElement {
  const span = document.createElement("span");
  span.className = "absent";
  span.textContent = text;
  return span;
}

function button(
  text: string,
  variant: string,
  onClick: () => void,
): HTMLElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = `btn ${variant}`;
  el.textContent = text;
  el.addEventListener("click", onClick);
  return el;
}

function banner(text: string, variant: "info" | "fault"): HTMLElement {
  const el = document.createElement("div");
  el.className = `banner banner--${variant}`;
  const p = document.createElement("p");
  p.className = "banner__text";
  p.textContent = text;
  el.append(p);
  return el;
}
