import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { readStorage, storageKeys, writeStorage } from "./storage";

// The Test/Live switch. Its values match an app's and a connection's
// `environment`: it sets the starting environment filter of the Apps and
// Connections lists and the default of the create-app dialog and the
// add-connection wizard, and filters Reconciliation. Payments ignore it
// until they can filter by environment.
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

/**
 * An environment filter that starts at the mode and goes back to it whenever
 * the mode switch changes; `null` means every environment.
 */
export function useEnvironmentFilter() {
  const { mode } = useMode();
  const [state, setState] = useState<{ mode: Mode; environment: Mode | null }>({ mode, environment: mode });
  if (state.mode !== mode) setState({ mode, environment: mode });
  const environment = state.mode === mode ? state.environment : mode;
  return [environment, (next: Mode | null) => setState({ mode, environment: next })] as const;
}
