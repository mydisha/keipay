// localStorage can be unavailable (private mode, blocked site data), so every
// access is guarded and falls back to "not stored".

export const storageKeys = {
  token: "kei_admin_token",
  theme: "keipay.theme",
  mode: "keipay.mode",
  sidebarCollapsed: "keipay.sidebar.collapsed",
} as const;

export function readStorage(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function writeStorage(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Not persisted; the in-memory state still applies for this tab.
  }
}

export function removeStorage(key: string): void {
  try {
    window.localStorage.removeItem(key);
  } catch {
    // Nothing stored to remove.
  }
}
