import { describe, expect, it, vi } from "vitest";

import { renderQuotaCard } from "./quota-card";
import type { QuotaState, QuotaView } from "../types";

function quota(overrides: Partial<QuotaView> = {}): QuotaView {
  return {
    name: "per",
    state: "ok",
    message: "quota is current",
    stale: false,
    fetchedAt: "2026-07-30T10:00:00Z",
    retryAfter: "",
    fiveHour: null,
    sevenDay: null,
    lastKnown: null,
    ...overrides,
  };
}

/** The four non-ok states, with the message the Go side produces for each. */
const failureStates: Array<{
  state: Exclude<QuotaState, "ok">;
  message: string;
  expectAction: string;
}> = [
  {
    state: "no_login",
    message: "not logged in -- run `acs login per`",
    expectAction: "Log in",
  },
  {
    state: "missing_scope",
    message:
      "logged in, but this credential cannot read usage -- run `acs login per` and choose the Claude subscription, not the Console",
    expectAction: "Log in again",
  },
  {
    state: "expired",
    message: "access token expired -- run `acs per` or `acs login per`",
    expectAction: "Log in again",
  },
  {
    state: "unavailable",
    message: "quota unavailable -- the usage endpoint did not answer",
    expectAction: "Retry",
  },
];

describe("renderQuotaCard failure states", () => {
  for (const { state, message, expectAction } of failureStates) {
    it(`shows the message and an action for ${state}`, () => {
      const card = renderQuotaCard(quota({ state, message }));

      expect(card.dataset.state).toBe(state);
      expect(card.textContent).toContain(message);

      const action = card.querySelector("button");
      expect(action, "every failure needs an action button").not.toBeNull();
      expect(action?.textContent).toBe(expectAction);
    });
  }
});

// The single most important test in the frontend. The usage endpoint answers HTTP
// 200 with an empty body when the token lacks the user:profile scope, so a card that
// turns "unknown" into 0% tells someone their window is free right before it blocks
// them.
describe("the string 0% never appears when the state is not ok", () => {
  for (const { state, message } of failureStates) {
    it(`omits 0% for ${state}`, () => {
      const card = renderQuotaCard(quota({ state, message }));

      expect(card.textContent).not.toContain("0%");
      expect(card.querySelector(".bar-row__fill")).toBeNull();
      expect(card.querySelector(".bar-row__value")).toBeNull();
    });
  }

  it("omits 0% when state is ok but a window is null", () => {
    const card = renderQuotaCard(
      quota({
        fiveHour: { utilization: 12, resetsAt: "", severity: "neutral" },
        sevenDay: null,
      }),
    );

    expect(card.textContent).toContain("12%");
    expect(card.textContent).not.toContain("0%");
    // The known window draws a bar; the unknown one draws an absence marker.
    expect(card.querySelectorAll(".bar-row__fill")).toHaveLength(1);
    expect(card.querySelectorAll(".absent")).toHaveLength(1);
  });

  it("renders a real 0% only when the API actually reported zero", () => {
    const card = renderQuotaCard(
      quota({
        fiveHour: { utilization: 0, resetsAt: "", severity: "neutral" },
        sevenDay: { utilization: 0, resetsAt: "", severity: "neutral" },
      }),
    );

    // A measured zero is legitimate data and must still be shown.
    expect(card.textContent).toContain("0%");
    expect(card.querySelectorAll(".bar-row__fill")).toHaveLength(2);
  });
});

