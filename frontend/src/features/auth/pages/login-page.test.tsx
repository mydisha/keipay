import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { setTestCookie } from "@/test/cookies";
import { me } from "@/test/fixtures";
import { errorBody, json, mockApi, signedOut } from "@/test/http";
import { renderApp } from "@/test/render";

async function fillAndSubmit(user: ReturnType<typeof renderApp>["user"], email: string, password: string) {
  const emailInput = await screen.findByLabelText("Email");
  if (email) await user.type(emailInput, email);
  if (password) await user.type(screen.getByLabelText("Password"), password);
  await user.click(screen.getByRole("button", { name: "Sign in" }));
}

/** Signed out until POST /admin/login answers with `body`. */
function loginApi(login: (request: Request) => Response | Promise<Response>) {
  let session: unknown = null;
  return mockApi({
    "GET /admin/me": () => (session ? json(200, session) : signedOut()),
    "POST /admin/login": async (request) => {
      const response = await login(request);
      if (response.ok) session = await response.clone().json();
      return response;
    },
  });
}

describe("Login page", () => {
  it("asks for email and password before calling the API", async () => {
    const api = loginApi(() => json(200, me()));
    const { user } = renderApp("/login");
    await fillAndSubmit(user, "", "");
    expect(await screen.findByText("Enter your email.")).toBeInTheDocument();
    expect(screen.getByText("Enter your password.")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByLabelText("Username")).not.toBeInTheDocument();
    expect(api.to("POST", "/admin/login")).toHaveLength(0);
  });

  it("shows 'Incorrect email or password.' for invalid_credentials", async () => {
    loginApi(() => json(401, errorBody("invalid_credentials", "invalid credentials")));
    const { user, router } = renderApp("/login");
    await fillAndSubmit(user, "ops@example.com", "wrong");
    expect(await screen.findByRole("alert")).toHaveTextContent("Incorrect email or password.");
    expect(router.state.location.pathname).toBe("/login");
  });

  it.each([
    [61, "Too many failed attempts. Try again in 2 minutes."],
    [900, "Too many failed attempts. Try again in 15 minutes."],
    [45, "Too many failed attempts. Try again in 1 minute."],
  ])("explains rate limiting with %i seconds in minutes", async (seconds, text) => {
    loginApi(() => json(429, errorBody("rate_limited", "too many attempts", { retry_after_seconds: seconds })));
    const { user } = renderApp("/login");
    await fillAndSubmit(user, "ops@example.com", "secret");
    expect(await screen.findByRole("alert")).toHaveTextContent(text);
  });

  it("shows a generic message for a server error", async () => {
    loginApi(() => json(500, errorBody("internal_error", "internal server error")));
    const { user } = renderApp("/login");
    await fillAndSubmit(user, "ops@example.com", "pw");
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't sign in. Try again.");
  });

  it("sends the trimmed email with the CSRF header and goes to the console", async () => {
    setTestCookie("keipay_csrf", "csrf-login");
    const api = loginApi(() => json(200, me()));
    const { user, router } = renderApp("/login");
    await fillAndSubmit(user, " Ops@Example.com ", "secret password");
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument();
    const [request] = api.to("POST", "/admin/login");
    expect(await request!.json()).toEqual({ email: "Ops@Example.com", password: "secret password" });
    expect(request!.headers.get("X-CSRF-Token")).toBe("csrf-login");
  });

  it("goes to the forced password change when required", async () => {
    loginApi(() => json(200, me({ must_change_password: true })));
    const { user, router } = renderApp("/login");
    await fillAndSubmit(user, "ops@example.com", "temporary-pass");
    await waitFor(() => expect(router.state.location.pathname).toBe("/change-password"));
    expect(await screen.findByRole("heading", { level: 1, name: "Set a new password" })).toBeInTheDocument();
  });

  it("explains an expired session", async () => {
    mockApi({ "GET /admin/me": signedOut });
    renderApp("/login?reason=session_expired");
    expect(await screen.findByRole("alert")).toHaveTextContent("Your session has expired. Sign in again.");
  });

  it("shows no banner without the reason", async () => {
    mockApi({ "GET /admin/me": signedOut });
    renderApp("/login");
    await screen.findByRole("button", { name: "Sign in" });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("describes the product beside the form", async () => {
    mockApi({ "GET /admin/me": signedOut });
    renderApp("/login");
    const panel = await screen.findByRole("region", { name: "About KeiPay" });
    expect(panel).toHaveTextContent("One API for every payment gateway.");
    expect(panel).toHaveTextContent("KeiPay never holds funds.");
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
  });
});
