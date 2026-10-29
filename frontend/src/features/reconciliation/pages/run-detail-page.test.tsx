import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  CON_ID,
  gatewaySide,
  keipaySide,
  me,
  page,
  RCI_ID,
  REC_ID,
  reconciliationItem,
  reconciliationRun,
} from "@/test/fixtures";
import { errorBody, json, mockApi, type Handler } from "@/test/http";
import type { ReconciliationItem } from "../api/runs";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

const RUN = `/admin/reconciliation-runs/${REC_ID}`;
const ITEMS = `${RUN}/items`;
const RERUN = `${RUN}/rerun`;
const ITEM = `/admin/reconciliation-items/${RCI_ID}`;
const RESOLVE = `${ITEM}/resolve`;
const MATCHED_ID = "rci_01JBKQ7Z8M4X2Y6V3N9T5R1W1E";

beforeEach(() => setViewport("desktop"));
afterEach(() => vi.useRealTimers());

function defaultItems(): ReconciliationItem[] {
  return [
    reconciliationItem(),
    reconciliationItem({ id: MATCHED_ID, reference: "kp_pay_matched", result: "matched", gateway: gatewaySide() }),
  ];
}

/** The run, its items as one page, and each item's own read. */
function routes(extra: Record<string, Handler | object> = {}, items: ReconciliationItem[] = defaultItems()) {
  const reads = Object.fromEntries(items.map((item) => [`GET /admin/reconciliation-items/${item.id}`, { item }]));
  return mockApi({
    [`GET ${RUN}`]: { run: reconciliationRun() },
    [`GET ${ITEMS}`]: page(items),
    ...reads,
    ...extra,
  });
}

function lastQuery(api: ReturnType<typeof routes>, path: string): Record<string, string> {
  const requests = api.to("GET", path);
  return Object.fromEntries(new URL(requests[requests.length - 1]!.url).searchParams);
}

