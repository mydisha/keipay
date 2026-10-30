import { describe, expect, it } from "vitest";
import { dashboardTotals } from "@/test/fixtures";
import { deltaOf, ratePoints, sortCurrencies, sumTotals } from "./dashboard-math";

describe("deltaOf", () => {
  it("gives the signed percentage change, toned by direction", () => {
    expect(deltaOf(150, 100)).toEqual({ text: "+50%", tone: "positive" });
    expect(deltaOf(75, 100)).toEqual({ text: "-25%", tone: "negative" });
  });

  it("rounds to one decimal in id-ID", () => {
    expect(deltaOf(1, 3)?.text).toBe("-66,7%");
  });

  it("shows nothing when there is nothing real to compare", () => {
    expect(deltaOf(12, 0)).toBeUndefined();
    expect(deltaOf(0, 0)).toBeUndefined();
    expect(deltaOf(100, 100)).toBeUndefined();
  });
});

describe("ratePoints", () => {
  it("gives the change in percentage points", () => {
    expect(ratePoints(0.75, 0.5)).toEqual({ text: "+25 pp", tone: "positive" });
    expect(ratePoints(0.5, 0.625)).toEqual({ text: "-12,5 pp", tone: "negative" });
  });

  it("gives nothing when the rate did not move or either period has no finished payments", () => {
    expect(ratePoints(0.5, 0.5)).toBeUndefined();
    expect(ratePoints(null, 0.5)).toBeUndefined();
    expect(ratePoints(0.5, null)).toBeUndefined();
  });
});

describe("sortCurrencies", () => {
  it("puts IDR first, then the rest alphabetically, once each", () => {
    expect(sortCurrencies(["USD", "SGD", "IDR", "USD"])).toEqual(["IDR", "SGD", "USD"]);
    expect(sortCurrencies([])).toEqual([]);
    expect(sortCurrencies(["EUR"])).toEqual(["EUR"]);
  });
});

describe("sumTotals", () => {
  it("adds the counts of every currency and recomputes the rate", () => {
    const totals = sumTotals(
      [
        { currency: "IDR", current: dashboardTotals({ created: 4, successful: 3, expired: 1 }), previous: dashboardTotals({ created: 1 }) },
        { currency: "USD", current: dashboardTotals({ created: 2, successful: 1, failed: 1 }), previous: dashboardTotals() },
      ],
      "current",
    );
    expect(totals.created).toBe(6);
    expect(totals.successful).toBe(4);
    expect(totals.success_rate).toBeCloseTo(4 / 6);
    expect(totals.gross).toBe(0);
  });

  it("has no rate when nothing finished, and zero counts without currencies", () => {
    expect(sumTotals([{ currency: "IDR", current: dashboardTotals({ created: 2, pending: 2 }), previous: dashboardTotals() }], "current").success_rate).toBeNull();
    expect(sumTotals([], "previous")).toEqual({ created: 0, pending: 0, paid: 0, settled: 0, expired: 0, failed: 0, successful: 0, gross: 0, success_rate: null });
  });
});
