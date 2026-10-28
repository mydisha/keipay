import createClient, { type Middleware } from "openapi-fetch";
import { ApiError, errorFromBody, networkError, unknownError } from "./errors";
import type { paths } from "./schema";
import { expireSession, getToken } from "./session";

const SESSION_CODES = new Set(["missing_token", "invalid_token"]);
const LOGIN_PATH = "/admin/login";

export interface ApiClientOptions {
  baseUrl?: string;
  fetch?: typeof globalThis.fetch;
  getToken?: () => string | null;
  onSessionExpired?: () => void;
}

/**
 * Builds the typed client. Every admin call carries the bearer token; a 401
 * `missing_token`/`invalid_token` from anything but login ends the session.
 */
export function createApiClient(options: ApiClientOptions = {}) {
  const readToken = options.getToken ?? getToken;
  const sessionExpired = options.onSessionExpired ?? expireSession;
  const doFetch = options.fetch ?? ((input, init) => globalThis.fetch(input, init));

  const client = createClient<paths>({
    baseUrl: options.baseUrl ?? window.location.origin,
    fetch: doFetch,
  });

  const middleware: Middleware = {
    onRequest({ request }) {
      const token = readToken();
      if (token) request.headers.set("Authorization", `Bearer ${token}`);
      return request;
    },
    async onResponse({ response, schemaPath }) {
      if (response.status !== 401 || schemaPath === LOGIN_PATH) return undefined;
      const body: unknown = await response
        .clone()
        .json()
        .catch(() => null);
      const error = errorFromBody(401, body);
      if (SESSION_CODES.has(error.code)) sessionExpired();
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
