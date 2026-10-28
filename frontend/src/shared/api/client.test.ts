import { describe, expect, it, vi } from "vitest";
import { errorBody, json } from "@/test/http";
import { createApiClient, unwrap } from "./client";
import { ApiError, errorFromBody } from "./errors";
import { getToken, setToken } from "./session";

function clientWith(fetchImpl: (request: Request) => Promise<Response>, onSessionExpired = vi.fn()) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => fetchImpl(input as Request));
  const client = createApiClient({ baseUrl: "http://keipay.test", fetch: fetchMock, onSessionExpired });
  return { client, fetchMock, onSessionExpired };
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

describe("API client (T-27)", () => {
  it("adds the bearer token and returns typed data", async () => {
    setToken("tok-123");
    const { client, fetchMock } = clientWith(async () => json(200, { tenants: [] }));
    const data = await unwrap(client.GET("/admin/tenants"));
    expect(data).toEqual({ tenants: [] });
    const request = fetchMock.mock.calls[0]![0] as Request;
    expect(request.headers.get("Authorization")).toBe("Bearer tok-123");
    expect(request.url).toBe("http://keipay.test/admin/tenants");
  });

  it("sends no Authorization header without a token", async () => {
    const { client, fetchMock } = clientWith(async () => json(200, { tenants: [] }));
    await unwrap(client.GET("/admin/tenants"));
    expect((fetchMock.mock.calls[0]![0] as Request).headers.has("Authorization")).toBe(false);
  });

  it("maps the error envelope to ApiError fields", async () => {
    const fields = [{ field: "slug", rule: "required" }];
    const { client } = clientWith(async () =>
      json(400, errorBody("invalid_request", "invalid request body", { fields })),
    );
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

  it.each(["invalid_token", "missing_token"])("ends the session on 401 %s", async (code) => {
    setToken("expired");
    const { client, onSessionExpired } = clientWith(async () => json(401, errorBody(code, "token problem")));
    const error = await caught(unwrap(client.GET("/admin/payments")));
    expect(error.code).toBe(code);
    expect(onSessionExpired).toHaveBeenCalledOnce();
  });

  it("does not end the session when login answers 401", async () => {
    const { client, onSessionExpired } = clientWith(async () =>
      json(401, errorBody("invalid_credentials", "invalid credentials")),
    );
    const error = await caught(unwrap(client.POST("/admin/login", { body: { username: "a", password: "b" } })));
    expect(error.code).toBe("invalid_credentials");
    expect(onSessionExpired).not.toHaveBeenCalled();
  });

  it("does not end the session for other 401 codes (wrong current password)", async () => {
    const { client, onSessionExpired } = clientWith(async () =>
      json(401, errorBody("invalid_credentials", "invalid credentials")),
    );
    await caught(
      unwrap(client.POST("/admin/change-password", { body: { current_password: "x", new_password: "y".repeat(8) } })),
    );
    expect(onSessionExpired).not.toHaveBeenCalled();
  });

  it("the default session handler clears the token", async () => {
    setToken("expired");
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, origin: "http://keipay.test", assign });
    const fetchMock = vi.fn(async () => json(401, errorBody("invalid_token", "invalid token")));
    const client = createApiClient({ baseUrl: "http://keipay.test", fetch: fetchMock });
    await caught(unwrap(client.GET("/admin/tenants")));
    expect(getToken()).toBeNull();
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
