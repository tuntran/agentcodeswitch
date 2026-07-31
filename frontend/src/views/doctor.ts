import { runDoctor } from "../bindings";
import type { CheckView, DoctorView } from "../types";

// Doctor exists because the failure modes it catches are silent and point the wrong
// way: a drifted config dir literal presents as "I got randomly logged out". So every
// failing check here has to name the fix, not just the fault.

export function renderDoctorView(container: HTMLElement): void {
  const header = document.createElement("header");
  header.className = "page-header";
  const title = document.createElement("h1");
  title.textContent = "Doctor";
  header.append(title);

  const body = document.createElement("div");
  body.className = "stack";
  container.replaceChildren(header, body);

  const shallow = document.createElement("button");
  shallow.type = "button";
  shallow.className = "btn btn--ghost";
  shallow.textContent = "Run checks";
  shallow.addEventListener("click", () => void run(body, false));

  // --deep reads the credential itself, so macOS shows a Keychain prompt. Saying so
  // in advance is the difference between an expected dialog and an alarming one.
  const deep = document.createElement("button");
  deep.type = "button";
  deep.className = "btn btn--ghost";
  deep.textContent = "Run deep checks (macOS will ask for Keychain access)";
  deep.addEventListener("click", () => void run(body, true));

  header.append(shallow, deep);
  void run(body, false);
}

async function run(body: HTMLElement, deep: boolean): Promise<void> {
  body.replaceChildren(note(deep ? "Reading credentials…" : "Checking…"));
  let report: DoctorView;
  try {
    report = await runDoctor(deep);
  } catch (err) {
    body.replaceChildren(banner(String(err), "fault"));
    return;
  }
  body.replaceChildren(...renderReport(report));
}

function renderReport(report: DoctorView): HTMLElement[] {
  if (report.configError) {
    return [
      banner(`The acs config could not be read: ${report.configError}`, "fault"),
    ];
  }
  if (report.profiles.length === 0) {
    return [note("No profiles to check yet.")];
  }

  const out: HTMLElement[] = [
    banner(
      report.ok
        ? "Every check passed."
        : "Some checks failed. Each one below says what to do about it.",
      report.ok ? "info" : "attention",
    ),
  ];

  for (const profile of report.profiles) {
    const card = document.createElement("article");
    card.className = "card";
    card.dataset.profile = profile.name;

    const title = document.createElement("h3");
    title.textContent = profile.name;
    card.append(title);

    for (const check of profile.checks) {
      card.append(checkRow(check));
    }
    out.push(card);
  }

  if (report.orphans.length > 0) {
    // Informational, and worded that way on purpose. Any other tool that sets
    // CLAUDE_CONFIG_DIR leaves a credential acs cannot distinguish from a drifted
    // literal, so telling the user to delete it would mean telling them to break a
    // working install of something else. Matches the CLI and doctor.go's stance.
    const orphans = document.createElement("div");
    orphans.className = "banner banner--info";
    const text = document.createElement("p");
    text.className = "banner__text";
    text.textContent =
      `${report.orphans.length} stored Claude Code credential(s) no acs profile claims: ` +
      `${report.orphans.join(", ")}. These belong to another tool, or to a profile whose ` +
      `config directory literal changed. If a profile above shows a literal or keychain ` +
      `failure, this is the credential it lost. Otherwise leave them alone.`;
    orphans.append(text);
    out.push(orphans);
  }

  if (!report.deep) {
    out.push(
      note(
        "Scopes and token expiry are only checked by the deep run, which reads secrets.",
      ),
    );
  }
  return out;
}

function checkRow(check: CheckView): HTMLElement {
  const row = document.createElement("div");
  row.className = `check check--${check.status}`;
  row.dataset.check = check.name;

  const mark = document.createElement("span");
  mark.className = "check__mark";
  mark.textContent = check.status === "ok" ? "✓" : check.status === "warn" ? "!" : "✗";
  row.append(mark);

  const name = document.createElement("span");
  name.className = "check__name";
  name.textContent = check.name;
  row.append(name);

  const detail = document.createElement("span");
  detail.className = "check__detail";
  detail.textContent = check.detail;
  row.append(detail);

  if (check.status !== "ok" && check.action) {
    const action = document.createElement("p");
    action.className = "check__action";
    action.textContent = `→ ${check.action}`;
    row.append(action);
  }
  return row;
}

function banner(text: string, variant: "info" | "attention" | "fault"): HTMLElement {
  const el = document.createElement("div");
  el.className = `banner banner--${variant}`;
  const p = document.createElement("p");
  p.className = "banner__text";
  p.textContent = text;
  el.append(p);
  return el;
}

function note(text: string): HTMLElement {
  const p = document.createElement("p");
  p.className = "note";
  p.textContent = text;
  return p;
}
