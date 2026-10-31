import { describe, expect, it } from "vitest";
import { deltaOf, ratePoints, sortCurrencies } from "./dashboard-math";

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
