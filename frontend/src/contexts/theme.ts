import { createContext, useContext } from "react";

export type Theme = "dark" | "light";

export const ThemeContext = createContext<{ theme: Theme; toggleTheme: () => void } | null>(null);

export function useTheme() {
  const value = useContext(ThemeContext);
  if (!value) throw new Error("useTheme must be used within ThemeProvider");
  return value;
}
