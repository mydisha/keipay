import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { CON_ID, dashboard, dashboardCounts, dashboardSeries, dashboardTotals, me, noDashboardTotals, PAY_ID, payment, REC_ID } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { stubElementSize } from "@/test/layout";
import { renderApp } from "@/test/render";

const DASHBOARD = "GET /admin/dashboard";

function lastQuery(api: ReturnType<typeof mockApi>) {
  const request = api.to("GET", "/admin/dashboard").at(-1);
  return request ? Object.fromEntries(new URL(request.url).searchParams) : undefined;
}

/** The figure that carries `label` (its `dt` and `dd`); the nav also says "Payments", so match the term only. */
function kpi(label: string) {
  return within(screen.getByRole("main")).getByText(label, { selector: "dt" }).parentElement!;
}

function emptyDashboard() {
  return dashboard({
    payments: { totals: noDashboardTotals(), by_currency: [], series: dashboardSeries(7, []) },
    deliveries: { total: 0, pending: 0, delivered: 0, failed: 0, finished: 0, success_rate: null },
    inbound: {
      total: 0,
      applied: 0,
      problems: 0,
      by_result: { applied: 0, stale: 0, duplicate: 0, mismatch: 0, unmapped: 0, invalid_signature: 0, invalid_payload: 0, error: 0 },
    },
    connections: [],
    reconciliation: { unresolved_items: 0, latest_run: null },
    recent_payments: [],
  });
}

beforeEach(() => {
  stubElementSize(640, 240);
});

