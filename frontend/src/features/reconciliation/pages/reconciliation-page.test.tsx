import { screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { setToken } from "@/shared/api/session";
import { tenant } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp, TEST_TOKEN } from "@/test/render";

beforeEach(() => {
  setToken(TEST_TOKEN);
  setViewport("desktop");
});

describe("Reconciliation page", () => {
  it("shows per-app totals and ledger entries", async () => {
    mockApi({
      "GET /admin/tenants": { tenants: [tenant()] },
      "GET /admin/ledger": {
        entries: [
          { id: 3, tenant_id: 1, payment_id: 10, amount: 150000, xendit_fee: 4500, net_amount: 145500, type: "PAYMENT", created_at: "2026-01-01T00:00:00Z" },
        ],
        summary: [{ tenant_id: 1, count: 1, gross_amount: 150000, total_fee: 4500, net_amount: 145500 }],
      },
    });
    renderApp("/reconciliation");
    const totals = await screen.findByRole("list", { name: "Totals per app" });
    expect(within(totals).getByText("Rp145.500")).toBeInTheDocument();
    expect(await within(totals).findByText("Demo App")).toBeInTheDocument();
    const table = screen.getByRole("table", { name: "Ledger entries" });
    expect(within(table).getByText("Rp4.500")).toBeInTheDocument();
  });

  it("shows the empty state", async () => {
    mockApi({ "GET /admin/tenants": { tenants: [] }, "GET /admin/ledger": { entries: [], summary: [] } });
    renderApp("/reconciliation");
    expect(await screen.findByText("No ledger entries yet")).toBeInTheDocument();
  });

  it("shows an error state", async () => {
    mockApi({
      "GET /admin/tenants": { tenants: [] },
      "GET /admin/ledger": () => json(500, errorBody("internal_error", "internal server error")),
    });
    renderApp("/reconciliation");
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  });
});
