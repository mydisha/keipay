import createClient, { type Middleware } from "openapi-fetch";
import { CSRF_BOOTSTRAP_PATH, CSRF_HEADER, isUnsafeMethod, readCsrfToken } from "./csrf";
import { ApiError, errorFromBody, networkError, unknownError } from "./errors";
import type { paths } from "./schema";
import { isAuthRedirectReason, redirectForAuth, type AuthRedirectReason } from "./session";

// Sign-in and setup report their own failures on the form; `/admin/me` is
// read by the route guards, which know whether the current page is public.
const NO_REDIRECT_PATHS = new Set(["/admin/login", "/admin/setup", "/admin/me"]);
const REDIRECT_STATUSES = new Set([401, 403]);

export interface ApiClientOptions {
  baseUrl?: string;
  fetch?: typeof globalThis.fetch;
  onAuthRedirect?: (reason: AuthRedirectReason) => void;
}

/**
 * Builds the typed client. The session travels in a same-origin cookie; unsafe
 * methods add the CSRF header from the `keipay_csrf` cookie, and an ended
 * session or a pending password change moves the user to the right page.
 */
export function createApiClient(options: ApiClientOptions = {}) {
  const baseUrl = options.baseUrl ?? window.location.origin;
  const onAuthRedirect = options.onAuthRedirect ?? redirectForAuth;
  const doFetch = options.fetch ?? ((input, init) => globalThis.fetch(input, init));

  // Concurrent writes share one bootstrap request.
  let bootstrapping: Promise<void> | null = null;
  async function ensureCsrfCookie(): Promise<string | null> {
    const token = readCsrfToken();
    if (token) return token;
    bootstrapping ??= doFetch(new Request(new URL(CSRF_BOOTSTRAP_PATH, baseUrl), { credentials: "same-origin" }))
      .then(() => undefined)
      .catch(() => undefined)
      .finally(() => {
        bootstrapping = null;
      });
    await bootstrapping;
    // Still missing: send without it and let the server answer 403.
    return readCsrfToken();
  }

  const client = createClient<paths>({ baseUrl, fetch: doFetch, credentials: "same-origin" });

  const middleware: Middleware = {
    async onRequest({ request }) {
      if (!isUnsafeMethod(request.method)) return request;
      const token = await ensureCsrfCookie();
      if (token) request.headers.set(CSRF_HEADER, token);
      return request;
    },
    async onResponse({ response, schemaPath }) {
      if (!REDIRECT_STATUSES.has(response.status) || NO_REDIRECT_PATHS.has(schemaPath)) return undefined;
      const body: unknown = await response
        .clone()
        .json()
        .catch(() => null);
      const { code } = errorFromBody(response.status, body);
      if (isAuthRedirectReason(code)) onAuthRedirect(code);
      return undefined;
    },
    onError({ error }) {
      return error instanceof ApiError ? error : networkError(error);
    },
  };
  client.use(middleware);
  return client;
}

export type ApiClient = ReturnType<typeof createApiClient>;

export const api: ApiClient = createApiClient();

interface FetchResult {
  data?: unknown;
  error?: unknown;
  response: Response;
}

/**
 * Awaits an openapi-fetch call and returns its data, or throws an ApiError
 * built from the error envelope. Use inside TanStack Query functions.
 */
export async function unwrap<R extends FetchResult>(call: Promise<R>): Promise<NonNullable<R["data"]>> {
  let result: R;
  try {
    result = await call;
  } catch (error) {
    if (error instanceof ApiError) throw error;
    // A 2xx body that isn't JSON makes openapi-fetch throw while parsing.
    if (error instanceof SyntaxError) throw unknownError(200);
    throw networkError(error);
  }
  const { response } = result;
  if (!response.ok || result.error !== undefined) {
    throw errorFromBody(response.status, result.error);
  }
  return result.data as NonNullable<R["data"]>;
}

/** Like `unwrap` for operations that answer 204 with no body. */
export async function unwrapEmpty<R extends FetchResult>(call: Promise<R>): Promise<void> {
  let result: R;
  try {
    result = await call;
  } catch (error) {
    if (error instanceof ApiError) throw error;
    throw networkError(error);
  }
  if (!result.response.ok) throw errorFromBody(result.response.status, result.error);
}
