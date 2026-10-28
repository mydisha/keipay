// The session lives in an HttpOnly cookie the console never sees. This module
// only decides where to send the user when the server says the session is
// gone or the password must change first.

export type AuthRedirectReason = "session_expired" | "session_required" | "password_change_required" | "signed_out";

export const LOGIN_PATH = "/login";
export const SESSION_EXPIRED_PATH = "/login?reason=session_expired";
export const CHANGE_PASSWORD_PATH = "/change-password";

export const authRedirectPaths: Record<AuthRedirectReason, string> = {
  session_expired: SESSION_EXPIRED_PATH,
  session_required: LOGIN_PATH,
  signed_out: LOGIN_PATH,
  password_change_required: CHANGE_PASSWORD_PATH,
};

/** Error codes that move the user to another page instead of showing an error. */
export const AUTH_REDIRECT_CODES: ReadonlySet<string> = new Set([
  "session_expired",
  "session_required",
  "password_change_required",
]);

export function isAuthRedirectReason(code: string): code is AuthRedirectReason {
  return AUTH_REDIRECT_CODES.has(code);
}

type AuthRedirectHandler = (reason: AuthRedirectReason) => void;
let handler: AuthRedirectHandler | null = null;

/** The app registers an in-app redirect; without one we fall back to a full navigation. */
export function setAuthRedirectHandler(next: AuthRedirectHandler | null): void {
  handler = next;
}

export function redirectForAuth(reason: AuthRedirectReason): void {
  if (handler) handler(reason);
  else window.location.assign(authRedirectPaths[reason]);
}