describe("Run detail", () => {
  it("shows the header facts and a progress state while queued, then the results once it finished", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let calls = 0;
    const api = routes({
      [`GET ${RUN}`]: () => {
        calls += 1;
        return json(200, {
          run: calls === 1 ? reconciliationRun({ status: "queued", summary: null, started_at: null, finished_at: null }) : reconciliationRun(),
        });
      },
    });
    renderApp(`/reconciliation/runs/${REC_ID}`);
    expect(await screen.findByRole("heading", { level: 1, name: REC_ID })).toBeInTheDocument();
    expect(screen.getByText("Queued")).toBeInTheDocument();
    expect(screen.getByTestId("run-progress")).toHaveTextContent("Comparing records…");
    expect(screen.getByText("1 Okt 2026 – 7 Okt 2026")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /xendit-test/ })).toHaveAttribute("href", `/connections/${CON_ID}`);
    expect(screen.getByText("Olivia Owner")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Export CSV" })).not.toBeInTheDocument();
    expect(api.to("GET", ITEMS)).toHaveLength(0);

    await vi.advanceTimersByTimeAsync(2000);
    expect(await screen.findByRole("table", { name: "Items" })).toBeInTheDocument();
    expect(screen.queryByTestId("run-progress")).not.toBeInTheDocument();
    const after = api.to("GET", RUN).length;
    await vi.advanceTimersByTimeAsync(6000);
    expect(api.to("GET", RUN)).toHaveLength(after);
  });

  it("shows a failed run's message and code with the Re-run action", async () => {
    const api = routes({
      [`GET ${RUN}`]: {
        run: reconciliationRun({
          status: "failed",
          summary: null,
          error: { code: "provider_rejected", message: "The gateway rejected the credentials or the request." },
        }),
      },
    });
    renderApp(`/reconciliation/runs/${REC_ID}`);
    const alert = await screen.findByTestId("run-failed");
    expect(alert).toHaveTextContent("This run failed");
    expect(alert).toHaveTextContent("The gateway rejected the credentials or the request.");
    expect(alert).toHaveTextContent("Error code: provider_rejected");
    expect(screen.getByRole("button", { name: "Re-run" })).toBeInTheDocument();
    expect(api.to("GET", ITEMS)).toHaveLength(0);
  });

  it("shows result counts that filter the items, and an export link with the same filters", async () => {
    const api = routes();
    const { user } = renderApp(`/reconciliation/runs/${REC_ID}`);
    const table = await screen.findByRole("table", { name: "Items" });
    await within(table).findByText("kp_pay_matched");
    expect(lastQuery(api, ITEMS)).toEqual({ limit: "100" });

    const results = screen.getByRole("region", { name: "Results" });
    expect(results).toHaveTextContent("Items3");
    expect(results).toHaveTextContent("Unresolved2");
    await user.click(within(results).getByRole("button", { name: "Show amount differs items: 1" }));
    await waitFor(() => expect(lastQuery(api, ITEMS).result).toBe("amount_mismatch"));
    expect(within(results).getByRole("button", { name: "Show amount differs items: 1" })).toHaveAttribute("aria-pressed", "true");

    await user.click(
      within(screen.getByRole("group", { name: "Filter by resolution" })).getByRole("button", { name: "Unresolved" }),
    );
    await waitFor(() => expect(lastQuery(api, ITEMS).resolved).toBe("false"));

    const href = screen.getByRole("link", { name: "Export CSV" }).getAttribute("href")!;
    const url = new URL(href, "http://localhost");
    expect(url.pathname).toBe(`${ITEMS}/export`);
    expect(Object.fromEntries(url.searchParams)).toEqual({ result: "amount_mismatch", resolved: "false" });
  });

  it("lists items with text labels for result, statuses and resolution", async () => {
    routes();
    renderApp(`/reconciliation/runs/${REC_ID}`);
    const table = await screen.findByRole("table", { name: "Items" });
    await within(table).findByText("kp_pay_matched");
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("Amount differs");
    expect(rows[0]).toHaveTextContent("Rp150.000");
    expect(rows[0]).toHaveTextContent("Rp300.000");
    expect(rows[0]).toHaveTextContent("Unresolved");
    expect(rows[1]).toHaveTextContent("Matched");
  });

  it("says when there was nothing to compare", async () => {
    routes({
      [`GET ${RUN}`]: {
        run: reconciliationRun({
          summary: {
            total: 0,
            unresolved: 0,
            results: {
              matched: 0,
              missing_in_keipay: 0,
              missing_in_gateway: 0,
              amount_mismatch: 0,
              status_mismatch: 0,
              fee_mismatch: 0,
            },
          },
        }),
      },
    });
    renderApp(`/reconciliation/runs/${REC_ID}`);
    expect(await screen.findByText("Nothing to compare in this range")).toBeInTheDocument();
  });

  it.each([
    ["an unknown run", () => json(404, errorBody("not_found", "not found"))],
    ["a malformed id", () => json(400, errorBody("invalid_request", "invalid", { fields: [{ field: "id", rule: "public_id" }] }))],
  ])("shows not found for %s", async (_, handler) => {
    routes({ [`GET ${RUN}`]: handler });
    renderApp(`/reconciliation/runs/${REC_ID}`);
    expect(await screen.findByText("Run not found")).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "Back to reconciliation" })[0]).toHaveAttribute("href", "/reconciliation?tab=runs");
  });

  it("shows an error with retry for other failures", async () => {
    routes({ [`GET ${RUN}`]: () => json(500, errorBody("internal_error", "internal")) });
    renderApp(`/reconciliation/runs/${REC_ID}`);
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  });

  it("re-runs after confirmation", async () => {
    const api = routes({ [`POST ${RERUN}`]: () => json(202, { run: reconciliationRun({ status: "queued", run_count: 2, summary: null }) }) });
    const { user } = renderApp(`/reconciliation/runs/${REC_ID}`);
    await user.click(await screen.findByRole("button", { name: "Re-run" }));
    const dialog = await screen.findByRole("dialog", { name: "Re-run this reconciliation?" });
    await user.click(within(dialog).getByRole("button", { name: "Re-run" }));
    expect(await screen.findByText("Run queued again")).toBeInTheDocument();
    expect(api.to("POST", RERUN)).toHaveLength(1);
    expect(await screen.findByTestId("run-progress")).toBeInTheDocument();
  });

  it("says so when the run is still in progress (409)", async () => {
    routes({ [`POST ${RERUN}`]: () => json(409, errorBody("run_in_progress", "run in progress")) });
    const { user } = renderApp(`/reconciliation/runs/${REC_ID}`);
    await user.click(await screen.findByRole("button", { name: "Re-run" }));
    const dialog = await screen.findByRole("dialog", { name: "Re-run this reconciliation?" });
    await user.click(within(dialog).getByRole("button", { name: "Re-run" }));
    expect(await screen.findByText("This run is still in progress. Try again when it finishes.")).toBeInTheDocument();
  });

  it("is read-only for a viewer", async () => {
    routes({ "GET /admin/me": me({ role: "viewer" }) });
    renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    expect(await within(sheet).findByText("Not resolved yet. An owner or admin can resolve it with a note.")).toBeInTheDocument();
    expect(within(sheet).queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Re-run" })).not.toBeInTheDocument();
    // The open sheet hides the page from the accessibility tree.
    expect(screen.getByRole("link", { name: "Export CSV", hidden: true })).toBeInTheDocument();
  });

  it("renders items as cards on a phone", async () => {
    setViewport("mobile");
    routes();
    renderApp(`/reconciliation/runs/${REC_ID}`);
    expect(await screen.findByRole("list", { name: "Items" })).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
});

describe("Item sheet", () => {
  it("opens from a deep link and marks the differing fields", async () => {
    routes();
    renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    const comparison = await within(sheet).findByRole("table", { name: "Comparison" });
    const row = (name: string) => within(comparison).getByRole("rowheader", { name: new RegExp(`^${name}`) }).closest("tr")!;
    expect(row("Amount")).toHaveTextContent("Differs");
    expect(row("Amount")).toHaveTextContent("Rp150.000");
    expect(row("Amount")).toHaveTextContent("Rp300.000");
    expect(row("Status")).not.toHaveTextContent("Differs");
    expect(row("Fee")).not.toHaveTextContent("Differs");
    expect(within(sheet).getByText(/The gateway has 2 transactions for this reference/)).toBeInTheDocument();
    expect(within(sheet).getByRole("button", { name: "KeiPay record" })).toHaveAttribute("aria-expanded", "false");
    expect(within(sheet).getByRole("button", { name: "Gateway record" })).toHaveAttribute("aria-expanded", "false");
  });

  it("marks a status difference but not PAID against SETTLED", async () => {
    routes({}, [
      reconciliationItem({
        result: "status_mismatch",
        keipay: keipaySide({ status: "EXPIRED", fee: null }),
        gateway: gatewaySide({ status: "SETTLED" }),
      }),
      reconciliationItem({ id: MATCHED_ID, result: "matched", gateway: gatewaySide({ status: "SETTLED" }) }),
    ]);
    const { user, router } = renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    let sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    let comparison = await within(sheet).findByRole("table", { name: "Comparison" });
    const statusRow = () => within(comparison).getByRole("rowheader", { name: /^Status/ }).closest("tr")!;
    expect(statusRow()).toHaveTextContent("Differs");
    expect(within(comparison).getByRole("rowheader", { name: /^Fee/ }).closest("tr")).toHaveTextContent("Unknown");

    await user.keyboard("{Escape}");
    await waitFor(() => expect(router.state.location.search).not.toContain("item="));
    await user.click(screen.getAllByRole("button", { name: /View item/ })[1]!);
    sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    comparison = await within(sheet).findByRole("table", { name: "Comparison" });
    expect(statusRow()).not.toHaveTextContent("Differs");
    expect(within(sheet).getByText("Matched items need no resolution.")).toBeInTheDocument();
    expect(within(sheet).queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("shows the missing side", async () => {
    routes({}, [reconciliationItem({ result: "missing_in_keipay", keipay: null })]);
    renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    expect(await within(sheet).findByText("Not in KeiPay")).toBeInTheDocument();
    expect(within(sheet).queryByText("Differs")).not.toBeInTheDocument();
  });

  it("resolves an item with a trimmed note", async () => {
    const api = routes({
      [`POST ${RESOLVE}`]: () =>
        json(200, {
          item: reconciliationItem({
            resolution: {
              note: "Refunded the double payment.",
              resolved_by: { id: "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", name: "Olivia Owner" },
              resolved_at: "2026-10-08T02:00:00Z",
            },
          }),
        }),
    });
    const { user } = renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    const note = await within(sheet).findByRole("textbox", { name: "Resolution note" });

    await user.click(within(sheet).getByRole("button", { name: "Resolve item" }));
    expect(await within(sheet).findByText("Write a note.")).toBeInTheDocument();

    await user.type(note, "  Refunded the double payment.  ");
    expect(within(sheet).getByText("28/1000")).toBeInTheDocument();
    await user.click(within(sheet).getByRole("button", { name: "Resolve item" }));
    expect(await screen.findByText("Item resolved")).toBeInTheDocument();
    expect(await api.to("POST", RESOLVE)[0]!.json()).toEqual({ note: "Refunded the double payment." });
    expect(await within(sheet).findByText("Refunded the double payment.")).toBeInTheDocument();
    expect(within(sheet).queryByRole("textbox")).not.toBeInTheDocument();
    // The row in the list is updated too.
    const table = screen.getByRole("table", { name: "Items", hidden: true });
    expect(within(table).getAllByText("Resolved", { exact: true }).length).toBeGreaterThan(0);
  });

  it.each([
    ["the close button", "close"],
    ["Escape", "escape"],
  ] as const)("can be closed with %s while the run is still reloading after a resolve", async (_label, how) => {
    let runReads = 0;
    let finishReload: () => void = () => {};
    routes({
      [`GET ${RUN}`]: async () => {
        runReads += 1;
        if (runReads > 1) await new Promise<void>((resolve) => (finishReload = resolve));
        return json(200, { run: reconciliationRun() });
      },
      [`POST ${RESOLVE}`]: () =>
        json(200, {
          item: reconciliationItem({
            resolution: { note: "Checked with finance.", resolved_by: null, resolved_at: "2026-10-08T02:00:00Z" },
          }),
        }),
    });
    const { user, router } = renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    await user.type(await within(sheet).findByRole("textbox", { name: "Resolution note" }), "Checked with finance.");
    await user.click(within(sheet).getByRole("button", { name: "Resolve item" }));
    expect(await within(sheet).findByText("Checked with finance.")).toBeInTheDocument();
    expect(await screen.findByText("Item resolved")).toBeInTheDocument();
    await waitFor(() => expect(runReads).toBeGreaterThan(1));

    if (how === "close") await user.click(within(sheet).getByRole("button", { name: "Close" }));
    else await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Reconciliation item" })).not.toBeInTheDocument());
    expect(router.state.location.search).not.toContain("item=");
    finishReload();
  });

  it.each([
    ["conflict", "Someone already resolved this item. Reload to see their note."],
    ["run_in_progress", "This run is still in progress. Try again when it finishes."],
    ["run_not_succeeded", "This run failed, so its items can't be resolved. Re-run it first."],
  ])("explains a 409 %s", async (code, message) => {
    routes({ [`POST ${RESOLVE}`]: () => json(409, errorBody(code, "conflict")) });
    const { user } = renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    await user.type(await within(sheet).findByRole("textbox", { name: "Resolution note" }), "Checked");
    await user.click(within(sheet).getByRole("button", { name: "Resolve item" }));
    expect(await within(sheet).findByRole("alert")).toHaveTextContent(message);
  });

  it("puts a server note error on the field", async () => {
    routes({
      [`POST ${RESOLVE}`]: () =>
        json(400, errorBody("invalid_request", "invalid", { fields: [{ field: "note", rule: "max" }] })),
    });
    const { user } = renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    await user.type(await within(sheet).findByRole("textbox", { name: "Resolution note" }), "Checked");
    await user.click(within(sheet).getByRole("button", { name: "Resolve item" }));
    expect(await within(sheet).findByText("Use at most 1000 characters.")).toBeInTheDocument();
  });

  it("reads a deep-linked item once, even when the list filter leaves it out", async () => {
    const resolved = reconciliationItem({
      resolution: { note: "Refunded.", resolved_by: null, resolved_at: "2026-10-08T02:00:00Z" },
    });
    // The list is filtered to unresolved items and has more pages; neither matters.
    const api = routes({ [`GET ${ITEMS}`]: page([], "next"), [`GET ${ITEM}`]: { item: resolved } });
    renderApp(`/reconciliation/runs/${REC_ID}?resolved=unresolved&item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    expect(await within(sheet).findByRole("table", { name: "Comparison" })).toBeInTheDocument();
    expect(within(sheet).getByText("Refunded.")).toBeInTheDocument();
    expect(api.to("GET", ITEM)).toHaveLength(1);
    expect(api.to("GET", ITEMS).every((request) => !new URL(request.url).searchParams.has("cursor"))).toBe(true);
  });

  it("says when the item isn't there", async () => {
    routes({ [`GET ${ITEM}`]: () => json(404, errorBody("not_found", "not found")) });
    renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    expect(await within(sheet).findByText("Item not found")).toBeInTheDocument();
  });

  it("treats a malformed item id as not found", async () => {
    routes({
      "GET /admin/reconciliation-items/nope": () =>
        json(400, errorBody("invalid_request", "invalid", { fields: [{ field: "id", rule: "public_id" }] })),
    });
    renderApp(`/reconciliation/runs/${REC_ID}?item=nope`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    expect(await within(sheet).findByText("Item not found")).toBeInTheDocument();
  });

  it("does not show another run's item", async () => {
    routes({ [`GET ${ITEM}`]: { item: reconciliationItem({ run_id: "rec_01JBKQ7Z8M4X2Y6V3N9T5R1W9Z" }) } }, []);
    renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    expect(await within(sheet).findByText("Item not found")).toBeInTheDocument();
    expect(within(sheet).queryByRole("table", { name: "Comparison" })).not.toBeInTheDocument();
  });

  it("offers a retry when the item read fails", async () => {
    let calls = 0;
    const read = () => {
      calls += 1;
      return calls === 1 ? json(500, errorBody("internal_error", "boom")) : json(200, { item: reconciliationItem() });
    };
    routes({ [`GET ${ITEM}`]: read }, []);
    const { user } = renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    await user.click(await within(sheet).findByRole("button", { name: /try again/i }));
    expect(await within(sheet).findByRole("table", { name: "Comparison" })).toBeInTheDocument();
  });
});
