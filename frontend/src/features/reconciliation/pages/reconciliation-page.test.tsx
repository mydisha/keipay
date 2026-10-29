import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import {
  app,
  APP_ID,
  CON_ID,
  connectionListItem,
  me,
  page,
  paymentReport,
  provider,
  REC_ID,
  reconciliationRun,
  summaryRow,
} from "@/test/fixtures";
import { errorBody, json, mockApi, type Handler } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

// The clock is 12:00 WIB on 15 October
// 2026, so "This month" is 1-15 October.

const NOW = new Date("2026-10-15T05:00:00Z");
const MONTH_FROM = "2026-10-01T00:00:00+07:00";
const MONTH_TO = "2026-10-16T00:00:00+07:00";
const SUMMARY = "/admin/payment-reports/summary";
const REPORTS = "/admin/payment-reports";
const RUNS = "/admin/reconciliation-runs";

const LIVE_CON = "con_01JBKQ7Z8M4X2Y6V3N9T5R1W1A";
const MIDTRANS_CON = "con_01JBKQ7Z8M4X2Y6V3N9T5R1W1B";
const DISABLED_CON = "con_01JBKQ7Z8M4X2Y6V3N9T5R1W1C";

const connections = [
  connectionListItem(),
  connectionListItem({ id: LIVE_CON, name: "xendit-live", environment: "live" }),
  connectionListItem({ id: MIDTRANS_CON, name: "midtrans-test", provider: "midtrans" }),
  connectionListItem({ id: DISABLED_CON, name: "xendit-old", status: "disabled" }),
];

const providers = [
  provider(),
  provider({
    name: "midtrans",
    display_name: "Midtrans",
    capabilities: { ...provider().capabilities, list_transactions: false },
  }),
];

beforeEach(() => {
  setViewport("desktop");
  vi.useFakeTimers({ toFake: ["Date"], now: NOW });
});

afterEach(() => {
  vi.useRealTimers();
});

function routes(extra: Record<string, Handler | object> = {}) {
  return mockApi({
    "GET /admin/apps": page([app()]),
    "GET /admin/connections": page(connections),
    "GET /admin/providers": { data: providers },
    [`GET ${SUMMARY}`]: (request: Request) => {
      const url = new URL(request.url);
      return json(200, {
        interval: url.searchParams.get("interval") ?? "total",
        from: url.searchParams.get("from"),
        to: url.searchParams.get("to"),
        data: [summaryRow()],
      });
    },
    [`GET ${REPORTS}`]: page([paymentReport()]),
    [`GET ${RUNS}`]: page([reconciliationRun()]),
    ...extra,
  });
}

function lastQuery(api: ReturnType<typeof routes>, path: string): Record<string, string> {
  const requests = api.to("GET", path);
  return Object.fromEntries(new URL(requests[requests.length - 1]!.url).searchParams);
}

function useTestMode() {
  window.localStorage.setItem(storageKeys.mode, "test");
}

