import { screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { auditLog, delivery, me, tenant } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

// Everything here is driven by `me.permissions`; the role only picks the
// fixture's permission list.
const viewer = me({ role: "viewer" });
const admin = me({ role: "admin" });

beforeEach(() => setViewport("desktop"));

function settingsTabs() {
  const nav = screen.getByRole("navigation", { name: "Settings sections" });
  return within(nav)
    .getAllByRole("link")
    .map((link) => link.textContent);
}

describe("Permission-aware console (T-49, AC-25)", () => {
  it("viewers see apps without Create app or row actions", async () => {
    mockApi({ "GET /admin/me": viewer, "GET /admin/tenants": { tenants: [tenant()] } });
    renderApp("/apps");
    expect(await screen.findByText("Demo App")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create app" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Actions for Demo App" })).not.toBeInTheDocument();
  });

  it("viewers with no apps get an empty state without a create action", async () => {
    mockApi({ "GET /admin/me": viewer, "GET /admin/tenants": { tenants: [] } });
    renderApp("/apps");
    const empty = await screen.findByTestId("empty-state");
    expect(within(empty).queryByRole("button")).not.toBeInTheDocument();
  });

  it("only tenants:write unlocks app actions, whatever the role is called", async () => {
    mockApi({
      "GET /admin/me": me({ role: "viewer" }, ["tenants:read", "tenants:write"]),
      "GET /admin/tenants": { tenants: [tenant()] },
    });
    renderApp("/apps");
    expect(await screen.findByRole("button", { name: "Actions for Demo App" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create app" })).toBeInTheDocument();
  });

  it("viewers see deliveries without Replay", async () => {
    mockApi({
      "GET /admin/me": viewer,
      "GET /admin/tenants": { tenants: [tenant()] },
      "GET /admin/webhook-deliveries": { deliveries: [delivery()] },
    });
    renderApp("/webhooks");
    expect(await screen.findByText("connection refused")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Replay delivery 7" })).not.toBeInTheDocument();
  });

  it("admins keep Replay", async () => {
    mockApi({
      "GET /admin/me": admin,
      "GET /admin/tenants": { tenants: [tenant()] },
      "GET /admin/webhook-deliveries": { deliveries: [delivery()] },
    });
    renderApp("/webhooks");
    expect(await screen.findByRole("button", { name: "Replay delivery 7" })).toBeInTheDocument();
  });

  it.each([
    ["owner", me(), ["Account", "Users", "Audit log"]],
    ["admin", admin, ["Account", "Audit log"]],
    ["viewer", viewer, ["Account"]],
  ])("settings tabs for an %s", async (_, body, tabs) => {
    mockApi({ "GET /admin/me": body });
    renderApp("/settings");
    await screen.findByRole("region", { name: "Account" });
    expect(settingsTabs()).toEqual(tabs);
  });

  it.each([
    ["/settings/users", viewer],
    ["/settings/users", admin],
    ["/settings/audit-log", viewer],
  ])("%s without the permission shows ForbiddenState, not a redirect", async (path, body) => {
    const api = mockApi({ "GET /admin/me": body });
    const { router } = renderApp(path);
    expect(await screen.findByTestId("forbidden-state")).toHaveTextContent("You don't have access to this page");
    expect(router.state.location.pathname).toBe(path);
    expect(api.requests.filter((r) => r.url.includes("/admin/users") || r.url.includes("/admin/audit-logs"))).toHaveLength(0);
  });

  it("an API 403 forbidden on a page renders ForbiddenState", async () => {
    mockApi({ "GET /admin/audit-logs": () => json(403, errorBody("forbidden", "forbidden")) });
    renderApp("/settings/audit-log");
    expect(await screen.findByTestId("forbidden-state")).toBeInTheDocument();
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });

  it("owners open the audit log tab", async () => {
    mockApi({ "GET /admin/audit-logs": { data: [auditLog()], next_cursor: null } });
    renderApp("/settings/audit-log");
    expect(await screen.findByText("tenant.create")).toBeInTheDocument();
  });
});
