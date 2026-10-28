import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { getToken, setToken } from "@/shared/api/session";
import { storageKeys } from "@/shared/lib/storage";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp, TEST_TOKEN } from "@/test/render";

const NAV = ["Dashboard", "Apps", "Connections", "Webhooks", "Payments", "Reconciliation", "Settings"];

beforeEach(() => {
  setToken(TEST_TOKEN);
  setViewport("desktop");
});

describe("App shell (T-29)", () => {
  it("shows the final navigation in the sidebar", async () => {
    renderApp("/");
    const nav = await screen.findByRole("navigation", { name: "Main" });
    expect(within(nav).getAllByRole("link").map((a) => a.textContent)).toEqual(NAV);
    expect(within(nav).getByRole("link", { name: "Dashboard" })).toHaveAttribute("aria-current", "page");
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
    expect(window.localStorage.getItem(storageKeys.mode)).toBe("test");

    unmount();
    mockApi({ "GET /admin/payments": { payments: [], total: 0 }, "GET /admin/tenants": { tenants: [] } });
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
    mockApi({ "GET /admin/payments": { payments: [], total: 0 }, "GET /admin/tenants": { tenants: [] } });
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
    mockApi({ "GET /admin/tenants": { tenants: [] } });
    const { user, router } = renderApp("/");
    await user.click(await screen.findByRole("button", { name: "Open navigation" }));
    const sheet = await screen.findByRole("dialog");
    await user.click(within(sheet).getByRole("link", { name: "Apps" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/apps"));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("signs out from the user menu", async () => {
    const { user, router } = renderApp("/");
    await user.click(await screen.findByRole("button", { name: "Account menu" }));
    expect(await screen.findByText("Signed in as admin")).toBeInTheDocument();
    await user.click(screen.getByRole("menuitem", { name: "Sign out" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(getToken()).toBeNull();
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
      "GET /admin/tenants": { tenants: [] },
      "GET /admin/webhook-deliveries": { deliveries: [] },
      "GET /admin/ledger": { entries: [], summary: [] },
    });
    const { router } = renderApp(from);
    await waitFor(() => expect(router.state.location.pathname).toBe(to));
  });

  it("sends signed-out users to the login page", async () => {
    window.localStorage.clear();
    const { router } = renderApp("/payments");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(screen.queryByText("Your session has expired. Sign in again.")).not.toBeInTheDocument();
  });

  it("ends an expired session with the session-expired message (AC-26)", async () => {
    mockApi({ "GET /admin/tenants": () => json(401, errorBody("invalid_token", "invalid or expired token")) });
    const { router } = renderApp("/apps");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(router.state.location.search).toBe("?reason=session_expired");
    expect(await screen.findByText("Your session has expired. Sign in again.")).toBeInTheDocument();
    expect(getToken()).toBeNull();
  });
});
