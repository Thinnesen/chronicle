// The theme: "system" follows the operating system. The choice stays in the browser of the user.
export type Theme = "system" | "light" | "dark";

const key = "chronicle-theme";

export function readTheme(): Theme {
  try {
    const t = localStorage.getItem(key);
    return t === "light" || t === "dark" ? t : "system";
  } catch {
    return "system";
  }
}

export function applyTheme(t: Theme): void {
  const root = document.documentElement;
  if (t === "system") {
    root.removeAttribute("data-theme");
  } else {
    root.setAttribute("data-theme", t);
  }
  try {
    localStorage.setItem(key, t);
  } catch {
    // The theme then lasts only for this page.
  }
}
