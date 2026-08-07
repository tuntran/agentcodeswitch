import { describe, expect, it, vi } from "vitest";

import { formatAge, renderQuotaCard } from "./quota-card";
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
    // Not "Log in again": Claude Code refreshes the token on start, so starting it
    // is the remedy. Logging in again is the fallback, one button over.
    expectAction: "Open Claude Code",
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

  it("labels stale numbers as stale, with their age", () => {
    const card = renderQuotaCard(
      quota({
        stale: true,
        fiveHour: { utilization: 5, resetsAt: "", severity: "neutral" },
      }),
    );

    const chip = card.querySelector(".chip");
    expect(chip?.textContent).toContain("stale");
    expect(chip?.textContent).toContain("ago");
  });

  // The age is shown even when the number is current. Without it, a reading from ten
  // minutes ago looks exactly like one from a second ago, which is what made the
  // dashboard feel stuck while it was working correctly.
  it("shows the age of fresh numbers without calling them stale", () => {
    const card = renderQuotaCard(
      quota({ fiveHour: { utilization: 5, resetsAt: "", severity: "neutral" } }),
    );

    const chip = card.querySelector(".chip");
    expect(chip, "an ok card must always say how old its numbers are").not.toBeNull();
    expect(chip?.textContent).toContain("updated");
    expect(chip?.textContent).not.toContain("stale");
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

describe("formatAge", () => {
  const base = new Date("2026-07-30T10:00:00Z");

  it("reads an unknown timestamp as unknown, never as 1970", () => {
    expect(formatAge("", base)).toBe("unknown");
    expect(formatAge("not-a-date", base)).toBe("unknown");
  });

  it("describes recent readings as current", () => {
    expect(formatAge("2026-07-30T09:59:55Z", base)).toBe("just now");
  });

  it("scales the unit with the age", () => {
    expect(formatAge("2026-07-30T09:59:30Z", base)).toBe("30s ago");
    expect(formatAge("2026-07-30T09:56:00Z", base)).toBe("4m ago");
    expect(formatAge("2026-07-30T07:00:00Z", base)).toBe("3h ago");
    expect(formatAge("2026-07-28T10:00:00Z", base)).toBe("2d ago");
  });

  it("treats a future timestamp as current rather than negative", () => {
    expect(formatAge("2026-07-30T10:05:00Z", base)).toBe("just now");
  });
});

describe("the ok card's refresh control", () => {
  const okQuota = () =>
    quota({ fiveHour: { utilization: 5, resetsAt: "", severity: "neutral" } });

  it("asks for a refresh with the profile name", () => {
    const onRefresh = vi.fn().mockResolvedValue(undefined);
    const card = renderQuotaCard(okQuota(), { onRefresh });

    card.querySelector<HTMLButtonElement>(".quota-card__refresh")?.click();

    expect(onRefresh).toHaveBeenCalledTimes(1);
    expect(onRefresh).toHaveBeenCalledWith("per");
  });

  it("disables itself while the refresh is in flight", () => {
    const onRefresh = vi.fn().mockReturnValue(new Promise(() => {}));
    const card = renderQuotaCard(okQuota(), { onRefresh });
    const button = card.querySelector<HTMLButtonElement>(".quota-card__refresh");

    button?.click();

    expect(button?.disabled).toBe(true);
    // The label survives so the button keeps its accessible name.
    expect(button?.textContent).toBe("Refresh");
    expect(button?.getAttribute("aria-busy")).toBe("true");
  });

  // Without this the button stays disabled after a failure, with nothing on screen
  // saying why, until the user navigates away and back. The card is only replaced on
  // success, so recovery has to happen here.
  it("re-enables itself when the refresh fails", async () => {
    const onRefresh = vi.fn().mockRejectedValue(new Error("endpoint down"));
    const card = renderQuotaCard(okQuota(), { onRefresh });
    const button = card.querySelector<HTMLButtonElement>(".quota-card__refresh");

    button?.click();
    expect(button?.disabled).toBe(true);

    // Let the rejection settle.
    await Promise.resolve();
    await Promise.resolve();

    expect(button?.disabled).toBe(false);
    expect(button?.hasAttribute("aria-busy")).toBe(false);
  });
});

describe("an expired token points at Claude Code, not at a fresh login", () => {
  const expiredQuota = () =>
    quota({ state: "expired", message: "access token expired" });

  it("makes launching Claude Code the primary action", () => {
    const onLaunch = vi.fn();
    const onLogin = vi.fn();
    const card = renderQuotaCard(expiredQuota(), { onLaunch, onLogin });

    const buttons = card.querySelectorAll("button");
    expect(buttons[0].textContent).toBe("Open Claude Code");
    // Weighted, not merely first: two identical-looking buttons make "primary"
    // meaningless.
    expect(buttons[0].classList.contains("btn--primary")).toBe(true);
    expect(buttons[1].classList.contains("btn--primary")).toBe(false);
    buttons[0].click();

    expect(onLaunch).toHaveBeenCalledWith("per");
    expect(
      onLogin,
      "the primary action must not send them to login",
    ).not.toHaveBeenCalled();
  });

  it("keeps logging in again available as a fallback", () => {
    const onLogin = vi.fn();
    const card = renderQuotaCard(expiredQuota(), { onLogin });

    const buttons = Array.from(card.querySelectorAll("button"));
    const fallback = buttons.find((b) => b.textContent === "Log in again");
    expect(fallback, "a dead refresh token still needs a way out").toBeDefined();

    fallback?.click();
    expect(onLogin).toHaveBeenCalledWith("per");
  });
});

// The other three failure states must keep the actions they had.
describe("the remaining failure states are untouched", () => {
  it("sends no_login and missing_scope to login", () => {
    for (const state of ["no_login", "missing_scope"] as const) {
      const onLogin = vi.fn();
      const card = renderQuotaCard(quota({ state, message: "x" }), { onLogin });
      card.querySelector("button")?.click();
      expect(onLogin, state).toHaveBeenCalledWith("per");
    }
  });

  it("sends unavailable to retry", () => {
    const onRetry = vi.fn();
    const onLogin = vi.fn();
    const card = renderQuotaCard(quota({ state: "unavailable", message: "x" }), {
      onRetry,
      onLogin,
    });

    card.querySelector("button")?.click();

    expect(onRetry).toHaveBeenCalledWith("per");
    expect(onLogin).not.toHaveBeenCalled();
  });
});
