import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { app, APP_ID, appSummary, page, PAY_ID } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

beforeEach(() => {
  setViewport("desktop");
});

describe("Reconciliation page", () => {
  const ledger = {
    entries: [
      {
        id: 3,
        app: appSummary(),
        payment_id: PAY_ID,
        amount: 150000,
        xendit_fee: 4500,
        net_amount: 145500,
        type: "PAYMENT",
        created_at: "2026-01-01T00:00:00Z",
      },
    ],
    summary: [{ app: appSummary(), count: 1, gross_amount: 150000, total_fee: 4500, net_amount: 145500 }],
  };

  it("shows per-app totals and ledger entries with app summaries and pay_ ids (T-47)", async () => {
    mockApi({ "GET /admin/apps": page([]), "GET /admin/ledger": ledger });
    renderApp("/reconciliation");
    const totals = await screen.findByRole("list", { name: "Totals per app" });
    expect(within(totals).getByText("Rp145.500")).toBeInTheDocument();
    expect(within(totals).getByRole("link", { name: "Demo App" })).toHaveAttribute("href", `/apps/${APP_ID}`);
    expect(within(totals).getByText("Test")).toBeInTheDocument();
    const table = screen.getByRole("table", { name: "Ledger entries" });
    expect(within(table).getByText("Rp4.500")).toBeInTheDocument();
    expect(within(table).getByText(PAY_ID)).toBeInTheDocument();
    expect(within(table).getByRole("link", { name: "Demo App" })).toBeInTheDocument();
  });

  it("filters by app public id (T-47)", async () => {
    const api = mockApi({ "GET /admin/apps": page([app()]), "GET /admin/ledger": ledger });
    const { user } = renderApp("/reconciliation");
    await screen.findByRole("list", { name: "Totals per app" });
    await user.click(screen.getByRole("combobox", { name: "App" }));
    await user.click(await screen.findByRole("option", { name: "Demo App (Test)" }));
    await waitFor(() => {
      const requests = api.to("GET", "/admin/ledger");
      expect(new URL(requests[requests.length - 1]!.url).searchParams.get("app_id")).toBe(APP_ID);
    });
  });

  it("shows the empty state", async () => {
    mockApi({ "GET /admin/apps": page([]), "GET /admin/ledger": { entries: [], summary: [] } });
    renderApp("/reconciliation");
    expect(await screen.findByText("No ledger entries yet")).toBeInTheDocument();
  });

  it("shows an error state", async () => {
    mockApi({
      "GET /admin/apps": page([]),
      "GET /admin/ledger": () => json(500, errorBody("internal_error", "internal server error")),
    });
    renderApp("/reconciliation");
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  });
});
