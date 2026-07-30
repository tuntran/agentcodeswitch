// Copy field: monospace value plus an always-visible copy button.
//
// Always visible rather than on hover, because hover-only controls do not exist for
// keyboard or touch users.
//
// Only ever used for values that belong in a bug report -- configDirLiteral, orgId,
// the Keychain hash. Never a token.

const COPIED_FEEDBACK_MS = 1200;

export interface CopyFieldOptions {
  /** Shown instead of the value when it is empty. */
  placeholder?: string;
  /** Injected in tests, where navigator.clipboard does not exist. */
  copy?: (text: string) => Promise<void>;
}

export function renderCopyField(
  value: string,
  options: CopyFieldOptions = {},
): HTMLElement {
  const wrapper = document.createElement("span");
  wrapper.className = "copy-field";

  if (!value) {
    const absent = document.createElement("span");
    absent.className = "absent";
    absent.textContent = options.placeholder ?? "—";
    wrapper.append(absent);
    return wrapper;
  }

  const text = document.createElement("code");
  text.className = "copy-field__value";
  text.textContent = value;
  // The full value stays reachable even when CSS truncates the display.
  text.title = value;
  wrapper.append(text);

  const button = document.createElement("button");
  button.type = "button";
  button.className = "btn btn--icon";
  button.textContent = "⧉";
  button.setAttribute("aria-label", `Copy ${value}`);

  // A live region so the confirmation reaches a screen reader too, not only the eye.
  const live = document.createElement("span");
  live.className = "sr-only";
  live.setAttribute("aria-live", "polite");

  const copy = options.copy ?? defaultCopy;
  button.addEventListener("click", () => {
    void copy(value).then(
      () => {
        button.textContent = "✓";
        live.textContent = "Copied";
        window.setTimeout(() => {
          button.textContent = "⧉";
          live.textContent = "";
        }, COPIED_FEEDBACK_MS);
      },
      () => {
        live.textContent = "Copy failed";
      },
    );
  });

  wrapper.append(button, live);
  return wrapper;
}

async function defaultCopy(text: string): Promise<void> {
  await navigator.clipboard.writeText(text);
}
