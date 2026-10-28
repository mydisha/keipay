import { describe, expect, it, vi } from "vitest";
import { setTestCookie } from "@/test/cookies";
import { errorBody, json } from "@/test/http";
import { createApiClient, unwrap, unwrapEmpty } from "./client";
import { ApiError, errorFromBody } from "./errors";

function clientWith(fetchImpl: (request: Request) => Promise<Response>, onAuthRedirect = vi.fn()) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => fetchImpl(input as Request));
  const client = createApiClient({ baseUrl: "http://keipay.test", fetch: fetchMock, onAuthRedirect });
  const requests = () => fetchMock.mock.calls.map((call) => call[0] as Request);
  return { client, fetchMock, requests, onAuthRedirect };
}

async function caught(promise: Promise<unknown>): Promise<ApiError> {
  try {
    await promise;
  } catch (error) {
    if (error instanceof ApiError) return error;
    throw error;
  }
  throw new Error("expected the call to fail");
}

const newTenant = { name: "Acme", slug: "acme" };

describe("API client", () => {
  it("returns typed data and sends cookies same-origin", async () => {
    const { client, requests } = clientWith(async () => json(200, { tenants: [] }));
    const data = await unwrap(client.GET("/admin/tenants"));
    expect(data).toEqual({ tenants: [] });
    const [request] = requests();
    expect(request!.url).toBe("http://keipay.test/admin/tenants");
    expect(request!.credentials).toBe("same-origin");
  });

  it("maps the error envelope to ApiError fields", async () => {
    setTestCookie("keipay_csrf", "csrf-1");
    const fields = [{ field: "slug", rule: "required" }];
    const { client } = clientWith(async () => json(400, errorBody("invalid_request", "invalid request body", { fields })));
    const error = await caught(unwrap(client.POST("/admin/tenants", { body: { name: "", slug: "" } })));
    expect(error).toMatchObject({ status: 400, code: "invalid_request", message: "invalid request body" });
    expect(error.details).toEqual({ fields });
    expect(error.fields).toEqual(fields);
  });

  it("turns an HTML 502 into unknown_error", async () => {
    const { client } = clientWith(
      async () => new Response("<html><body>Bad gateway</body></html>", { status: 502, headers: { "Content-Type": "text/html" } }),
    );
    const error = await caught(unwrap(client.GET("/admin/tenants")));
    expect(error).toMatchObject({ status: 502, code: "unknown_error" });
  });

  it("turns a non-JSON 200 body into unknown_error", async () => {
    const { client } = clientWith(async () => new Response("<!doctype html>", { status: 200 }));
    const error = await caught(unwrap(client.GET("/admin/tenants")));
    expect(error.code).toBe("unknown_error");
  });

  it("turns a thrown fetch into network_error", async () => {
    const { client } = clientWith(async () => {
      throw new TypeError("Failed to fetch");
    });
    const error = await caught(unwrap(client.GET("/admin/tenants")));
    expect(error).toMatchObject({ status: 0, code: "network_error" });
  });

  it("accepts a 204 without a body", async () => {
    setTestCookie("keipay_csrf", "csrf-1");
    const { client } = clientWith(async () => new Response(null, { status: 204 }));
    await expect(unwrapEmpty(client.POST("/admin/logout"))).resolves.toBeUndefined();
  });
});

describe("API client CSRF (T-40)", () => {
  it.each([
    ["POST", (c: ReturnType<typeof clientWith>["client"]) => c.POST("/admin/tenants", { body: newTenant })],
    [
      "PUT",
      (c: ReturnType<typeof clientWith>["client"]) =>
        c.PUT("/admin/tenants/{id}", {
          params: { path: { id: 1 } },
          body: { name: "A", webhook_url: "", redirect_success_url: "", redirect_failure_url: "", active: true },
        }),
    ],
    [
      "PATCH",
      (c: ReturnType<typeof clientWith>["client"]) =>
        c.PATCH("/admin/users/{id}", { params: { path: { id: "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0C" } }, body: { name: "B" } }),
    ],
  ])("%s sends X-CSRF-Token from the keipay_csrf cookie at send time", async (method, call) => {
    const { client, requests } = clientWith(async () => json(200, {}));
    setTestCookie("keipay_csrf", "first");
    await call(client);
    setTestCookie("keipay_csrf", "rotated");
    await call(client);
    const sent = requests().filter((r) => r.method === method);
    expect(sent.map((r) => r.headers.get("X-CSRF-Token"))).toEqual(["first", "rotated"]);
  });

  it("does not send the header on GET", async () => {
    setTestCookie("keipay_csrf", "csrf-1");
    const { client, requests } = clientWith(async () => json(200, { tenants: [] }));
    await unwrap(client.GET("/admin/tenants"));
    expect(requests()[0]!.headers.has("X-CSRF-Token")).toBe(false);
  });

  it("fetches GET /admin/setup first when the cookie is missing, then sends the header", async () => {
    const { client, requests } = clientWith(async (request) => {
      if (new URL(request.url).pathname === "/admin/setup") {
        setTestCookie("keipay_csrf", "issued");
        return json(200, { setup_required: false, setup_token_required: false });
      }
      return json(201, {});
    });
    await client.POST("/admin/tenants", { body: newTenant });
    const sent = requests();
    expect(sent.map((r) => `${r.method} ${new URL(r.url).pathname}`)).toEqual(["GET /admin/setup", "POST /admin/tenants"]);
    expect(sent[1]!.headers.get("X-CSRF-Token")).toBe("issued");
  });

  it("shares one bootstrap request between concurrent writes", async () => {
    const { client, requests } = clientWith(async (request) => {
      if (new URL(request.url).pathname === "/admin/setup") {
        setTestCookie("keipay_csrf", "issued");
        return json(200, { setup_required: false, setup_token_required: false });
      }
      return json(201, {});
    });
    await Promise.all([client.POST("/admin/tenants", { body: newTenant }), client.POST("/admin/logout")]);
    expect(requests().filter((r) => r.method === "GET")).toHaveLength(1);
    expect(requests().filter((r) => r.method === "POST").every((r) => r.headers.get("X-CSRF-Token") === "issued")).toBe(true);
  });

  it("sends without the header when the cookie still can't be obtained", async () => {
    const { client, requests } = clientWith(async (request) => {
      if (new URL(request.url).pathname === "/admin/setup") throw new TypeError("Failed to fetch");
      return json(403, errorBody("invalid_csrf_token", "invalid csrf token"));
    });
    const error = await caught(unwrap(client.POST("/admin/tenants", { body: newTenant })));
    expect(error.code).toBe("invalid_csrf_token");
    expect(requests().at(-1)!.headers.has("X-CSRF-Token")).toBe(false);
  });

  it("never sends an Authorization header", async () => {
    setTestCookie("keipay_csrf", "csrf-1");
    const { client, requests } = clientWith(async () => json(200, {}));
    await client.GET("/admin/tenants");
    await client.POST("/admin/tenants", { body: newTenant });
    await client.POST("/admin/logout");
    expect(requests().some((r) => r.headers.has("Authorization"))).toBe(false);
  });
});

