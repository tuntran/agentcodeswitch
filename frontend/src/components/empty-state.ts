// Empty state: state the fact, then name the possibilities when the state is
// ambiguous.
//
// The ambiguity rule matters for acs. "No quota cached yet" and "the endpoint is
// down" mean nearly opposite things, and collapsing both into one blank cell throws
// away the distinction the user needs.
//
// No illustration, no cheerful voice.

export interface EmptyStateOptions {
  /** The fact, stated plainly. */
  headline: string;
  /** The possibilities, when more than one reading is plausible. */
  detail?: string;
  actionLabel?: string;
  onAction?: () => void;
}

export function emptyState(options: EmptyStateOptions): HTMLElement {
  const wrapper = document.createElement("div");
  wrapper.className = "empty-state";

  const headline = document.createElement("h3");
  headline.textContent = options.headline;
  wrapper.append(headline);

  if (options.detail) {
    const detail = document.createElement("p");
    detail.className = "empty-state__detail";
    detail.textContent = options.detail;
    wrapper.append(detail);
  }

  if (options.actionLabel && options.onAction) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "btn btn--ghost";
    button.textContent = options.actionLabel;
    button.addEventListener("click", options.onAction);
    wrapper.append(button);
  }
  return wrapper;
}
