import { describe, expect, it, vi } from "vitest";

import {
  renderModelField,
  type SaveContext1M,
  type SaveModel,
} from "./model-field";

interface Rendered {
  input: HTMLInputElement;
  toggle: HTMLInputElement;
  status: () => string;
}

function field(
  model: string,
  context1m: boolean,
  saveModel: SaveModel,
  saveContext1M: SaveContext1M = vi.fn<SaveContext1M>().mockResolvedValue(
    undefined,
  ),
): Rendered {
  const el = renderModelField({
    profileName: "per",
    model,
    context1m,
    saveModel,
    saveContext1M,
  });
  document.body.replaceChildren(el);
  const inputs = el.querySelectorAll("input");
  return {
    input: inputs[0] as HTMLInputElement,
    toggle: inputs[1] as HTMLInputElement,
    status: () =>
      el.querySelector(".model-field__status")?.textContent ?? "",
  };
}

function change(input: HTMLInputElement, value: string): void {
  input.value = value;
  input.dispatchEvent(new Event("change"));
}

const ok = (): ReturnType<typeof vi.fn<SaveModel>> =>
  vi.fn<SaveModel>().mockResolvedValue(undefined);

describe("renderModelField", () => {
  it("saves on change, not on every keystroke", async () => {
    const saveModel = ok();
    const { input } = field("", true, saveModel);

    input.value = "claude-op";
    input.dispatchEvent(new Event("input"));
    expect(saveModel).not.toHaveBeenCalled();

    change(input, "claude-opus-5");
    await vi.waitFor(() =>
      expect(saveModel).toHaveBeenCalledWith("per", "claude-opus-5"),
    );
  });

  it("saves an empty value so a pinned model can be cleared", async () => {
    const saveModel = ok();
    const { input } = field("claude-opus-5", true, saveModel);

    change(input, "");
    await vi.waitFor(() => expect(saveModel).toHaveBeenCalledWith("per", ""));
  });

  it("does not write when the value is unchanged", () => {
    const saveModel = ok();
    const { input } = field("claude-opus-5", true, saveModel);

    // Blur alone fires change in some engines. Writing on it would put a config
    // write behind every click that leaves the field.
    change(input, "claude-opus-5");
    expect(saveModel).not.toHaveBeenCalled();
  });

  it("keeps the rejected text so a typo can be corrected", async () => {
    const saveModel = vi
      .fn<SaveModel>()
      .mockRejectedValue(new Error("model id has a space"));
    const { input, status } = field("", true, saveModel);

    change(input, "claude opus");
    await vi.waitFor(() => expect(status()).toContain("space"));
    expect(input.value).toBe("claude opus");
  });

  it("offers suggestions without restricting the field to them", () => {
    const { input } = field("", true, ok());

    // A <select> here would block every model released after this build.
    expect(input.tagName).toBe("INPUT");
    const list = input.getAttribute("list");
    expect(document.querySelector(`datalist#${list} option`)).not.toBeNull();
    // The suffix is the checkbox's job, so it must not be baked into a suggestion.
    for (const option of document.querySelectorAll("datalist option")) {
      expect((option as HTMLOptionElement).value).not.toContain("[1m]");
    }
  });

  it("defaults to 1M context on", () => {
    const { toggle } = field("claude-opus-5", true, ok());
    expect(toggle.checked).toBe(true);
  });

  it("saves the toggle on its own, without touching the model", async () => {
    const saveModel = ok();
    const saveContext1M = vi.fn<SaveContext1M>().mockResolvedValue(undefined);
    const { toggle } = field("claude-opus-5", true, saveModel, saveContext1M);

    toggle.checked = false;
    toggle.dispatchEvent(new Event("change"));

    await vi.waitFor(() =>
      expect(saveContext1M).toHaveBeenCalledWith("per", false),
    );
    expect(saveModel).not.toHaveBeenCalled();
  });

  it("puts the checkbox back when the save fails", async () => {
    const saveContext1M = vi
      .fn<SaveContext1M>()
      .mockRejectedValue(new Error("config is locked"));
    const { toggle, status } = field("claude-opus-5", true, ok(), saveContext1M);

    toggle.checked = false;
    toggle.dispatchEvent(new Event("change"));

    await vi.waitFor(() => expect(status()).toContain("locked"));
    // Leaving it unticked would claim a setting that was never written.
    expect(toggle.checked).toBe(true);
  });

  it("moves a pasted [1m] suffix into the checkbox", async () => {
    const saveModel = ok();
    const { input, toggle } = field("", false, saveModel);

    change(input, "claude-opus-4-8[1m]");

    // Sent whole: the backend splits it the same way, and stripping it here
    // first would lose the intent on the way to disk.
    await vi.waitFor(() =>
      expect(saveModel).toHaveBeenCalledWith("per", "claude-opus-4-8[1m]"),
    );
    await vi.waitFor(() => expect(input.value).toBe("claude-opus-4-8"));
    expect(toggle.checked).toBe(true);
  });

  it("collapses a doubled suffix instead of leaving one behind", async () => {
    const saveModel = ok();
    const { input, toggle } = field("", false, saveModel);

    change(input, "claude-opus-4-8[1m][1m]");

    await vi.waitFor(() => expect(input.value).toBe("claude-opus-4-8"));
    expect(toggle.checked).toBe(true);
  });

  it("does not re-tick a box the user cleared during the model save", async () => {
    let releaseModelSave: () => void = () => {};
    const saveModel = vi
      .fn<SaveModel>()
      .mockReturnValue(
        new Promise<void>((resolve) => {
          releaseModelSave = resolve;
        }),
      );
    const saveContext1M = vi.fn<SaveContext1M>().mockResolvedValue(undefined);
    const { input, toggle } = field("", false, saveModel, saveContext1M);

    // Editing the text blurs the field, so the model save starts first and the
    // checkbox click lands while it is still in flight.
    change(input, "claude-haiku-4-5-20251001[1m]");
    toggle.checked = false;
    toggle.dispatchEvent(new Event("change"));
    releaseModelSave();

    await vi.waitFor(() => expect(input.value).toBe("claude-haiku-4-5-20251001"));
    // The click was refused while a save was running, so the box still reflects
    // what was written rather than a state nobody persisted.
    expect(saveContext1M).not.toHaveBeenCalled();
    expect(toggle.checked).toBe(true);
  });

  it("disables the toggle while no model is named", () => {
    const { input, toggle } = field("", true, ok());
    // Nothing to append a suffix to. Leaving it enabled would offer a setting
    // that does nothing.
    expect(toggle.disabled).toBe(true);

    input.value = "claude-opus-5";
    input.dispatchEvent(new Event("input"));
    expect(toggle.disabled).toBe(false);
  });
});