describe("API client session redirects (T-41)", () => {
  it.each([
    [401, "session_expired"],
    [401, "session_required"],
    [403, "password_change_required"],
  ] as const)("%i %s on a console call redirects with that reason", async (statusCode, code) => {
    const { client, onAuthRedirect } = clientWith(async () => json(statusCode, errorBody(code, "auth")));
    const error = await caught(unwrap(client.GET("/admin/payments")));
    expect(error.code).toBe(code);
    expect(onAuthRedirect).toHaveBeenCalledExactlyOnceWith(code);
  });

  it("does not redirect when sign-in answers 401 invalid_credentials", async () => {
    setTestCookie("keipay_csrf", "csrf-1");
    const { client, onAuthRedirect } = clientWith(async () => json(401, errorBody("invalid_credentials", "invalid credentials")));
    const error = await caught(unwrap(client.POST("/admin/login", { body: { email: "a@b.co", password: "b" } })));
    expect(error.code).toBe("invalid_credentials");
    expect(onAuthRedirect).not.toHaveBeenCalled();
  });

  it.each([
    ["/admin/login", (c: ReturnType<typeof clientWith>["client"]) => c.POST("/admin/login", { body: { email: "a", password: "b" } })],
    ["/admin/setup", (c: ReturnType<typeof clientWith>["client"]) => c.GET("/admin/setup")],
    ["/admin/me", (c: ReturnType<typeof clientWith>["client"]) => c.GET("/admin/me")],
  ])("leaves %s to its page even on session codes", async (_, call) => {
    setTestCookie("keipay_csrf", "csrf-1");
    const { client, onAuthRedirect } = clientWith(async () => json(401, errorBody("session_expired", "session expired")));
    await call(client);
    expect(onAuthRedirect).not.toHaveBeenCalled();
  });

  it("does not redirect for other 401/403 codes (wrong current password, forbidden)", async () => {
    setTestCookie("keipay_csrf", "csrf-1");
    const { client, onAuthRedirect } = clientWith(async (request) =>
      new URL(request.url).pathname === "/admin/me/password"
        ? json(401, errorBody("invalid_credentials", "invalid credentials"))
        : json(403, errorBody("forbidden", "forbidden")),
    );
    await client.POST("/admin/me/password", { body: { current_password: "x", new_password: "y".repeat(12) } });
    await client.GET("/admin/users");
    expect(onAuthRedirect).not.toHaveBeenCalled();
  });

  it("falls back to a full navigation without an in-app handler", async () => {
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, origin: "http://keipay.test", assign });
    const fetchMock = vi.fn(async () => json(401, errorBody("session_expired", "session expired")));
    const client = createApiClient({ baseUrl: "http://keipay.test", fetch: fetchMock });
    await caught(unwrap(client.GET("/admin/tenants")));
    expect(assign).toHaveBeenCalledWith("/login?reason=session_expired");
  });
});

describe("errorFromBody", () => {
  it.each([
    ["a string", "Bad gateway"],
    ["null", null],
    ["an old-style error", { error: "not found" }],
    ["a missing message", { error: { code: "not_found" } }],
  ])("returns unknown_error for %s", (_, body) => {
    expect(errorFromBody(500, body).code).toBe("unknown_error");
  });

  it("defaults details to an empty object", () => {
    const error = errorFromBody(404, { error: { code: "not_found", message: "not found", details: null } });
    expect(error.details).toEqual({});
    expect(error.fields).toEqual([]);
  });
});
