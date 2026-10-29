// Calendar dates in WIB (Asia/Jakarta, UTC+7, no daylight saving). The
// console picks whole WIB days and sends half-open ranges of instants:
// `from` is the first day's midnight, `to` the midnight after the last day.

const DAY_MS = 24 * 3600 * 1000;
const WIB_OFFSET_MS = 7 * 3600 * 1000;
export const WIB_OFFSET = "+07:00";

const DATE_PATTERN = /^(\d{4})-(\d{2})-(\d{2})$/;

/** A `YYYY-MM-DD` date as the UTC midnight of that calendar day (only for date arithmetic). */
function dayNumber(date: string): number {
  const match = DATE_PATTERN.exec(date);
  if (!match) return Number.NaN;
  const [, y, m, d] = match;
  return Date.UTC(Number(y), Number(m) - 1, Number(d));
}

function dateFromDayNumber(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10);
}

/** True for a real calendar date written as `YYYY-MM-DD` (as `<input type="date">` gives it). */
export function isDateString(value: string | null | undefined): value is string {
  if (!value) return false;
  const ms = dayNumber(value);
  return !Number.isNaN(ms) && dateFromDayNumber(ms) === value;
}

/** The WIB calendar date of an instant. */
export function wibDate(instant: Date): string {
  return dateFromDayNumber(instant.getTime() + WIB_OFFSET_MS);
}

export function addDays(date: string, days: number): string {
  return dateFromDayNumber(dayNumber(date) + days * DAY_MS);
}

/** `2026-10-01` → `2026-10-01T00:00:00+07:00`. */
export function wibMidnight(date: string): string {
  return `${date}T00:00:00${WIB_OFFSET}`;
}

/** WIB days `from`..`to` (both inclusive) as the half-open range of instants the API takes. */
export function wibRange(from: string, to: string): { from: string; to: string } {
  return { from: wibMidnight(from), to: wibMidnight(addDays(to, 1)) };
}

/** Number of days from `from` to `to`, both inclusive (0 or less when `to` is before `from`). */
export function daysInRange(from: string, to: string): number {
  return Math.round((dayNumber(to) - dayNumber(from)) / DAY_MS) + 1;
}

/** The last WIB day an exclusive end instant covers: `2026-11-01T00:00:00+07:00` → `2026-10-31`. */
export function inclusiveEndDate(exclusiveEnd: string): string {
  return wibDate(new Date(Date.parse(exclusiveEnd) - 1));
}

/** A `YYYY-MM-DD` WIB date as the instant of its midnight, for display formatting. */
export function wibDateInstant(date: string): Date {
  return new Date(wibMidnight(date));
}

export type DatePreset = "today" | "yesterday" | "last_7_days" | "this_month" | "last_month";

export const DATE_PRESETS: readonly DatePreset[] = ["today", "yesterday", "last_7_days", "this_month", "last_month"];

export interface DateRange {
  from: string;
  to: string;
}

/** The WIB days a preset covers at `now`. */
export function presetRange(preset: DatePreset, now: Date): DateRange {
  const today = wibDate(now);
  const firstOfMonth = `${today.slice(0, 8)}01`;
  switch (preset) {
    case "today":
      return { from: today, to: today };
    case "yesterday": {
      const yesterday = addDays(today, -1);
      return { from: yesterday, to: yesterday };
    }
    case "last_7_days":
      return { from: addDays(today, -6), to: today };
    case "this_month":
      return { from: firstOfMonth, to: today };
    case "last_month": {
      const lastDay = addDays(firstOfMonth, -1);
      return { from: `${lastDay.slice(0, 8)}01`, to: lastDay };
    }
  }
}

/** The preset whose range is exactly `range` at `now`, if any. */
export function matchPreset(range: DateRange, now: Date): DatePreset | null {
  return DATE_PRESETS.find((preset) => {
    const candidate = presetRange(preset, now);
    return candidate.from === range.from && candidate.to === range.to;
  }) ?? null;
}
