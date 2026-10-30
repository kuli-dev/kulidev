// Dark (the default), light, or follow the system; remembered per browser.
export type ThemePref = "system" | "light" | "dark";

const KEY = "kuli.theme";
const media = matchMedia("(prefers-color-scheme: dark)");

export function themePref(): ThemePref {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "light" || v === "system") return v;
  } catch { /* storage off */ }
  return "dark";
}

export const isDark = () => {
  const p = themePref();
  return p === "dark" || (p === "system" && media.matches);
};

export function applyTheme() {
  const p = themePref();
  document.documentElement.dataset.theme = p;
  dispatchEvent(new Event("kuli-theme"));
}

export function setThemePref(p: ThemePref) {
  try {
    if (p === "dark") localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, p);
  } catch { /* storage off */ }
  applyTheme();
}

media.addEventListener("change", () => dispatchEvent(new Event("kuli-theme")));