describe("Dashboard page", () => {
  it("shows the figures of the range with their change against the previous period", async () => {
    const api = mockApi({ [DASHBOARD]: dashboard() });
    renderApp("/");
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument();
    await screen.findByText("Gross volume");
    const gross = kpi("Gross volume");
    expect(gross).toHaveTextContent("Rp450.000");
    expect(gross).toHaveTextContent("+350%");
    const count = kpi("Payments");
    expect(count).toHaveTextContent("4");
    expect(count).toHaveTextContent("+100%");
    const rate = kpi("Success rate");
    expect(rate).toHaveTextContent("75%");
    expect(rate).toHaveTextContent("+25 pp");
    expect(rate).toHaveTextContent("3 of 4 finished");
    const delivery = kpi("Delivery success");
    expect(delivery).toHaveTextContent("75%");
    expect(delivery).toHaveTextContent("3 of 4 delivered");
    expect(lastQuery(api)).toEqual({ range: "7d", environment: "live" });
    // The comparison is named once, in the header, not under every figure.
    expect(screen.getByText("Live apps and connections over the last 7 days. Changes compare with the 7 days before.")).toBeInTheDocument();
    expect(screen.queryByText(/vs previous/)).not.toBeInTheDocument();
    expect(screen.queryByTestId("dashboard-skeleton")).not.toBeInTheDocument();
    expect(document.querySelectorAll("[aria-busy=true]")).toHaveLength(0);
  });

  it("asks for 30 days from the chip and keeps the range in the URL", async () => {
    const api = mockApi({ [DASHBOARD]: () => json(200, dashboard({ range: "30d", payments: { totals: noDashboardTotals(), by_currency: [], series: dashboardSeries(30, []) } })) });
    const { user, router } = renderApp("/");
    const group = await screen.findByRole("group", { name: "Range" });
    expect(within(group).getByRole("button", { name: "Last 7 days" })).toHaveAttribute("aria-pressed", "true");
    await user.click(within(group).getByRole("button", { name: "Last 30 days" }));
    await waitFor(() => expect(lastQuery(api)).toEqual({ range: "30d", environment: "live" }));
    expect(router.state.location.search).toBe("?range=30d");
    expect(within(group).getByRole("button", { name: "Last 30 days" })).toHaveAttribute("aria-pressed", "true");
  });

  it("reads the range from the URL on load", async () => {
    const api = mockApi({ [DASHBOARD]: dashboard({ range: "30d" }) });
    renderApp("/?range=30d");
    await screen.findByText("Gross volume");
    expect(lastQuery(api)).toEqual({ range: "30d", environment: "live" });
    expect(screen.getByRole("button", { name: "Last 30 days" })).toHaveAttribute("aria-pressed", "true");
  });

  it("falls back to 7 days for an unknown range in the URL instead of sending it", async () => {
    const api = mockApi({ [DASHBOARD]: dashboard() });
    renderApp("/?range=3d");
    await screen.findByText("Gross volume");
    expect(lastQuery(api)).toEqual({ range: "7d", environment: "live" });
    expect(api.to("GET", "/admin/dashboard").every((request) => new URL(request.url).searchParams.get("range") === "7d")).toBe(true);
    expect(screen.getByRole("button", { name: "Last 7 days" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });

  it("drops ?range= when going back to the default 7 days", async () => {
    const api = mockApi({ [DASHBOARD]: dashboard() });
    const { user, router } = renderApp("/?range=30d");
    const group = await screen.findByRole("group", { name: "Range" });
    await user.click(within(group).getByRole("button", { name: "Last 7 days" }));
    await waitFor(() => expect(lastQuery(api)).toEqual({ range: "7d", environment: "live" }));
    expect(router.state.location.search).toBe("");
  });

  it("follows the Test mode switch and says so", async () => {
    const api = mockApi({ [DASHBOARD]: dashboard() });
    const { user } = renderApp("/");
    await screen.findByText("Gross volume");
    await user.click(screen.getByRole("switch", { name: "Test mode" }));
    await waitFor(() => expect(lastQuery(api)).toEqual({ range: "7d", environment: "test" }));
    expect(screen.getByText(/^Test apps and connections over the last 7 days/)).toBeInTheDocument();
  });

  it("shows an empty state per card, never an error, when the range has nothing", async () => {
    mockApi({ [DASHBOARD]: emptyDashboard() });
    renderApp("/");
    expect(await screen.findByText("No payments in this range")).toBeInTheDocument();
    expect(screen.getByText("No connections yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Add connection" })).toHaveAttribute("href", "/connections/new");
    expect(screen.getByTestId("all-clear")).toHaveTextContent("Nothing to fix");
    expect(screen.getByText("No reconciliation run yet")).toBeInTheDocument();
    expect(screen.getByText("No payments yet")).toBeInTheDocument();
    // Missing figures are written out, never shown as a dash or a change.
    expect(kpi("Gross volume")).toHaveTextContent(/^Gross volumeNone$/);
    expect(kpi("Success rate")).toHaveTextContent("Not yet");
    expect(kpi("Success rate")).toHaveTextContent("No finished payments");
    expect(screen.getByRole("main")).not.toHaveTextContent("—");
    expect(kpi("Delivery success")).toHaveTextContent("No finished deliveries");
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "Currency" })).not.toBeInTheDocument();
  });

  it("hides Add connection from a viewer", async () => {
    mockApi({ "GET /admin/me": me({ role: "viewer" }), [DASHBOARD]: emptyDashboard() });
    renderApp("/");
    expect(await screen.findByText("No connections yet")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Add connection" })).not.toBeInTheDocument();
    expect(screen.getByText(/An admin needs to add a gateway connection/)).toBeInTheDocument();
  });

  it("shows the error state with a working Try again", async () => {
    let calls = 0;
    mockApi({
      [DASHBOARD]: () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "boom")) : json(200, dashboard());
      },
    });
    const { user } = renderApp("/");
    const error = await screen.findByTestId("error-state");
    expect(error).not.toHaveTextContent("boom");
    await user.click(within(error).getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Gross volume")).toBeInTheDocument();
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });

  it("shows the forbidden state for a 403", async () => {
    mockApi({ [DASHBOARD]: () => json(403, errorBody("forbidden", "forbidden")) });
    renderApp("/");
    expect(await screen.findByTestId("forbidden-state")).toBeInTheDocument();
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument();
  });

  it("links every problem, the latest run, the connections and the payments to their page", async () => {
    mockApi({ [DASHBOARD]: dashboard() });
    renderApp("/");
    const attention = await screen.findByRole("list", { name: "Needs attention" });
    expect(within(attention).getByRole("link", { name: /1 failed delivery/ })).toHaveAttribute("href", "/webhooks?status=FAILED");
    expect(within(attention).getByRole("link", { name: /2 rejected callbacks/ })).toHaveAttribute("href", "/webhooks?tab=inbound");
    expect(within(attention).getByRole("link", { name: /2 unresolved reconciliation items/ })).toHaveAttribute(
      "href",
      "/reconciliation?tab=runs",
    );
    expect(screen.getByRole("link", { name: new RegExp(REC_ID) })).toHaveAttribute("href", `/reconciliation/runs/${REC_ID}`);
    const health = screen.getByRole("list", { name: "Connections" });
    expect(within(health).getByRole("link", { name: "xendit-live" })).toHaveAttribute("href", `/connections/${CON_ID}`);
    expect(within(health).getByText("Passed")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View all" })).toHaveAttribute("href", "/connections");
    expect(screen.getByRole("link", { name: "View all payments" })).toHaveAttribute("href", "/payments");
    const recent = screen.getByRole("table", { name: "Recent payments" });
    expect(within(recent).getByText(PAY_ID)).toBeInTheDocument();
    expect(within(recent).getByText("Rp150.000")).toBeInTheDocument();
    expect(within(recent).getByText("Paid")).toBeInTheDocument();
  });

  it("lists at most five connections and says how many more there are", async () => {
    const connections = Array.from({ length: 7 }, (_, i) => ({
      ...dashboard().connections[0]!,
      id: `con_0${i}JBKQ7Z8M4X2Y6V3N9T5R1W0`,
      name: `xendit-${i}`,
    }));
    mockApi({ [DASHBOARD]: dashboard({ connections }) });
    renderApp("/");
    const health = await screen.findByRole("list", { name: "Connections" });
    expect(within(health).getAllByRole("listitem")).toHaveLength(5);
    expect(screen.getByText("2 more")).toBeInTheDocument();
  });

  it("offers the currencies IDR first and switches the gross figure", async () => {
    const series = dashboardSeries(7, ["IDR", "USD"]);
    series[6] = { ...series[6]!, created: 2, successful: 2, gross: [{ currency: "IDR", amount: 150000 }, { currency: "USD", amount: 1050 }] };
    mockApi({
      [DASHBOARD]: dashboard({
        payments: {
          totals: {
            current: dashboardCounts({ created: 2, paid: 2, successful: 2, finished: 2, success_rate: 1 }),
            previous: dashboardCounts({ created: 1, paid: 1, successful: 1, finished: 1, success_rate: 1 }),
          },
          by_currency: [
            {
              currency: "USD",
              current: dashboardTotals({ created: 1, paid: 1, successful: 1, finished: 1, gross: 1050, success_rate: 1 }),
              previous: dashboardTotals(),
            },
            {
              currency: "IDR",
              current: dashboardTotals({ created: 1, paid: 1, successful: 1, finished: 1, gross: 150000, success_rate: 1 }),
              previous: dashboardTotals({ created: 1, paid: 1, successful: 1, finished: 1, gross: 100000, success_rate: 1 }),
            },
          ],
          series,
        },
      }),
    });
    const { user } = renderApp("/");
    const group = await screen.findByRole("group", { name: "Currency" });
    expect(within(group).getAllByRole("button").map((b) => b.textContent)).toEqual(["IDR", "USD"]);
    expect(kpi("Gross volume")).toHaveTextContent("Rp150.000");
    expect(kpi("Gross volume")).toHaveTextContent("+50%");
    await user.click(within(group).getByRole("button", { name: "USD" }));
    expect(kpi("Gross volume")).toHaveTextContent("US$10,50");
    // USD had nothing in the previous period, so there is no change to show.
    expect(kpi("Gross volume")).not.toHaveTextContent("%");
    // Counts and rates come from the totals across every currency, whichever currency is chosen.
    expect(kpi("Payments").querySelector("dd > span")).toHaveTextContent(/^2$/);
    expect(kpi("Success rate")).toHaveTextContent("2 of 2 finished");
  });

  it("renders the chart as a named image with a text table of every day", async () => {
    mockApi({ [DASHBOARD]: dashboard() });
    renderApp("/");
    const chart = await screen.findByRole("img", { name: /payments per day over the last 7 days/ });
    await waitFor(() => expect(chart.querySelectorAll(".recharts-bar-rectangle").length).toBeGreaterThan(0));
    const table = screen.getByRole("table", { name: "Payments per day (WIB)" });
    expect(within(table).getAllByRole("row")).toHaveLength(8);
    expect(within(table).getByRole("columnheader", { name: "Gross IDR" })).toBeInTheDocument();
    const lastDay = within(table).getByRole("rowheader", { name: "29 Okt" }).closest("tr")!;
    expect(lastDay).toHaveTextContent("Rp150.000");
  });

  it("shows the skeleton while loading", async () => {
    mockApi({ [DASHBOARD]: () => new Promise(() => {}) });
    renderApp("/");
    expect(await screen.findByTestId("dashboard-skeleton")).toHaveAttribute("aria-busy", "true");
  });

  it("shows the newest payments in the table", async () => {
    mockApi({ [DASHBOARD]: dashboard({ recent_payments: [payment({ id: "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W1A", status: "PENDING", amount: 25000 })] }) });
    renderApp("/");
    const recent = await screen.findByRole("table", { name: "Recent payments" });
    expect(within(recent).getByText("Rp25.000")).toBeInTheDocument();
    expect(within(recent).getByText("Pending")).toBeInTheDocument();
  });
});
