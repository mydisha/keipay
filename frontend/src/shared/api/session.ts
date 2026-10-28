import { readStorage, removeStorage, storageKeys, writeStorage } from "@/shared/lib/storage";

// The bearer token stays in localStorage until phase 1 moves to cookies.

export const SESSION_EXPIRED_PATH = "/login?reason=session_expired";

export function getToken(): string | null {
  return readStorage(storageKeys.token);
}

export function setToken(token: string): void {
  writeStorage(storageKeys.token, token);
}

export function clearToken(): void {
  removeStorage(storageKeys.token);
}

/** The `sub` claim of the stored JWT, for display only (never trusted). */
export function getTokenSubject(): string | null {
  const token = getToken();
  const payload = token?.split(".")[1];
  if (!payload) return null;
  try {
    const json = atob(payload.replace(/-/g, "+").replace(/_/g, "/"));
    const sub = (JSON.parse(json) as { sub?: unknown }).sub;
    return typeof sub === "string" && sub !== "" ? sub : null;
  } catch {
    return null;
  }
}

type ExpiredListener = () => void;
let onExpired: ExpiredListener | null = null;

/** The app registers an in-app redirect; without one we fall back to a full navigation. */
export function setSessionExpiredHandler(listener: ExpiredListener | null): void {
  onExpired = listener;
}

export function expireSession(): void {
  clearToken();
  if (onExpired) onExpired();
  else window.location.assign(SESSION_EXPIRED_PATH);
}