describe("Summary tab", () => {
  it("requests this month in WIB for the current mode and shows totals and payments", async () => {
    const api = routes({
      [`GET ${SUMMARY}`]: {
        interval: "total",
        from: MONTH_FROM,
        to: MONTH_TO,
        data: [
          summaryRow({ fee_unknown_count: 1 }),
          summaryRow({ currency: "USD", gross: 1050, fee: 0, net: 1050, count: 1, connection: null }),
        ],
      },
    });
    renderApp("/reconciliation");
    const totals = await screen.findByRole("table", { name: "Totals" });
    await within(totals).findByText("Rp295.005");
    expect(lastQuery(api, SUMMARY)).toEqual({ from: MONTH_FROM, to: MONTH_TO, environment: "live", interval: "total" });
    const rows = within(totals).getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("Total");
    expect(within(rows[0]!).getByRole("link", { name: "Demo App" })).toHaveAttribute("href", `/apps/${APP_ID}`);
    expect(rows[0]).toHaveTextContent("xendit-test");
    expect(rows[0]).toHaveTextContent("Rp300.000");
    expect(rows[0]).toHaveTextContent("Rp295.005");
    expect(rows[0]).toHaveTextContent("1 without fee");
    // Amounts use the row's own currency; no connection shows a dash.
    expect(rows[1]).toHaveTextContent("US$10,50");
    expect(within(rows[1]!).getAllByText("—").length).toBeGreaterThan(0);
    expect(rows[1]).not.toHaveTextContent("without fee");

    const reports = await screen.findByRole("table", { name: "Payments" });
    await within(reports).findByText("pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0D");
    expect(within(reports).getByText("Paid")).toBeInTheDocument();
    expect(lastQuery(api, REPORTS)).toMatchObject({ from: MONTH_FROM, to: MONTH_TO, environment: "live" });
    expect(screen.getByText("Showing live data. The page follows the Test/Live switch.")).toBeInTheDocument();
  });

  it("follows test mode", async () => {
    useTestMode();
    const api = routes();
    renderApp("/reconciliation");
    await screen.findByRole("table", { name: "Totals" });
    expect(lastQuery(api, SUMMARY).environment).toBe("test");
    await waitFor(() => expect(lastQuery(api, REPORTS).environment).toBe("test"));
  });

  it("switches the interval and names each period", async () => {
    const api = routes({
      [`GET ${SUMMARY}`]: (request: Request) =>
        json(200, {
          interval: new URL(request.url).searchParams.get("interval"),
          from: MONTH_FROM,
          to: MONTH_TO,
          data: [summaryRow({ period_start: "2026-10-01T17:00:00Z", period_end: "2026-10-02T17:00:00Z" })],
        }),
    });
    const { user, router } = renderApp("/reconciliation");
    await screen.findByRole("table", { name: "Totals" });
    await user.click(within(screen.getByRole("group", { name: "Interval" })).getByRole("button", { name: "Day" }));
    await waitFor(() => expect(lastQuery(api, SUMMARY).interval).toBe("day"));
    expect(await screen.findByText("2 Okt 2026")).toBeInTheDocument();
    expect(router.state.location.search).toContain("interval=day");
  });

  it("applies a preset and the chips to the query and the export link", async () => {
    useTestMode();
    const api = routes({
      [`GET ${SUMMARY}`]: {
        interval: "total",
        from: MONTH_FROM,
        to: MONTH_TO,
        data: [summaryRow(), summaryRow({ currency: "USD" })],
      },
    });
    const { user } = renderApp("/reconciliation");
    await screen.findByRole("table", { name: "Totals" });

    await user.click(within(screen.getByRole("group", { name: "Period" })).getByRole("button", { name: "Last month" }));
    await waitFor(() =>
      expect(lastQuery(api, SUMMARY)).toMatchObject({
        from: "2026-09-01T00:00:00+07:00",
        to: "2026-10-01T00:00:00+07:00",
      }),
    );

    // Only connections of the current mode are offered.
    const connectionChips = await screen.findByRole("group", { name: "Filter by connection" });
    expect(within(connectionChips).getAllByRole("button").map((b) => b.textContent)).toEqual([
      "xendit-test",
      "midtrans-test",
      "xendit-old",
    ]);
    await user.click(within(connectionChips).getByRole("button", { name: "xendit-test" }));
    await waitFor(() => expect(lastQuery(api, SUMMARY).connection_id).toBe(CON_ID));

    await user.click(
      within(await screen.findByRole("group", { name: "Filter by currency" })).getByRole("button", { name: "USD" }),
    );
    await waitFor(() => expect(lastQuery(api, SUMMARY).currency).toBe("USD"));

    await user.click(screen.getByRole("combobox", { name: "App" }));
    await user.click(await screen.findByRole("option", { name: "Demo App (Test)" }));
    await waitFor(() => expect(lastQuery(api, SUMMARY).app_id).toBe(APP_ID));

    const expected = {
      from: "2026-09-01T00:00:00+07:00",
      to: "2026-10-01T00:00:00+07:00",
      environment: "test",
      app_id: APP_ID,
      connection_id: CON_ID,
      currency: "USD",
    };
    expect(lastQuery(api, REPORTS)).toMatchObject(expected);
    const href = screen.getByRole("link", { name: "Export CSV" }).getAttribute("href")!;
    const exportUrl = new URL(href, "http://localhost");
    expect(exportUrl.pathname).toBe("/admin/payment-reports/export");
    expect(Object.fromEntries(exportUrl.searchParams)).toEqual(expected);
    expect(screen.getByRole("link", { name: "Export CSV" })).toHaveAttribute("download");
  });

  it("keeps every currency chip while one currency is chosen", async () => {
    const api = routes({
      [`GET ${SUMMARY}`]: (request: Request) => {
        const currency = new URL(request.url).searchParams.get("currency");
        const rows = [summaryRow(), summaryRow({ currency: "USD" })].filter((row) => !currency || row.currency === currency);
        return json(200, { interval: "total", from: MONTH_FROM, to: MONTH_TO, data: rows });
      },
    });
    const { user } = renderApp("/reconciliation");
    const chips = await screen.findByRole("group", { name: "Filter by currency" });
    await user.click(within(chips).getByRole("button", { name: "USD" }));
    await waitFor(() => expect(lastQuery(api, SUMMARY).currency).toBe("USD"));
    // The totals now hold only USD rows.
    await waitFor(() => expect(within(screen.getByRole("table", { name: "Totals" })).queryByText(/Rp/)).toBeNull());
    const names = within(screen.getByRole("group", { name: "Filter by currency" }))
      .getAllByRole("button")
      .map((b) => b.textContent);
    expect(names).toEqual(expect.arrayContaining(["IDR", "USD"]));
  });

  it("hides the currency chips when only one currency appears", async () => {
    routes();
    renderApp("/reconciliation");
    await screen.findByRole("table", { name: "Totals" });
    expect(screen.queryByRole("group", { name: "Filter by currency" })).not.toBeInTheDocument();
  });

  it("blocks an invalid range with a message instead of a request", async () => {
    const api = routes();
    renderApp("/reconciliation?from=2026-10-10&to=2026-10-01");
    expect(await screen.findByRole("alert")).toHaveTextContent("The end date can't be before the start date.");
    expect(screen.getByLabelText("To (WIB)")).toHaveAttribute("aria-invalid", "true");
    expect(api.to("GET", SUMMARY)).toHaveLength(0);
    expect(api.to("GET", REPORTS)).toHaveLength(0);
    expect(screen.queryByRole("link", { name: "Export CSV" })).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("To (WIB)"), { target: { value: "2026-10-12" } });
    await waitFor(() =>
      expect(lastQuery(api, SUMMARY)).toMatchObject({
        from: "2026-10-10T00:00:00+07:00",
        to: "2026-10-13T00:00:00+07:00",
      }),
    );
  });

  it("limits daily totals to 92 days", async () => {
    const api = routes();
    renderApp("/reconciliation?from=2026-01-01&to=2026-06-30&interval=day");
    expect(await screen.findByRole("alert")).toHaveTextContent(/at most 92 days/);
    expect(api.to("GET", SUMMARY)).toHaveLength(0);
  });

  it("shows a server range error as the field message", async () => {
    routes({
      [`GET ${SUMMARY}`]: () =>
        json(400, errorBody("invalid_request", "invalid request", { fields: [{ field: "to", rule: "max_range" }] })),
    });
    renderApp("/reconciliation");
    expect(await screen.findByRole("alert")).toHaveTextContent("Choose a range of at most 366 days.");
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });

  it("shows the empty state without the payments list", async () => {
    routes({ [`GET ${SUMMARY}`]: { interval: "total", from: MONTH_FROM, to: MONTH_TO, data: [] } });
    renderApp("/reconciliation");
    expect(await screen.findByText("No paid payments in this period")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View payments" })).toHaveAttribute("href", "/payments");
    expect(screen.queryByRole("table", { name: "Payments" })).not.toBeInTheDocument();
  });

  it("offers to clear filters when the filters empty it", async () => {
    routes({ [`GET ${SUMMARY}`]: { interval: "total", from: MONTH_FROM, to: MONTH_TO, data: [] } });
    const { user, router } = renderApp(`/reconciliation?currency=USD&app_id=${APP_ID}`);
    await screen.findByText("No paid payments in this period");
    const empty = screen.getByTestId("empty-state");
    await user.click(within(empty).getByRole("button", { name: "Clear filters" }));
    await waitFor(() => expect(router.state.location.search).not.toContain("currency"));
    expect(router.state.location.search).not.toContain("app_id");
  });

  it("shows loading skeletons, then an error with retry", async () => {
    let fail = true;
    routes({
      [`GET ${SUMMARY}`]: () =>
        fail
          ? json(500, errorBody("internal_error", "internal"))
          : json(200, { interval: "total", from: MONTH_FROM, to: MONTH_TO, data: [summaryRow()] }),
    });
    const { user } = renderApp("/reconciliation");
    expect(await screen.findAllByTestId("skeleton-row")).not.toHaveLength(0);
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("table", { name: "Totals" })).toBeInTheDocument();
  });

  it("renders cards on a phone", async () => {
    setViewport("mobile");
    routes();
    renderApp("/reconciliation");
    expect(await screen.findByRole("list", { name: "Totals" })).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
});

describe("Runs tab", () => {
  it("lists runs of the mode with text status labels and opens one", async () => {
    const api = routes({
      [`GET ${RUNS}`]: page([
        reconciliationRun(),
        reconciliationRun({
          id: "rec_01JBKQ7Z8M4X2Y6V3N9T5R1W1D",
          status: "failed",
          summary: null,
          error: { code: "provider_rejected", message: "The gateway rejected the request." },
          created_by: null,
        }),
      ]),
    });
    const { user, router } = renderApp("/reconciliation?tab=runs");
    const table = await screen.findByRole("table", { name: "Runs" });
    await within(table).findByText("Olivia Owner");
    expect(lastQuery(api, RUNS)).toMatchObject({ environment: "live" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("Succeeded");
    expect(rows[0]).toHaveTextContent("1 Okt 2026 – 7 Okt 2026");
    expect(rows[0]).toHaveTextContent("xendit-test");
    expect(rows[0]).toHaveTextContent("Olivia Owner");
    expect(rows[1]).toHaveTextContent("Failed");
    expect(rows[1]).toHaveTextContent("System");
    await user.click(within(rows[0]!).getByRole("link", { name: `View run ${REC_ID}` }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/reconciliation/runs/${REC_ID}`));
  });

  it("filters by status", async () => {
    const api = routes();
    const { user } = renderApp("/reconciliation?tab=runs");
    await screen.findByRole("table", { name: "Runs" });
    await user.click(within(screen.getByRole("group", { name: "Filter by status" })).getByRole("button", { name: "Failed" }));
    await waitFor(() => expect(lastQuery(api, RUNS).status).toBe("failed"));
  });

  it("polls while a run is active and stops once it finished", async () => {
    vi.useRealTimers();
    vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
    let calls = 0;
    const api = routes({
      [`GET ${RUNS}`]: () => {
        calls += 1;
        return json(200, page([reconciliationRun({ status: calls === 1 ? "running" : "succeeded", summary: null })]));
      },
    });
    renderApp("/reconciliation?tab=runs");
    const table = await screen.findByRole("table", { name: "Runs" });
    expect(await within(table).findByText("Running")).toBeInTheDocument();
    await vi.advanceTimersByTimeAsync(3000);
    expect(await within(table).findByText("Succeeded")).toBeInTheDocument();
    const after = api.to("GET", RUNS).length;
    await vi.advanceTimersByTimeAsync(9000);
    expect(api.to("GET", RUNS)).toHaveLength(after);
  });

  it("explains reconciliation in the empty state and offers a run", async () => {
    routes({ [`GET ${RUNS}`]: page([]) });
    renderApp("/reconciliation?tab=runs");
    const empty = await screen.findByTestId("empty-state");
    expect(empty).toHaveTextContent("No reconciliation runs yet");
    expect(within(empty).getByRole("button", { name: "Run reconciliation" })).toBeInTheDocument();
  });

  it("hides run actions from a viewer and says who can start one", async () => {
    routes({ [`GET ${RUNS}`]: page([]), "GET /admin/me": me({ role: "viewer" }) });
    renderApp("/reconciliation?tab=runs");
    const empty = await screen.findByTestId("empty-state");
    expect(empty).toHaveTextContent("Runs appear here once an owner or admin starts one.");
    expect(screen.queryByRole("button", { name: "Run reconciliation" })).not.toBeInTheDocument();
  });

  it("shows an error with retry", async () => {
    routes({ [`GET ${RUNS}`]: () => json(500, errorBody("internal_error", "internal")) });
    renderApp("/reconciliation?tab=runs");
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  });
});

describe("Run dialog", () => {
  async function openDialog(path = "/reconciliation?tab=runs") {
    const rendered = renderApp(path);
    await screen.findByRole("table", { name: "Runs" });
    await rendered.user.click(screen.getByRole("button", { name: "Run reconciliation" }));
    const dialog = await screen.findByRole("dialog", { name: "Run reconciliation" });
    return { ...rendered, dialog };
  }

  it("offers only connections of the mode whose provider lists transactions", async () => {
    useTestMode();
    routes();
    const { user, dialog } = await openDialog();
    await user.click(await within(dialog).findByRole("combobox", { name: "Connection" }));
    const options = (await screen.findAllByRole("option")).map((o) => o.textContent);
    expect(options).toEqual(["xendit-test", "xendit-old (disabled)"]);
  });

  it("validates before sending", async () => {
    useTestMode();
    const api = routes();
    const { user, dialog } = await openDialog();
    await within(dialog).findByRole("combobox", { name: "Connection" });
    fireEvent.change(within(dialog).getByLabelText("From (WIB)"), { target: { value: "2026-09-01" } });
    fireEvent.change(within(dialog).getByLabelText("To (WIB)"), { target: { value: "2026-10-14" } });
    await user.click(within(dialog).getByRole("button", { name: "Start run" }));
    expect(await within(dialog).findByText("Choose a connection.")).toBeInTheDocument();
    expect(within(dialog).getByText("Choose a range of at most 31 days.")).toBeInTheDocument();
    expect(api.to("POST", RUNS)).toHaveLength(0);
  });

  it("puts server field errors on their fields", async () => {
    useTestMode();
    routes({
      [`POST ${RUNS}`]: () =>
        json(
          400,
          errorBody("invalid_request", "invalid request", {
            fields: [
              { field: "connection_id", rule: "exists" },
              { field: "to", rule: "max_range" },
            ],
          }),
        ),
    });
    const { user, dialog } = await openDialog();
    await user.click(await within(dialog).findByRole("combobox", { name: "Connection" }));
    await user.click(await screen.findByRole("option", { name: "xendit-test" }));
    await user.click(within(dialog).getByRole("button", { name: "Start run" }));
    expect(await within(dialog).findByText("That connection no longer exists. Pick another one.")).toBeInTheDocument();
    expect(within(dialog).getByText("Choose a range of at most 31 days.")).toBeInTheDocument();
  });

  it("starts yesterday's run and opens it", async () => {
    useTestMode();
    const api = routes({
      [`POST ${RUNS}`]: () => json(201, { run: reconciliationRun({ status: "queued", summary: null }) }),
      [`GET ${RUNS}/${REC_ID}`]: { run: reconciliationRun({ status: "queued", summary: null }) },
    });
    const { user, dialog, router } = await openDialog();
    await user.click(await within(dialog).findByRole("combobox", { name: "Connection" }));
    await user.click(await screen.findByRole("option", { name: "xendit-old (disabled)" }));
    await user.click(within(dialog).getByRole("button", { name: "Start run" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/reconciliation/runs/${REC_ID}`));
    expect(await api.to("POST", RUNS)[0]!.json()).toEqual({
      connection_id: DISABLED_CON,
      from: "2026-10-14T00:00:00+07:00",
      to: "2026-10-15T00:00:00+07:00",
    });
    expect(await screen.findByText("Reconciliation run started")).toBeInTheDocument();
  });

  it("explains when no connection can be reconciled", async () => {
    routes({ "GET /admin/connections": page([connectionListItem({ id: MIDTRANS_CON, name: "midtrans-live", provider: "midtrans", environment: "live" })]) });
    const { dialog } = await openDialog();
    expect(await within(dialog).findByText("No connection can be reconciled")).toBeInTheDocument();
    expect(within(dialog).getByRole("link", { name: "View connections" })).toHaveAttribute("href", "/connections");
    expect(within(dialog).queryByRole("button", { name: "Start run" })).not.toBeInTheDocument();
  });

  it("shows the server's refusal as a toast", async () => {
    useTestMode();
    routes({ [`POST ${RUNS}`]: () => json(403, errorBody("forbidden", "forbidden")) });
    const { user, dialog } = await openDialog();
    await user.click(await within(dialog).findByRole("combobox", { name: "Connection" }));
    await user.click(await screen.findByRole("option", { name: "xendit-test" }));
    await user.click(within(dialog).getByRole("button", { name: "Start run" }));
    expect(await screen.findByText("Your role doesn't allow this action.")).toBeInTheDocument();
  });
});

describe("Permissions", () => {
  it("shows the forbidden state without payments:read", async () => {
    routes({ "GET /admin/me": me({}, ["apps:read"]) });
    renderApp("/reconciliation");
    expect(await screen.findByTestId("forbidden-state")).toBeInTheDocument();
  });
});
