// Double-submit CSRF: the server sets `keipay_csrf` (readable by scripts) and
// every unsafe /admin request echoes it in `X-CSRF-Token`.

export const CSRF_COOKIE = "keipay_csrf";
export const CSRF_HEADER = "X-CSRF-Token";
/** Public endpoint that sets the CSRF cookie when the browser has none. */
export const CSRF_BOOTSTRAP_PATH = "/admin/setup";

const UNSAFE_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

export function isUnsafeMethod(method: string): boolean {
  return UNSAFE_METHODS.has(method.toUpperCase());
}

export function readCookie(name: string): string | null {
  for (const part of document.cookie.split(";")) {
    const index = part.indexOf("=");
    if (index === -1) continue;
    if (part.slice(0, index).trim() !== name) continue;
    const value = part.slice(index + 1).trim();
    if (!value) return null;
    try {
      return decodeURIComponent(value);
    } catch {
      return value;
    }
  }
  return null;
}

export function readCsrfToken(): string | null {
  return readCookie(CSRF_COOKIE);
}
