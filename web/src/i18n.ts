// Interface language and the other interface choices, as in Jusplay. The
// values come from prefs.js (a blocking script in <head>, so the first
// paint is already right) and are saved back through api/prefs. Switching
// re-renders in place; nothing reloads, so an open note is not disturbed.

export type Lang = "zh" | "ja";

export interface Prefs {
  lang: Lang;
  theme: "dark" | "light";
  font: "sans" | "mono";
  size: number;
  wrap: boolean;
  guides: boolean;
  numbers: boolean;
}

declare global {
  interface Window {
    jusPrefs?: Partial<Prefs>;
    kpTheme?: (dark: boolean) => Promise<void>;
  }
}

export const prefs: Prefs = {
  lang: "zh",
  theme: "dark",
  font: "sans",
  size: 15,
  wrap: true,
  guides: true,
  numbers: true,
  ...window.jusPrefs,
};

export const SIZE_MIN = 11;
export const SIZE_MAX = 28;
export const SIZE_DEFAULT = 15;

/** The text for the current language; both versions sit side by side at the call site. */
export const L = (zh: string, ja: string): string => (prefs.lang === "ja" ? ja : zh);

const langListeners: (() => void)[] = [];
const prefListeners: ((k: keyof Prefs) => void)[] = [];

/** Runs fn now and after every language change. */
export function onLang(fn: () => void): void {
  langListeners.push(fn);
  fn();
}

/** Runs fn after any preference changes. */
export function onPref(fn: (k: keyof Prefs) => void): void {
  prefListeners.push(fn);
}

function applyDocument(): void {
  const d = document.documentElement;
  d.dataset.theme = prefs.theme;
  d.dataset.font = prefs.font;
  d.lang = prefs.lang === "ja" ? "ja" : "zh-CN";
  d.style.setProperty("--editor-size", prefs.size + "px");
}

export function setPref<K extends keyof Prefs>(k: K, v: Prefs[K]): void {
  if (prefs[k] === v) return;
  prefs[k] = v;
  applyDocument();
  if (k === "theme") window.kpTheme?.(v === "dark").catch(() => undefined);
  if (k === "lang") for (const fn of langListeners) fn();
  for (const fn of prefListeners) fn(k);
  fetch("api/prefs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ [k]: v }),
  }).catch(() => undefined);
}

/** Static texts of the page: [selector, property, zh, ja]. */
export type StaticText = [string, "textContent" | "title" | "placeholder" | "aria-label", string, string];

export function applyStatic(items: StaticText[]): void {
  for (const [sel, prop, zh, ja] of items) {
    for (const e of document.querySelectorAll<HTMLElement>(sel)) {
      const v = L(zh, ja);
      if (prop === "textContent") e.textContent = v;
      else if (prop === "placeholder") (e as HTMLInputElement).placeholder = v;
      else e.setAttribute(prop, v);
    }
  }
}

applyDocument();
