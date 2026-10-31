import { describe, expect, it } from "vitest";
import { currencyExponent, formatDateTimeWIB, formatDayWIB, formatMoney, formatPercent, formatPoints, formatRelative, parseDate } from "./format";

describe("formatMoney", () => {
  it.each([
    [150000, "IDR", "Rp150.000"],
    [0, "IDR", "Rp0"],
    [-2500, "IDR", "-Rp2.500"],
    [1234567890123, "IDR", "Rp1.234.567.890.123"],
    [150000, "idr", "Rp150.000"],
  ])("%d %s → %s", (amount, currency, expected) => {
    expect(formatMoney(amount, currency)).toBe(expected);
  });

  it("defaults to IDR", () => {
    expect(formatMoney(5000)).toBe("Rp5.000");
  });

  it("formats other currencies by their ISO exponent through Intl", () => {
    const expected = new Intl.NumberFormat("id-ID", { style: "currency", currency: "USD" }).format(10.5);
    expect(formatMoney(1050, "USD")).toBe(expected);
    expect(formatMoney(1050, "USD")).toContain("10,50");
    expect(formatMoney(1050, "JPY")).toContain("1.050");
    expect(formatMoney(1050, "JPY")).not.toContain(",");
  });

  it("treats IDR as exponent 0 and unknown codes as 2", () => {
    expect(currencyExponent("IDR")).toBe(0);
    expect(currencyExponent("USD")).toBe(2);
    expect(currencyExponent("JPY")).toBe(0);
    expect(currencyExponent("NOT-A-CODE")).toBe(2);
  });

  it("does not throw for an invalid currency code", () => {
    expect(formatMoney(1050, "??")).toBe("?? 10.50");
  });
});

describe("formatDateTimeWIB", () => {
  it("shows Asia/Jakarta time across the UTC date boundary", () => {
    const text = formatDateTimeWIB(new Date("2026-01-01T18:30:00Z"));
    expect(text).toContain("2 Jan 2026");
    expect(text).toContain("01.30");
    expect(text.endsWith("WIB")).toBe(true);
  });
});

describe("formatPoints", () => {
  it("signs non-zero points with one decimal in id-ID", () => {
    expect(formatPoints(2.5)).toBe("+2,5");
    expect(formatPoints(-10)).toBe("-10");
    expect(formatPoints(0)).toBe("0");
  });
});

describe("formatPercent", () => {
  it.each([
    [0.925, "92,5%"],
    [1, "100%"],
    [0, "0%"],
    [0.3333, "33,3%"],
  ])("%d → %s", (ratio, expected) => {
    expect(formatPercent(ratio)).toBe(expected);
  });

  it("signs non-zero values on request and leaves zero alone", () => {
    expect(formatPercent(0.125, { signed: true })).toBe("+12,5%");
    expect(formatPercent(-0.5, { signed: true })).toBe("-50%");
    expect(formatPercent(0, { signed: true })).toBe("0%");
  });
});

describe("formatDayWIB", () => {
  it("shows the WIB day and short month across the UTC date boundary", () => {
    expect(formatDayWIB(new Date("2026-09-30T17:00:00Z"))).toBe("1 Okt");
    expect(formatDayWIB(new Date("2026-01-01T18:30:00Z"))).toBe("2 Jan");
  });
});

describe("formatRelative", () => {
  const now = new Date("2026-03-10T12:00:00Z");

  it.each([
    ["2026-03-10T11:55:00Z", "5 minutes ago"],
    ["2026-03-10T11:59:50Z", "now"],
    ["2026-03-10T09:00:00Z", "3 hours ago"],
    ["2026-03-09T12:00:00Z", "yesterday"],
    ["2026-03-10T14:00:00Z", "in 2 hours"],
  ])("%s → %s", (iso, expected) => {
    expect(formatRelative(new Date(iso), now)).toBe(expected);
  });
});

describe("parseDate", () => {
  it.each([null, undefined, "", "not a date"])("returns null for %s", (value) => {
    expect(parseDate(value)).toBeNull();
  });

  it("parses ISO strings", () => {
    expect(parseDate("2026-01-01T00:00:00Z")?.toISOString()).toBe("2026-01-01T00:00:00.000Z");
  });
});
