import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import { setTestCookie } from "@/test/cookies";
import { me } from "@/test/fixtures";
import { errorBody, json, mockApi, signedOut } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

const NAV = ["Dashboard", "Apps", "Connections", "Webhooks", "Payments", "Reconciliation", "Settings"];

beforeEach(() => {
  setViewport("desktop");
});

describe("App shell", () => {
  it("shows the final navigation in the sidebar", async () => {
    renderApp("/");
    const nav = await screen.findByRole("navigation", { name: "Main" });
    expect(within(nav).getAllByRole("link").map((a) => a.textContent)).toEqual(NAV);
    expect(within(nav).getByRole("link", { name: "Dashboard" })).toHaveAttribute("aria-current", "page");
    expect(nav).toHaveTextContent(/Overview.*Operate.*Control/);
  });

  it("shows who is signed in beside the navigation, without a second account menu", async () => {
    mockApi({ "GET /admin/me": me({ name: "Vera Viewer", role: "viewer" }) });
    renderApp("/");
    const identity = await screen.findByTestId("sidebar-identity");
    expect(identity).toHaveTextContent("Vera Viewer");
    expect(identity).toHaveTextContent("Viewer");
    expect(identity).toHaveTextContent("VV");
    expect(screen.getAllByRole("button", { name: "Account menu" })).toHaveLength(1);
  });

  it("collapses the sidebar and remembers it", async () => {
    const { user, unmount } = renderApp("/");
    await user.click(await screen.findByRole("button", { name: "Collapse sidebar" }));
    expect(window.localStorage.getItem(storageKeys.sidebarCollapsed)).toBe("true");
    unmount();
    renderApp("/");
    expect(await screen.findByRole("button", { name: "Expand sidebar" })).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByRole("link", { name: "Payments" })).toBeInTheDocument();
  });

  it("switches to test mode, shows the amber banner and persists it", async () => {
    const { user, unmount } = renderApp("/");
    expect(screen.queryByTestId("test-mode-banner")).not.toBeInTheDocument();
    const toggle = await screen.findByRole("switch", { name: "Test mode" });
    expect(toggle).toHaveAttribute("aria-checked", "false");
    await user.click(toggle);
    const banner = screen.getByTestId("test-mode-banner");
    expect(banner).toHaveTextContent("Test mode");
    expect(banner).not.toHaveTextContent(/filtered|only test data|showing test/i);
    expect(banner).toHaveTextContent("Reconciliation");
    expect(banner).toHaveTextContent("Payments still show every environment");
    expect(window.localStorage.getItem(storageKeys.mode)).toBe("test");

    unmount();
    mockApi({ "GET /admin/payments": { data: [], next_cursor: null }, "GET /admin/apps": { data: [], next_cursor: null } });
    renderApp("/payments");
    expect(await screen.findByTestId("test-mode-banner")).toBeInTheDocument();
  });

  it("sets the dark class from the theme menu and persists it", async () => {
    const { user } = renderApp("/");
    await user.click(await screen.findByRole("button", { name: "Change theme" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Dark" }));
    expect(document.documentElement).toHaveClass("dark");
    expect(window.localStorage.getItem(storageKeys.theme)).toBe("dark");
    await user.click(screen.getByRole("button", { name: "Change theme" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Light" }));
    expect(document.documentElement).not.toHaveClass("dark");
  });

  it("opens the palette with Ctrl+K and navigates", async () => {
    mockApi({ "GET /admin/payments": { data: [], next_cursor: null }, "GET /admin/apps": { data: [], next_cursor: null } });
    const { user, router } = renderApp("/");
    await screen.findByRole("heading", { level: 1, name: "Dashboard" });
    await user.keyboard("{Control>}k{/Control}");
    const dialog = await screen.findByRole("dialog", { name: "Command palette" });
    expect(dialog).toHaveTextContent(/arrives in a later release/);
    await user.type(within(dialog).getByRole("combobox"), "payments");
    await user.keyboard("{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/payments"));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(await screen.findByRole("heading", { level: 1, name: "Payments" })).toBeInTheDocument();
  });

  it("opens the palette with ⌘K and switches mode and theme", async () => {
    const { user } = renderApp("/");
    await screen.findByRole("heading", { level: 1, name: "Dashboard" });
    await user.keyboard("{Meta>}k{/Meta}");
    await user.click(await screen.findByRole("option", { name: "Switch to test mode" }));
    expect(screen.getByTestId("test-mode-banner")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Open command palette" }));
    await user.click(await screen.findByRole("option", { name: "Use dark theme" }));
    expect(document.documentElement).toHaveClass("dark");
  });

  it("uses a sheet for navigation on mobile", async () => {
    setViewport("mobile");
    mockApi({ "GET /admin/apps": { data: [], next_cursor: null } });
    const { user, router } = renderApp("/");
    await user.click(await screen.findByRole("button", { name: "Open navigation" }));
    const sheet = await screen.findByRole("dialog");
    await user.click(within(sheet).getByRole("link", { name: "Apps" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/apps"));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("shows the user's name, email and role in the user menu", async () => {
    mockApi({ "GET /admin/me": me({ name: "Vera Viewer", email: "vera@example.com", role: "viewer" }) });
    const { user } = renderApp("/");
    await user.click(await screen.findByRole("button", { name: "Account menu" }));
    const identity = await screen.findByTestId("user-menu-identity");
    expect(identity).toHaveTextContent("Vera Viewer");
    expect(identity).toHaveTextContent("vera@example.com");
    expect(identity).toHaveTextContent("Role: Viewer");
  });

  it("signs out through POST /admin/logout, clears the cache and goes to sign-in", async () => {
    setTestCookie("keipay_csrf", "csrf-1");
    let signedIn = true;
    const api = mockApi({
      "GET /admin/me": () => (signedIn ? json(200, me()) : signedOut()),
      "POST /admin/logout": () => {
        signedIn = false;
        return new Response(null, { status: 204 });
      },
    });
    const { user, router, queryClient } = renderApp("/");
    await user.click(await screen.findByRole("button", { name: "Account menu" }));
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toBe("");
    expect(api.to("POST", "/admin/logout")).toHaveLength(1);
    expect(api.to("POST", "/admin/logout")[0]!.headers.get("X-CSRF-Token")).toBe("csrf-1");
    expect(await screen.findByRole("button", { name: "Sign in" })).toBeInTheDocument();
    expect(queryClient.getQueryData(["me"])).toBeUndefined();
  });

  it("stays signed in with a toast when sign-out fails", async () => {
    setTestCookie("keipay_csrf", "csrf-1");
    mockApi({
      "POST /admin/logout": () => {
        throw new TypeError("Failed to fetch");
      },
    });
    const { user, router } = renderApp("/");
    await user.click(await screen.findByRole("button", { name: "Account menu" }));
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));
    expect(await screen.findByText("Couldn't sign out. Check your connection and try again.")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
    expect(screen.getByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument();
  });

  it("offers Users in the palette only with users:manage", async () => {
    const { user, unmount } = renderApp("/");
    await screen.findByRole("heading", { level: 1, name: "Dashboard" });
    await user.keyboard("{Control>}k{/Control}");
    expect(await screen.findByRole("option", { name: "Users" })).toBeInTheDocument();
    unmount();

    mockApi({ "GET /admin/me": me({ role: "admin" }) });
    const second = renderApp("/");
    await screen.findByRole("heading", { level: 1, name: "Dashboard" });
    await second.user.keyboard("{Control>}k{/Control}");
    await screen.findByRole("dialog", { name: "Command palette" });
    expect(screen.queryByRole("option", { name: "Users" })).not.toBeInTheDocument();
  });
});

describe("Routing", () => {
  it.each([
    ["/tenants", "/apps"],
    ["/deliveries", "/webhooks"],
    ["/ledger", "/reconciliation"],
    ["/no-such-page", "/"],
  ])("redirects %s to %s", async (from, to) => {
    mockApi({
      "GET /admin/apps": { data: [], next_cursor: null },
      "GET /admin/webhook-deliveries": { data: [], next_cursor: null },
      "GET /admin/payment-reports/summary": { interval: "total", from: "", to: "", data: [] },
    });
    const { router } = renderApp(from);
    await waitFor(() => expect(router.state.location.pathname).toBe(to));
  });

  it("sends signed-out users to the login page", async () => {
    mockApi({ "GET /admin/me": signedOut });
    const { router } = renderApp("/payments");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(screen.queryByText("Your session has expired. Sign in again.")).not.toBeInTheDocument();
  });

  it("ends an expired session with the session-expired message and a cleared cache", async () => {
    let expired = false;
    mockApi({
      "GET /admin/me": () => (expired ? signedOut() : json(200, me())),
      "GET /admin/apps": () => {
        expired = true;
        return json(401, errorBody("session_expired", "session expired"));
      },
    });
    const { router, queryClient } = renderApp("/apps");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toBe("?reason=session_expired");
    expect(await screen.findByText("Your session has expired. Sign in again.")).toBeInTheDocument();
    expect(queryClient.getQueryData(["apps", "list", { environment: "live", status: null }])).toBeUndefined();
  });

  it("sends a missing session to sign-in without the message", async () => {
    let gone = false;
    mockApi({
      "GET /admin/me": () => (gone ? signedOut() : json(200, me())),
      "GET /admin/apps": () => {
        gone = true;
        return json(401, errorBody("session_required", "session required"));
      },
    });
    const { router } = renderApp("/apps");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toBe("");
    expect(await screen.findByRole("button", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.queryByText("Your session has expired. Sign in again.")).not.toBeInTheDocument();
  });

  it("sends a pending password change to the change-password page", async () => {
    let pending = false;
    mockApi({
      "GET /admin/me": () => json(200, me({ must_change_password: pending })),
      "GET /admin/apps": () => {
        pending = true;
        return json(403, errorBody("password_change_required", "password change required"));
      },
    });
    const { router } = renderApp("/apps");
    await waitFor(() => expect(router.state.location.pathname).toBe("/change-password"));
    expect(await screen.findByRole("heading", { level: 1, name: "Set a new password" })).toBeInTheDocument();
  });

});