// lastKnown exists so a failed refresh can still show the previous reading without
// that reading being mistakable for a live one. The bar is the thing that says
// "current", so lastKnown must never produce one.
describe("lastKnown", () => {
  const snapshot = {
    fetchedAt: "2026-07-30T10:00:00Z",
    fiveHour: { utilization: 5, resetsAt: "", severity: "neutral" as const },
    sevenDay: { utilization: 91, resetsAt: "", severity: "attention" as const },
  };

  it("shows the previous reading as labelled text, not as a bar", () => {
    const card = renderQuotaCard(
      quota({
        state: "unavailable",
        message: "quota unavailable",
        lastKnown: snapshot,
      }),
    );

    expect(card.textContent).toContain("Last known");
    expect(card.textContent).toContain("5%");
    expect(card.textContent).toContain("91%");
    // No bar anywhere: drawing one is reserved for state === "ok".
    expect(card.querySelector(".bar-row__fill")).toBeNull();
    expect(card.querySelector(".bar-row__track")).toBeNull();
  });

  it("renders an em dash for a window the snapshot never had", () => {
    const card = renderQuotaCard(
      quota({
        state: "unavailable",
        message: "quota unavailable",
        lastKnown: { ...snapshot, sevenDay: null },
      }),
    );

    expect(card.textContent).toContain("weekly —");
    expect(card.textContent).not.toContain("0%");
  });

  it("says nothing about a previous reading when there was none", () => {
    const card = renderQuotaCard(
      quota({ state: "no_login", message: "not logged in" }),
    );

    expect(card.textContent).not.toContain("Last known");
  });
});

describe("renderQuotaCard ok state", () => {
  it("draws both windows with their values", () => {
    const card = renderQuotaCard(
      quota({
        fiveHour: {
          utilization: 5,
          resetsAt: "2026-07-30T12:00:00Z",
          severity: "neutral",
        },
        sevenDay: {
          utilization: 91,
          resetsAt: "2026-08-01T00:00:00Z",
          severity: "attention",
        },
      }),
    );

    expect(card.textContent).toContain("5%");
    expect(card.textContent).toContain("91%");
    expect(card.querySelectorAll(".bar-row__fill")).toHaveLength(2);
  });

  it("marks the attention severity the API reported", () => {
    const card = renderQuotaCard(
      quota({
        fiveHour: { utilization: 95, resetsAt: "", severity: "neutral" },
        sevenDay: { utilization: 10, resetsAt: "", severity: "attention" },
      }),
    );

    const fills = card.querySelectorAll(".bar-row__fill");
    // Severity comes from the API, so 95% stays neutral and 10% is flagged. The UI
    // does not apply a threshold of its own.
    expect(fills[0].classList.contains("bar-row__fill--attention")).toBe(false);
    expect(fills[1].classList.contains("bar-row__fill--attention")).toBe(true);
  });

  it("labels stale numbers with their fetch time", () => {
    const card = renderQuotaCard(
      quota({
        stale: true,
        fiveHour: { utilization: 5, resetsAt: "", severity: "neutral" },
      }),
    );

    const chip = card.querySelector(".chip");
    expect(chip?.textContent).toContain("stale");
    expect(chip?.textContent).toContain("fetched");
  });

  it("does not label fresh numbers as stale", () => {
    const card = renderQuotaCard(
      quota({ fiveHour: { utilization: 5, resetsAt: "", severity: "neutral" } }),
    );

    expect(card.querySelector(".chip")).toBeNull();
  });

  it("clamps a bar to at least 2px so 1% stays visible", () => {
    const card = renderQuotaCard(
      quota({ fiveHour: { utilization: 1, resetsAt: "", severity: "neutral" } }),
    );

    const fill = card.querySelector<HTMLElement>(".bar-row__fill");
    expect(fill?.style.width).toContain("2px");
  });
});

describe("renderQuotaCard actions", () => {
  it("routes the retry action to onRetry", () => {
    const onRetry = vi.fn();
    const onLogin = vi.fn();
    const card = renderQuotaCard(
      quota({ state: "unavailable", message: "quota unavailable" }),
      { onRetry, onLogin },
    );

    card.querySelector("button")?.click();
    expect(onRetry).toHaveBeenCalledWith("per");
    expect(onLogin).not.toHaveBeenCalled();
  });

  it("routes a credential problem to onLogin", () => {
    const onRetry = vi.fn();
    const onLogin = vi.fn();
    const card = renderQuotaCard(
      quota({ state: "missing_scope", message: "no usage scope" }),
      { onRetry, onLogin },
    );

    card.querySelector("button")?.click();
    expect(onLogin).toHaveBeenCalledWith("per");
    expect(onRetry).not.toHaveBeenCalled();
  });

  it("shows the retry time while backing off", () => {
    const card = renderQuotaCard(
      quota({
        state: "unavailable",
        message: "rate limited",
        retryAfter: "2026-07-30T10:05:00Z",
      }),
    );

    expect(card.textContent).toContain("retry at");
  });
});
