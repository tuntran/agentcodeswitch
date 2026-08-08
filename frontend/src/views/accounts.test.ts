import { beforeEach, describe, expect, it, vi } from "vitest";

// The view reaches Wails through src/bindings.ts and nothing else, so stubbing
// that one module keeps the generated bindings -- which exist only after
// `wails build` -- out of the test entirely.
vi.mock("../bindings", () => ({
  cancelAdd: vi.fn(),
  createProfileEntry: vi.fn(),
  inspectPurge: vi.fn(),
  launchLoginTerminal: vi.fn(),
  listProfiles: vi.fn(),
  pollLoginDone: vi.fn(),
  removeProfile: vi.fn(),
  setProfileModel: vi.fn(),
  setProfileContext1M: vi.fn(),
}));

import { listProfiles } from "../bindings";
import type { ProfileView } from "../types";
import { renderAccounts } from "./accounts";

const profile: ProfileView = {
  name: "per",
  label: "Personal",
  email: "person@example.com",
  plan: "claude_max",
  model: "",
  context1m: true,
  keychainHash: "707f7e46",
  loggedIn: true,
  orgId: "4e2622e5-e411-4e08-990a-7afa00000000",
  orgName: "Personal org",
  configDirLiteral: "/Users/example/.acs/profiles/per",
};

async function renderTable(): Promise<HTMLElement> {
  const container = document.createElement("div");
  document.body.replaceChildren(container);
  renderAccounts(container);
  await vi.waitFor(() =>
    expect(container.querySelector("table")).not.toBeNull(),
  );
  return container;
}

describe("renderAccounts", () => {
  beforeEach(() => {
    vi.mocked(listProfiles).mockResolvedValue([profile]);
  });

  it("gives the table its own horizontal overflow", async () => {
    const container = await renderTable();

    // Without this wrapper the columns widen the document itself, and the
    // page scrolls sideways with the topbar and heading going off screen.
    const scroll = container.querySelector(".table-scroll");
    expect(scroll?.querySelector(":scope > table.table")).not.toBeNull();

    // WebKit does not give a scroll container to the keyboard on its own, so
    // without these the off-screen columns would be reachable by mouse only.
    expect(scroll?.getAttribute("tabindex")).toBe("0");
    expect(scroll?.getAttribute("aria-label")).toBeTruthy();
  });

  it("keeps the action buttons inside a real table cell", async () => {
    const container = await renderTable();

    const actions = container.querySelector(".table__actions");
    // .table__actions is display:flex. On a td that cancels display:table-cell
    // and the row stops lining up with its headers, so it belongs on a child.
    expect(actions?.tagName).toBe("DIV");
    expect(actions?.parentElement?.tagName).toBe("TD");

    const headers = container.querySelectorAll("thead th").length;
    const cells = container.querySelectorAll("tbody tr:first-child > td").length;
    expect(cells).toBe(headers);
  });
});
