import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { me } from "@/test/fixtures";
import { errorBody, json, mockApi, signedOut } from "@/test/http";
import { renderApp } from "@/test/render";

interface SetupApiOptions {
  tokenRequired?: boolean;
  setup?: (request: Request) => Response | Promise<Response>;
}

/** A fresh server: no user, setup required, until POST /admin/setup succeeds. */
function setupApi({ tokenRequired = false, setup = () => json(201, me()) }: SetupApiOptions = {}) {
  let session: unknown = null;
  return mockApi({
    "GET /admin/me": () => (session ? json(200, session) : signedOut()),
    "GET /admin/setup": () => json(200, { setup_required: session === null, setup_token_required: tokenRequired }),
    "POST /admin/setup": async (request) => {
      const response = await setup(request);
      if (response.ok) session = await response.clone().json();
      return response;
    },
  });
}

type User = ReturnType<typeof renderApp>["user"];

async function fill(user: User, values: { name?: string; email?: string; password?: string; confirm?: string; token?: string }) {
  if (values.name) await user.type(await screen.findByLabelText("Name"), values.name);
  if (values.email) await user.type(screen.getByLabelText("Email"), values.email);
  if (values.password) await user.type(screen.getByLabelText("Password"), values.password);
  if (values.confirm) await user.type(screen.getByLabelText("Confirm password"), values.confirm);
  if (values.token) await user.type(screen.getByLabelText("Setup token"), values.token);
}

const valid = { name: "Olivia Owner", email: "owner@example.com", password: "a long password", confirm: "a long password" };

async function submit(user: User) {
  await user.click(screen.getByRole("button", { name: "Create owner account" }));
}

describe("Setup page (T-44)", () => {
  it("asks for the setup token only when the server requires it", async () => {
    setupApi({ tokenRequired: false });
    const { unmount } = renderApp("/setup");
    expect(await screen.findByLabelText("Name")).toBeInTheDocument();
    expect(screen.queryByLabelText("Setup token")).not.toBeInTheDocument();
    unmount();

    setupApi({ tokenRequired: true });
    renderApp("/setup");
    expect(await screen.findByLabelText("Setup token")).toBeInTheDocument();
  });

  it("validates like the contract before calling the API", async () => {
    const api = setupApi();
    const { user } = renderApp("/setup");
    await fill(user, { name: "  ", email: "not-an-email", password: "elevenchars", confirm: "different" });
    await submit(user);
    expect(await screen.findByText("Enter a name.")).toBeInTheDocument();
    expect(screen.getByText("Enter an email address like ops@example.com.")).toBeInTheDocument();
    expect(screen.getByText("Use at least 12 characters.")).toBeInTheDocument();
    expect(screen.getByText("Passwords don't match.")).toBeInTheDocument();

    await user.clear(screen.getByLabelText("Password"));
    await user.type(screen.getByLabelText("Password"), "a".repeat(73));
    await submit(user);
    expect(await screen.findByText(/Use at most 72 bytes/)).toBeInTheDocument();
    expect(api.to("POST", "/admin/setup")).toHaveLength(0);
  });

  it("accepts a unicode email and a multibyte password within 72 bytes", async () => {
    const api = setupApi();
    const { user, router } = renderApp("/setup");
    await fill(user, { name: "Üser", email: "üser@exämple.com", password: "ééééééééééé1", confirm: "ééééééééééé1" });
    await submit(user);
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await api.to("POST", "/admin/setup")[0]!.json()).toEqual({
      name: "Üser",
      email: "üser@exämple.com",
      password: "ééééééééééé1",
    });
  });

  it("puts server field errors on their fields", async () => {
    setupApi({
      setup: () =>
        json(400, errorBody("invalid_request", "invalid request body", { fields: [{ field: "email", rule: "email" }] })),
    });
    const { user } = renderApp("/setup");
    await fill(user, valid);
    await submit(user);
    expect(await screen.findByText("Enter an email address like ops@example.com.")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows a wrong setup token on the token field and sends it", async () => {
    const api = setupApi({
      tokenRequired: true,
      setup: () => json(403, errorBody("invalid_setup_token", "invalid setup token")),
    });
    const { user } = renderApp("/setup");
    await screen.findByLabelText("Setup token");
    await submit(user);
    expect(await screen.findByText("Enter the setup token.")).toBeInTheDocument();
    await fill(user, { ...valid, token: "wrong-token" });
    await submit(user);
    expect(await screen.findByText("The setup token is incorrect.")).toBeInTheDocument();
    expect(screen.getByLabelText("Setup token")).toHaveAttribute("aria-invalid", "true");
    expect(await api.to("POST", "/admin/setup")[0]!.json()).toMatchObject({ setup_token: "wrong-token" });
  });

  it("goes to sign-in when setup already happened", async () => {
    setupApi({ setup: () => json(409, errorBody("setup_completed", "setup already completed")) });
    const { user, router } = renderApp("/setup");
    await fill(user, valid);
    await submit(user);
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(await screen.findByText("Setup is already complete. Sign in instead.")).toBeInTheDocument();
  });

  it("explains rate limiting in minutes", async () => {
    setupApi({ setup: () => json(429, errorBody("rate_limited", "too many attempts", { retry_after_seconds: 120 })) });
    const { user } = renderApp("/setup");
    await fill(user, valid);
    await submit(user);
    expect(await screen.findByRole("alert")).toHaveTextContent("Too many attempts. Try again in 2 minutes.");
  });

  it("shows other failures above the form", async () => {
    setupApi({ setup: () => json(500, errorBody("internal_error", "boom")) });
    const { user } = renderApp("/setup");
    await fill(user, valid);
    await submit(user);
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't complete setup. Try again.");
  });

  it("signs the owner in and lands on the dashboard (AC-22)", async () => {
    const api = setupApi();
    const { user, router, queryClient } = renderApp("/setup");
    await fill(user, valid);
    await submit(user);
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument();
    expect(queryClient.getQueryData(["me"])).toEqual(me());
    expect(await api.to("POST", "/admin/setup")[0]!.json()).toEqual({
      name: "Olivia Owner",
      email: "owner@example.com",
      password: "a long password",
    });
  });

  it("offers a retry when the setup status can't be loaded", async () => {
    let healthy = false;
    mockApi({
      "GET /admin/me": signedOut,
      "GET /admin/setup": () =>
        healthy
          ? json(200, { setup_required: true, setup_token_required: false })
          : json(500, errorBody("internal_error", "boom")),
    });
    const { user } = renderApp("/setup");
    const retry = await screen.findByRole("button", { name: "Try again" });
    expect(screen.getByText("Couldn't check the setup status.")).toBeInTheDocument();
    healthy = true;
    await user.click(retry);
    expect(await screen.findByLabelText("Name")).toBeInTheDocument();
  });
});
