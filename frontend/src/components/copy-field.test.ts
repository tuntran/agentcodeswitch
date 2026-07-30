import { describe, expect, it, vi } from "vitest";

import { renderCopyField } from "./copy-field";

describe("renderCopyField", () => {
  it("shows the value and keeps the full string in the title", () => {
    const value = "/Users/tuntran/.acs/profiles/per";
    const field = renderCopyField(value);

    const code = field.querySelector("code");
    expect(code?.textContent).toBe(value);
    // CSS truncates the display, so the full path has to stay reachable.
    expect(code?.title).toBe(value);
  });

  it("copies the value and confirms it", async () => {
    const copy = vi.fn().mockResolvedValue(undefined);
    const field = renderCopyField("707f7e46", { copy });

    const button = field.querySelector("button");
    button?.click();
    await vi.waitFor(() => expect(button?.textContent).toBe("✓"));

    expect(copy).toHaveBeenCalledWith("707f7e46");
    // Announced as well as shown, so the confirmation is not eye-only.
    expect(field.querySelector("[aria-live]")?.textContent).toBe("Copied");
  });

  it("reports a failed copy rather than claiming success", async () => {
    const copy = vi.fn().mockRejectedValue(new Error("denied"));
    const field = renderCopyField("707f7e46", { copy });

    field.querySelector("button")?.click();
    await vi.waitFor(() =>
      expect(field.querySelector("[aria-live]")?.textContent).toBe("Copy failed"),
    );
    expect(field.querySelector("button")?.textContent).toBe("⧉");
  });

  it("renders an absence marker and no button for an empty value", () => {
    const field = renderCopyField("");

    expect(field.querySelector(".absent")?.textContent).toBe("—");
    expect(field.querySelector("button")).toBeNull();
  });

  it("keeps the copy button visible rather than hover-only", () => {
    const field = renderCopyField("707f7e46");

    const button = field.querySelector("button");
    expect(button?.getAttribute("aria-label")).toContain("Copy");
    // Nothing hides it behind :hover, which would not exist for keyboard or touch.
    expect(button?.hidden).toBe(false);
  });
});
