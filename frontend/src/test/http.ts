import { vi } from "vitest";
import { dashboard, me } from "./fixtures";

export type Handler = (request: Request) => Response | Promise<Response>;

export function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

export function errorBody(code: string, message: string, details: Record<string, unknown> = {}) {
  return { error: { code, message, details } };
}

/** `GET /admin/me` for a browser without a session cookie. */
export const signedOut = () => json(401, errorBody("session_required", "session required"));

/**
 * Routes every test gets unless it overrides them: a signed-in owner and a
 * finished first-run setup.
 */
const defaultRoutes: Record<string, Handler | object> = {
  "GET /admin/me": () => json(200, me()),
  "GET /admin/dashboard": () => json(200, dashboard()),
  "GET /admin/setup": { setup_required: false, setup_token_required: false },
};

/**
 * Stubs global fetch with routes keyed "METHOD /path". A plain value is sent
 * as a 200 JSON body; unknown routes answer 404 with the error envelope.
 */
export function mockApi(routes: Record<string, Handler | object> = {}) {
  const all = { ...defaultRoutes, ...routes };
  const requests: Request[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : new Request(input, init);
    requests.push(request);
    const url = new URL(request.url);
    const handler = all[`${request.method} ${url.pathname}`];
    if (handler === undefined) return json(404, errorBody("not_found", "not found"));
    return typeof handler === "function" ? (handler as Handler)(request) : json(200, handler);
  });
  vi.stubGlobal("fetch", fetchMock);
  return {
    fetch: fetchMock,
    requests,
    /** Requests to a path, newest last. */
    to(method: string, pathname: string) {
      return requests.filter((r) => r.method === method && new URL(r.url).pathname === pathname);
    },
  };
}
