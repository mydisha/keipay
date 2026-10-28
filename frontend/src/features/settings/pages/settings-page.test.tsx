import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { setToken } from "@/shared/api/session";
import { auditLog } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp, TEST_TOKEN } from "@/test/render";

beforeEach(() => {
  setToken(TEST_TOKEN);
  setViewport("desktop");
});

const first = auditLog();
const second = auditLog({
  id: "aud_01JBKQ7Z8M4X2Y6V3N9T5R1W0D",
  action: "tenant.rotate_credentials",
  actor: { type: "admin", id: "admin" },
  before: { api_key_last4: "0001" },
  after: { api_key_last4: "0002" },
});

describe("Settings page (AC-25)", () => {
  it("pages through the audit log with cursors", async () => {
    const api = mockApi({
      "GET /admin/audit-logs": (request: Request) => {
        const cursor = new URL(request.url).searchParams.get("cursor");
        return json(200, cursor === "c2" ? { data: [second], next_cursor: null } : { data: [first], next_cursor: "c2" });
      },
    });
    const { user } = renderApp("/settings");
    expect(await screen.findByText("tenant.create")).toBeInTheDocument();
    expect(screen.getByText("system:seed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText("tenant.rotate_credentials")).toBeInTheDocument();
    const lastUrl = new URL(api.to("GET", "/admin/audit-logs").at(-1)!.url);
    expect(lastUrl.searchParams.get("cursor")).toBe("c2");
    expect(lastUrl.searchParams.get("limit")).toBe("50");
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Previous" }));
    expect(await screen.findByText("tenant.create")).toBeInTheDocument();
  });

  it("opens an entry with before and after JSON", async () => {
    mockApi({ "GET /admin/audit-logs": { data: [second], next_cursor: null } });
    const { user } = renderApp("/settings");
    await user.click(await screen.findByRole("button", { name: "View tenant.rotate_credentials" }));
    const dialog = await screen.findByRole("dialog", { name: "tenant.rotate_credentials" });
    expect(within(dialog).getByRole("button", { name: "Before" })).toHaveAttribute("aria-expanded", "true");
    expect(within(dialog).getByText(/"api_key_last4": "0001"/)).toBeInTheDocument();
    expect(within(dialog).getByText(/"api_key_last4": "0002"/)).toBeInTheDocument();
  });

  it("shows the empty audit state, account link and users placeholder", async () => {
    mockApi({ "GET /admin/audit-logs": { data: [], next_cursor: null } });
    renderApp("/settings");
    expect(await screen.findByText("No admin changes recorded yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Change password" })).toHaveAttribute("href", "/change-password");
    expect(screen.getByText("User management is coming soon")).toBeInTheDocument();
  });

  it("goes back to the first page when the cursor is rejected", async () => {
    mockApi({
      "GET /admin/audit-logs": (request: Request) => {
        const cursor = new URL(request.url).searchParams.get("cursor");
        if (cursor === "stale") return json(400, errorBody("invalid_cursor", "invalid cursor"));
        return json(200, { data: [first], next_cursor: "stale" });
      },
    });
    const { user } = renderApp("/settings");
    await user.click(await screen.findByRole("button", { name: "Next" }));
    expect(await screen.findByText(/That page is no longer available/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Page 1")).toBeInTheDocument());
    expect(screen.getByText("tenant.create")).toBeInTheDocument();
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });

  it("shows an error state with retry", async () => {
    mockApi({ "GET /admin/audit-logs": () => json(500, errorBody("internal_error", "internal server error")) });
    renderApp("/settings");
    const error = await screen.findByTestId("error-state");
    expect(within(error).getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });
});
