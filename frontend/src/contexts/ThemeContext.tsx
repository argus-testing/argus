import { useCallback, useEffect, useState, type ReactNode } from "react";
import { ThemeContext, type Theme } from "./theme";

function initialTheme(): Theme {
  try {
    return localStorage.getItem("theme") === "light" ? "light" : "dark";
  } catch {
    return "dark";
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(initialTheme);

  useEffect(() => {
    if (theme === "light") document.documentElement.setAttribute("data-theme", "light");
    else document.documentElement.removeAttribute("data-theme");
    try {
      localStorage.setItem("theme", theme);
    } catch {
      // Theme remains usable when storage is unavailable.
    }
  }, [theme]);

  const toggleTheme = useCallback(() => setTheme((value) => (value === "dark" ? "light" : "dark")), []);
  return <ThemeContext.Provider value={{ theme, toggleTheme }}>{children}</ThemeContext.Provider>;
}
