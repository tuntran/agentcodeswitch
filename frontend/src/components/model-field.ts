// Model field: the per-profile default model and its context-window option,
// edited in place.
//
// Free text with a datalist, never a <select>. A closed list would mean acs
// blocks every model released after this build -- exactly the moment you want to
// switch to it. The suggestions are a convenience; anything typeable is accepted
// and a wrong id surfaces as claude's own error, which names the model.
//
// The "[1m]" suffix is a checkbox rather than part of the text, so turning it off
// gives the plain name back instead of leaving a half-edited id behind.

const SAVED_FEEDBACK_MS = 1200;

/**
 * Convenience only. Not a whitelist, and nothing keeps it current.
 *
 * ADD A NEW MODEL HERE, bare and without "[1m]" -- the checkbox owns the suffix.
 * The list is hand-maintained on purpose: there is no endpoint that returns the
 * models an account may use, so anything automatic would be a guess that goes
 * stale silently. Being out of date costs a dropdown entry and nothing else,
 * because the field stays free text.
 */
const SUGGESTIONS = [
  "claude-opus-4-8",
  "claude-opus-5",
  "claude-sonnet-5",
  "claude-haiku-4-5-20251001",
];

const CONTEXT_SUFFIX = "[1m]";

let datalistSeq = 0;

/**
 * Persistence, passed in rather than imported: this component must stay free of
 * the Wails bindings so vitest can render it without them existing.
 */
export type SaveModel = (name: string, model: string) => Promise<void>;
export type SaveContext1M = (name: string, on: boolean) => Promise<void>;

export interface ModelFieldProps {
  profileName: string;
  /** Without the suffix. */
  model: string;
  context1m: boolean;
  saveModel: SaveModel;
  saveContext1M: SaveContext1M;
}

/**
 * Renders the editable model cell for one profile.
 *
 * Saving happens on `change` -- blur or Enter -- rather than on every keystroke,
 * so a half-typed id is never written to the registry.
 */
export function renderModelField(props: ModelFieldProps): HTMLElement {
  const { profileName, saveModel, saveContext1M } = props;

  const wrapper = document.createElement("span");
  wrapper.className = "model-field";

  const listID = `model-suggestions-${(datalistSeq += 1)}`;
  const datalist = document.createElement("datalist");
  datalist.id = listID;
  for (const suggestion of SUGGESTIONS) {
    const option = document.createElement("option");
    option.value = suggestion;
    datalist.append(option);
  }

  const input = document.createElement("input");
  input.className = "input input--inline";
  input.value = props.model;
  // "default" rather than an example id: an empty field means Claude Code
  // chooses, which is a real setting and not a missing one.
  input.placeholder = "default";
  input.setAttribute("list", listID);
  input.setAttribute("aria-label", `Default model for ${profileName}`);

  const toggle = document.createElement("input");
  toggle.type = "checkbox";
  toggle.checked = props.context1m;

  const toggleLabel = document.createElement("label");
  toggleLabel.className = "model-field__toggle";
  toggleLabel.append(toggle, document.createTextNode("1M"));

  const live = document.createElement("span");
  live.className = "model-field__status";
  live.setAttribute("aria-live", "polite");

  let savedModel = props.model;
  let savedContext1M = props.context1m;
  // One save at a time. Both controls write the same registry entry, and editing
  // the text blurs the field -- so clicking the checkbox fires the model save
  // first and the two promises then settle in an order nothing guarantees. The
  // model save would re-tick a box the user had just cleared, leaving the UI
  // claiming a setting that is not on disk. Serialising is what keeps the two
  // controls from disagreeing about the same row.
  let busy = false;

  // There is nothing to append a suffix to while the field is empty, so the
  // option is disabled rather than left looking effective.
  const sync = (): void => {
    const value = input.value.trim();
    input.disabled = busy;
    toggle.disabled = busy || value === "";
    toggleLabel.title =
      value === ""
        ? "Applies once this profile names a model."
        : `Runs ${value}${CONTEXT_SUFFIX} for a 1M-token context window.`;
  };
  sync();
  input.addEventListener("input", sync);

  const report = (message: string, transient: boolean): void => {
    live.textContent = message;
    if (!transient) return;
    window.setTimeout(() => {
      // A later message owns the line; clearing it here would erase an error
      // that arrived inside this timer's window.
      if (live.textContent === message) live.textContent = "";
    }, SAVED_FEEDBACK_MS);
  };

  input.addEventListener("change", () => {
    // Pasting the suffixed form is the obvious thing to do -- it is what the
    // Claude Code docs show -- so it is read as "this model, extended context"
    // instead of being stored and suffixed again. The backend splits it the same
    // way; mirroring it here keeps the checkbox from disagreeing with what was
    // just saved.
    const raw = input.value.trim();
    const [next, suffixed] = splitSuffix(raw);
    if (busy || (next === savedModel && !suffixed)) return;

    busy = true;
    sync();
    live.textContent = "Saving…";
    void saveModel(profileName, raw).then(
      () => {
        savedModel = next;
        input.value = next;
        if (suffixed) {
          toggle.checked = true;
          savedContext1M = true;
        }
        busy = false;
        sync();
        report(next ? "Saved" : "Cleared", true);
      },
      (err: unknown) => {
        // The rejected text is left in the field on purpose: it is usually a
        // typo, and clearing it would make the user retype the whole id to fix
        // one character.
        busy = false;
        sync();
        report(String(err), false);
      },
    );
  });

  toggle.addEventListener("change", () => {
    const on = toggle.checked;
    if (busy || on === savedContext1M) {
      toggle.checked = savedContext1M;
      return;
    }
    busy = true;
    sync();
    live.textContent = "Saving…";
    void saveContext1M(profileName, on).then(
      () => {
        savedContext1M = on;
        busy = false;
        sync();
        report(on ? "1M context on" : "1M context off", true);
      },
      (err: unknown) => {
        toggle.checked = savedContext1M;
        busy = false;
        sync();
        report(String(err), false);
      },
    );
  });

  wrapper.append(input, datalist, toggleLabel, live);
  return wrapper;
}

/**
 * Strips every trailing "[1m]", mirroring profile.SplitContextSuffix in Go.
 *
 * Cutting only one leaves the second half of "...[1m][1m]" in the name, which the
 * backend then suffixes again into an id claude rejects -- while this field
 * redraws as "...[1m]" and looks right.
 */
function splitSuffix(model: string): [base: string, extended: boolean] {
  let base = model;
  let extended = false;
  while (base.endsWith(CONTEXT_SUFFIX)) {
    base = base.slice(0, -CONTEXT_SUFFIX.length);
    extended = true;
  }
  return [base, extended];
}
