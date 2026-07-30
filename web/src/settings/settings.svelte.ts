/**
 * Runtime interaction preferences, persisted per browser in localStorage.
 *
 * These are not the workspace configuration (that stays a hand-edited file):
 * they only shape how this session behaves — the default view, how a comment is
 * started, and whether the editor wraps. Losing them costs a preference, never
 * data, so localStorage is the right home and no server round-trip is involved.
 */

export type DefaultView = "split" | "source" | "preview";

/**
 * How starting a comment behaves when text is selected in the preview:
 *   button  — a small "Comment" button appears by the selection; click to add.
 *   popover — the comment form opens immediately on selection.
 *   off     — selecting never starts a comment (best for copying text).
 */
export type AnnotateTrigger = "button" | "popover" | "off";

export interface Settings {
  defaultView: DefaultView;
  annotateOn: AnnotateTrigger;
  wrapLines: boolean;
}

const STORAGE_KEY = "athenaeum.settings.v1";

const defaults: Settings = {
  defaultView: "split",
  // "button" rather than "popover": the old always-open behaviour fought with
  // selecting text to copy, which is the complaint this setting exists to fix.
  annotateOn: "button",
  wrapLines: true,
};

function loadInitial(): Settings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...defaults };
    return { ...defaults, ...(JSON.parse(raw) as Partial<Settings>) };
  } catch {
    return { ...defaults };
  }
}

/** The shared, reactive settings object. Mutate a field, then persist(). */
export const settings = $state<Settings>(loadInitial());

/** Write the current settings back to storage. */
export function persistSettings(): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(settings));
  } catch {
    // Storage may be unavailable (private mode); settings then last the session.
  }
}
