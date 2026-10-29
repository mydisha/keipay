import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { me } from "@/test/fixtures";
import { errorBody, json, mockApi, signedOut } from "@/test/http";
import { renderApp } from "@/test/render";

const setupDone = { setup_required: false, setup_token_required: false };
const setupNeeded = { setup_required: true, setup_token_required: false };

describe("RequireAuth", () => {
  it("shows a full-page skeleton, not the console, while the user loads", async () => {
    mockApi({ "GET /admin/me": () => new Promise<Response>(() => {}) });
    renderApp("/payments");
    expect(await screen.findByTestId("shell-skeleton")).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Main" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { level: 1 })).not.toBeInTheDocument();
  });

  it("sends a browser without a session to sign-in", async () => {
    mockApi({ "GET /admin/me": signedOut });
    const { router } = renderApp("/payments");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toBe("");
    expect(await screen.findByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });

  it("sends an expired session to sign-in with the message", async () => {
    let calls = 0;
    mockApi({
      "GET /admin/me": () => {
        calls += 1;
        // The server clears the cookie with the first answer.
        return calls === 1 ? json(401, errorBody("session_expired", "session expired")) : signedOut();
      },
    });
    const { router } = renderApp("/apps");
    await waitFor(() => expect(router.state.location.search).toBe("?reason=session_expired"));
    expect(router.state.location.pathname).toBe("/login");
    expect(await screen.findByText("Your session has expired. Sign in again.")).toBeInTheDocument();
  });

  it("shows an error with retry for other failures", async () => {
    let calls = 0;
    mockApi({
      "GET /admin/me": () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "internal server error")) : json(200, me());
      },
    });
    const { user, router } = renderApp("/");
    const error = await screen.findByTestId("error-state");
    expect(router.state.location.pathname).toBe("/");
    await user.click(within(error).getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument();
  });

  it("locks the console behind the password change while it is required", async () => {
    mockApi({ "GET /admin/me": me({ must_change_password: true }) });
    const { router } = renderApp("/apps");
    await waitFor(() => expect(router.state.location.pathname).toBe("/change-password"));
    expect(await screen.findByRole("heading", { level: 1, name: "Set a new password" })).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Main" })).not.toBeInTheDocument();
  });

  it("renders the page for a signed-in user", async () => {
    renderApp("/");
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Main" })).toBeInTheDocument();
  });
});

describe("Public-only pages", () => {
  it("first run: / and /login lead to /setup", async () => {
    mockApi({ "GET /admin/me": signedOut, "GET /admin/setup": setupNeeded });
    const { router } = renderApp("/");
    await waitFor(() => expect(router.state.location.pathname).toBe("/setup"));
    expect(await screen.findByRole("heading", { level: 1, name: "Set up KeiPay" })).toBeInTheDocument();
  });

  it("/login goes to /setup while setup is required", async () => {
    mockApi({ "GET /admin/me": signedOut, "GET /admin/setup": setupNeeded });
    const { router } = renderApp("/login");
    await waitFor(() => expect(router.state.location.pathname).toBe("/setup"));
  });

  it("/setup goes to /login once setup is done", async () => {
    mockApi({ "GET /admin/me": signedOut, "GET /admin/setup": setupDone });
    const { router } = renderApp("/setup");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(await screen.findByRole("heading", { level: 1, name: "Sign in to KeiPay" })).toBeInTheDocument();
  });

  it.each(["/login", "/setup"])("a signed-in user on %s goes to the console", async (path) => {
    const { router } = renderApp(path);
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument();
  });

  it("shows a skeleton while deciding", async () => {
    mockApi({ "GET /admin/me": signedOut, "GET /admin/setup": () => new Promise<Response>(() => {}) });
    renderApp("/login");
    expect(await screen.findByTestId("auth-skeleton")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sign in" })).not.toBeInTheDocument();
  });

  it("still shows sign-in when the setup status can't be loaded", async () => {
    mockApi({ "GET /admin/me": signedOut, "GET /admin/setup": () => json(500, errorBody("internal_error", "x")) });
    renderApp("/login");
    expect(await screen.findByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });
});
