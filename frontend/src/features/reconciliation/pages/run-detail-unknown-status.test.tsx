import { screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { gatewaySide, keipaySide, page, RCI_ID, REC_ID, reconciliationItem, reconciliationRun } from "@/test/fixtures";
import { mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";
import type { components } from "@/shared/api/schema";
import { comparisonRows } from "../components/item-sheet";

type PaymentStatus = components["schemas"]["PaymentStatus"];

// The contract allows `gateway.status: null` when the gateway reported a
// status KeiPay does not know (`provider_status` keeps the raw value). The
// console must show the raw value and count it as "not paid" when comparing.

const RUN = `/admin/reconciliation-runs/${REC_ID}`;
const ITEMS = `${RUN}/items`;
const ITEM = `/admin/reconciliation-items/${RCI_ID}`;

beforeEach(() => setViewport("desktop"));

function unknownStatusItem() {
  return reconciliationItem({
    result: "status_mismatch",
    keipay: keipaySide({ status: "PAID" }),
    gateway: gatewaySide({ status: null, provider_status: "WEIRD_STATE", amount: 150000, transaction_count: 1 }),
  });
}

describe("Unknown gateway status (null)", () => {
  it("shows the gateway's raw status in the items table", async () => {
    mockApi({ [`GET ${RUN}`]: { run: reconciliationRun() }, [`GET ${ITEMS}`]: page([unknownStatusItem()]) });
    renderApp(`/reconciliation/runs/${REC_ID}`);
    const table = await screen.findByRole("table", { name: "Items" });
    expect(await within(table).findByText("WEIRD_STATE")).toBeInTheDocument();
  });

  it("marks the status row as differing against a paid KeiPay payment", async () => {
    mockApi({
      [`GET ${RUN}`]: { run: reconciliationRun() },
      [`GET ${ITEMS}`]: page([unknownStatusItem()]),
      [`GET ${ITEM}`]: { item: unknownStatusItem() },
    });
    renderApp(`/reconciliation/runs/${REC_ID}?item=${RCI_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Reconciliation item" });
    const comparison = await within(sheet).findByRole("table", { name: "Comparison" });
    const statusRow = within(comparison).getByRole("rowheader", { name: /^Status/ }).closest("tr")!;
    expect(statusRow).toHaveTextContent("WEIRD_STATE");
    expect(statusRow).toHaveTextContent("Differs");
    const amountRow = within(comparison).getByRole("rowheader", { name: /^Amount/ }).closest("tr")!;
    expect(amountRow).not.toHaveTextContent("Differs");
  });

  it("counts an unknown status as not paid when comparing", () => {
    const status = (keipay: PaymentStatus) =>
      comparisonRows(keipaySide({ status: keipay }), gatewaySide({ status: null })).find((row) => row.id === "status")!;
    expect(status("PAID").differs).toBe(true);
    expect(status("SETTLED").differs).toBe(true);
    expect(status("EXPIRED").differs).toBe(false);
  });
});
