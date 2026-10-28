import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { readStorage, storageKeys, writeStorage } from "./storage";
import { useMediaQuery } from "./use-media-query";

export type Theme = "light" | "dark" | "system";
export type ResolvedTheme = "light" | "dark";

const THEMES: readonly Theme[] = ["light", "dark", "system"];

export function readStoredTheme(): Theme {
  const stored = readStorage(storageKeys.theme);
  return THEMES.includes(stored as Theme) ? (stored as Theme) : "system";
}

export function applyTheme(resolved: ResolvedTheme): void {
  const root = document.documentElement;
  root.classList.toggle("dark", resolved === "dark");
  root.style.colorScheme = resolved;
}

interface ThemeContextValue {
  theme: Theme;
  resolvedTheme: ResolvedTheme;
  setTheme: (theme: Theme) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(readStoredTheme);
  const prefersDark = useMediaQuery("(prefers-color-scheme: dark)");
  const resolvedTheme: ResolvedTheme = theme === "system" ? (prefersDark ? "dark" : "light") : theme;

  useEffect(() => {
    applyTheme(resolvedTheme);
  }, [resolvedTheme]);

  const setTheme = useCallback((next: Theme) => {
    writeStorage(storageKeys.theme, next);
    setThemeState(next);
  }, []);

  const value = useMemo(() => ({ theme, resolvedTheme, setTheme }), [theme, resolvedTheme, setTheme]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used inside ThemeProvider");
  return ctx;
}
