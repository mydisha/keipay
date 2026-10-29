import { describe, expect, it } from "vitest";
import {
  addDays,
  daysInRange,
  inclusiveEndDate,
  isDateString,
  matchPreset,
  presetRange,
  wibDate,
  wibMidnight,
  wibRange,
} from "./wib-dates";

// WIB dates become +07:00 midnights with an exclusive end.

describe("wibRange", () => {
  it("sends the first day's midnight and the midnight after the last day", () => {
    expect(wibRange("2026-10-01", "2026-10-01")).toEqual({
      from: "2026-10-01T00:00:00+07:00",
      to: "2026-10-02T00:00:00+07:00",
    });
  });

  it("crosses month ends", () => {
    expect(wibRange("2026-10-01", "2026-10-31").to).toBe("2026-11-01T00:00:00+07:00");
    expect(wibRange("2026-02-01", "2026-02-28").to).toBe("2026-03-01T00:00:00+07:00");
    expect(wibRange("2028-02-01", "2028-02-28").to).toBe("2028-02-29T00:00:00+07:00");
  });

  it("crosses the year end", () => {
    expect(wibRange("2026-12-01", "2026-12-31")).toEqual({
      from: "2026-12-01T00:00:00+07:00",
      to: "2027-01-01T00:00:00+07:00",
    });
  });

  it("is the instant of WIB midnight, 17:00 UTC the day before", () => {
    expect(new Date(wibMidnight("2026-10-01")).toISOString()).toBe("2026-09-30T17:00:00.000Z");
  });
});

describe("wibDate", () => {
  it("uses the WIB calendar day, not UTC", () => {
    expect(wibDate(new Date("2026-09-30T16:59:59Z"))).toBe("2026-09-30");
    expect(wibDate(new Date("2026-09-30T17:00:00Z"))).toBe("2026-10-01");
  });
});

describe("inclusiveEndDate", () => {
  it("shows the last day an exclusive end covers", () => {
    expect(inclusiveEndDate("2026-11-01T00:00:00+07:00")).toBe("2026-10-31");
    expect(inclusiveEndDate("2026-10-07T17:00:00Z")).toBe("2026-10-07");
  });
});

describe("date arithmetic", () => {
  it("counts both ends", () => {
    expect(daysInRange("2026-10-01", "2026-10-01")).toBe(1);
    expect(daysInRange("2026-10-01", "2026-10-31")).toBe(31);
    expect(daysInRange("2026-01-01", "2026-12-31")).toBe(365);
    expect(daysInRange("2026-10-02", "2026-10-01")).toBe(0);
  });

  it("adds days across months and years", () => {
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01");
    expect(addDays("2026-03-01", -1)).toBe("2026-02-28");
  });

  it.each([
    ["2026-10-01", true],
    ["2026-02-30", false],
    ["2026-13-01", false],
    ["2026-1-01", false],
    ["", false],
    [null, false],
  ])("isDateString(%s) is %s", (value, expected) => {
    expect(isDateString(value)).toBe(expected);
  });
});

describe("presets", () => {
  // 23:30 WIB on 31 October is still 16:30 UTC on 31 October; 00:30 WIB on
  // 1 November is still 31 October in UTC. Both must use the WIB day.
  const lastEvening = new Date("2026-10-31T16:30:00Z");
  const firstNight = new Date("2026-10-31T17:30:00Z");

  it("uses the WIB day at 23:30 WIB on the last day of a month", () => {
    expect(presetRange("today", lastEvening)).toEqual({ from: "2026-10-31", to: "2026-10-31" });
    expect(presetRange("yesterday", lastEvening)).toEqual({ from: "2026-10-30", to: "2026-10-30" });
    expect(presetRange("last_7_days", lastEvening)).toEqual({ from: "2026-10-25", to: "2026-10-31" });
    expect(presetRange("this_month", lastEvening)).toEqual({ from: "2026-10-01", to: "2026-10-31" });
    expect(presetRange("last_month", lastEvening)).toEqual({ from: "2026-09-01", to: "2026-09-30" });
  });

  it("moves to the new month at WIB midnight even while UTC is still in the old one", () => {
    expect(presetRange("today", firstNight)).toEqual({ from: "2026-11-01", to: "2026-11-01" });
    expect(presetRange("yesterday", firstNight)).toEqual({ from: "2026-10-31", to: "2026-10-31" });
    expect(presetRange("this_month", firstNight)).toEqual({ from: "2026-11-01", to: "2026-11-01" });
    expect(presetRange("last_month", firstNight)).toEqual({ from: "2026-10-01", to: "2026-10-31" });
  });

  it("handles January (last month is December of the year before)", () => {
    expect(presetRange("last_month", new Date("2027-01-10T05:00:00Z"))).toEqual({ from: "2026-12-01", to: "2026-12-31" });
  });

  it("recognises a range that is a preset", () => {
    expect(matchPreset({ from: "2026-10-01", to: "2026-10-31" }, lastEvening)).toBe("this_month");
    expect(matchPreset({ from: "2026-10-25", to: "2026-10-31" }, lastEvening)).toBe("last_7_days");
    expect(matchPreset({ from: "2026-10-02", to: "2026-10-31" }, lastEvening)).toBeNull();
  });
});
