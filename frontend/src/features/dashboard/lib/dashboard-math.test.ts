import { describe, expect, it } from "vitest";
import { dashboardSeries, dashboardTotals } from "@/test/fixtures";
import { deltaOf, ratePoints, sortCurrencies, sparklineFrom, sumTotals } from "./dashboard-math";

describe("deltaOf", () => {
  it("gives the signed percentage change with the comparison period", () => {
    expect(deltaOf(150, 100, 7)).toEqual({ direction: "up", text: "+50% vs previous 7 days", tone: "positive" });
    expect(deltaOf(75, 100, 30)).toEqual({ direction: "down", text: "-25% vs previous 30 days", tone: "negative" });
    expect(deltaOf(100, 100, 7)).toEqual({ direction: "flat", text: "0% vs previous 7 days", tone: "neutral" });
  });

  it("rounds to one decimal in id-ID", () => {
    expect(deltaOf(1, 3, 7).text).toBe("-66,7% vs previous 7 days");
  });

  it("says new when the previous period was empty, and no change when both are", () => {
    expect(deltaOf(12, 0, 7)).toEqual({ direction: "up", text: "New in this range", tone: "positive" });
    expect(deltaOf(0, 0, 7)).toEqual({ direction: "flat", text: "No change", tone: "neutral" });
  });
});

describe("ratePoints", () => {
  it("gives the change in percentage points", () => {
    expect(ratePoints(0.75, 0.5, 7)).toEqual({ direction: "up", text: "+25 pp vs previous 7 days", tone: "positive" });
    expect(ratePoints(0.5, 0.625, 30)).toEqual({ direction: "down", text: "-12,5 pp vs previous 30 days", tone: "negative" });
    expect(ratePoints(0.5, 0.5, 7)).toEqual({ direction: "flat", text: "No change", tone: "neutral" });
  });

  it("gives nothing when either period has no finished payments", () => {
    expect(ratePoints(null, 0.5, 7)).toBeUndefined();
    expect(ratePoints(0.5, null, 7)).toBeUndefined();
  });
});

describe("sortCurrencies", () => {
  it("puts IDR first, then the rest alphabetically, once each", () => {
    expect(sortCurrencies(["USD", "SGD", "IDR", "USD"])).toEqual(["IDR", "SGD", "USD"]);
    expect(sortCurrencies([])).toEqual([]);
    expect(sortCurrencies(["EUR"])).toEqual(["EUR"]);
  });
});

describe("sparklineFrom", () => {
  const series = dashboardSeries(3, ["IDR", "USD"]);
  series[1] = { ...series[1]!, created: 4, successful: 2, gross: [{ currency: "IDR", amount: 300 }, { currency: "USD", amount: 5 }] };

  it("reads a count per day", () => {
    expect(sparklineFrom(series, "created")).toEqual([0, 4, 0]);
    expect(sparklineFrom(series, "successful")).toEqual([0, 2, 0]);
  });

  it("reads the gross of one currency per day and zero where it is missing", () => {
    expect(sparklineFrom(series, { gross: "USD" })).toEqual([0, 5, 0]);
    expect(sparklineFrom(series, { gross: "SGD" })).toEqual([0, 0, 0]);
    expect(sparklineFrom([], "created")).toEqual([]);
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
