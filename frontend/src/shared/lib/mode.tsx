import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { readStorage, storageKeys, writeStorage } from "./storage";

// Test/Live is console state only until environments arrive (phase 2).
export type Mode = "live" | "test";

interface ModeContextValue {
  mode: Mode;
  setMode: (mode: Mode) => void;
}

const ModeContext = createContext<ModeContextValue | null>(null);

function readStoredMode(): Mode {
  return readStorage(storageKeys.mode) === "test" ? "test" : "live";
}

export function ModeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<Mode>(readStoredMode);
  const setMode = useCallback((next: Mode) => {
    writeStorage(storageKeys.mode, next);
    setModeState(next);
  }, []);
  const value = useMemo(() => ({ mode, setMode }), [mode, setMode]);
  return <ModeContext.Provider value={value}>{children}</ModeContext.Provider>;
}

export function useMode(): ModeContextValue {
  const ctx = useContext(ModeContext);
  if (!ctx) throw new Error("useMode must be used inside ModeProvider");
  return ctx;
}
