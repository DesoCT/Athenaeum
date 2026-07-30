/**
 * Runtime interaction preferences, persisted per browser in localStorage.
 *
 * These are not the workspace configuration (that stays a hand-edited file):
 * they only shape how this session behaves — the default view, how a comment is
 * started, the theme, and so on. Losing them costs a preference, never data, so
 * localStorage is the right home and no server round-trip is involved.
 */

export type DefaultView = "split" | "source" | "preview";

/**
 * How starting a comment behaves when text is selected in the preview:
 *   button  — a small "Comment" button appears by the selection; click to add.
 *   popover — the comment form opens immediately on selection.
 *   off     — selecting never starts a comment (best for copying text).
 */
export type AnnotateTrigger = "button" | "popover" | "off";

export type Theme = "system" | "light" | "dark";
export type InterfaceSize = "small" | "default" | "large";
export type Visibility = "personal" | "shared";

export interface Settings {
  defaultView: DefaultView;
  annotateOn: AnnotateTrigger;
  wrapLines: boolean;
  theme: Theme;
  interfaceSize: InterfaceSize;
  autosave: boolean;
  defaultVisibility: Visibility;
}

const STORAGE_KEY = "athenaeum.settings.v1";

const defaults: Settings = {
  defaultView: "split",
  // "button" rather than "popover": the old always-open behaviour fought with
  // selecting text to copy, which is the complaint this setting exists to fix.
  annotateOn: "button",
  wrapLines: true,
  // "dark" preserves the current look; "system" and "light" are opt-in.
  theme: "dark",
  interfaceSize: "default",
  // Explicit save stays the default (D-012); autosave is opt-in.
  autosave: false,
  defaultVisibility: "personal",
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

const SIZE_PX: Record<InterfaceSize, number> = { small: 15, default: 16, large: 18 };

/** Resolve "system" to the OS preference. */
function resolvedTheme(): "light" | "dark" {
  if (settings.theme !== "system") return settings.theme;
  return matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

/**
 * Apply the theme and interface size to the document root. The rest of the CSS
 * keys off data-theme and the root font size (rem units scale the UI).
 */
export function applyEnvironment(): void {
  const root = document.documentElement;
  root.dataset.theme = resolvedTheme();
  root.style.fontSize = `${SIZE_PX[settings.interfaceSize]}px`;
}

/**
 * Keep a "system" theme in sync with the OS. Returns an unsubscribe function.
 */
export function watchSystemTheme(): () => void {
  const mql = matchMedia("(prefers-color-scheme: light)");
  const onChange = () => {
    if (settings.theme === "system") applyEnvironment();
  };
  mql.addEventListener("change", onChange);
  return () => mql.removeEventListener("change", onChange);
}
