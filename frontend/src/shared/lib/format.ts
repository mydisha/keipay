// Display formats for the console: id-ID numbers, Asia/Jakarta (WIB) times.

export const DISPLAY_LOCALE = "id-ID";
export const DISPLAY_TIME_ZONE = "Asia/Jakarta";
const RELATIVE_LOCALE = "en";

// KeiPay stores IDR in whole rupiah (exponent 0), unlike ISO 4217's 2.
const EXPONENT_OVERRIDES: Record<string, number> = { IDR: 0 };

const numberFormat = new Intl.NumberFormat(DISPLAY_LOCALE, { maximumFractionDigits: 0 });

export function currencyExponent(currency: string): number {
  const code = currency.toUpperCase();
  const override = EXPONENT_OVERRIDES[code];
  if (override !== undefined) return override;
  try {
    return (
      new Intl.NumberFormat("en", { style: "currency", currency: code }).resolvedOptions()
        .maximumFractionDigits ?? 2
    );
  } catch {
    return 2;
  }
}

/** Formats an amount in minor units, e.g. 150000 IDR → "Rp150.000". */
export function formatMoney(amount: number, currency = "IDR"): string {
  const code = currency.toUpperCase();
  if (code === "IDR") {
    const sign = amount < 0 ? "-" : "";
    return `${sign}Rp${numberFormat.format(Math.abs(amount))}`;
  }
  const exponent = currencyExponent(code);
  const major = amount / 10 ** exponent;
  try {
    return new Intl.NumberFormat(DISPLAY_LOCALE, {
      style: "currency",
      currency: code,
      minimumFractionDigits: exponent,
      maximumFractionDigits: exponent,
    }).format(major);
  } catch {
    return `${code} ${major.toFixed(exponent)}`;
  }
}

export function formatNumber(value: number): string {
  return numberFormat.format(value);
}

/** A ratio as a percentage, e.g. 0.925 → "92,5%"; `signed` adds + or − to non-zero values. */
export function formatPercent(ratio: number, { signed = false, fractionDigits = 1 }: { signed?: boolean; fractionDigits?: number } = {}): string {
  return new Intl.NumberFormat(DISPLAY_LOCALE, {
    style: "percent",
    maximumFractionDigits: fractionDigits,
    signDisplay: signed ? "exceptZero" : "auto",
  }).format(ratio);
}

export function parseDate(value: string | Date | null | undefined): Date | null {
  if (value === null || value === undefined || value === "") return null;
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

const absoluteFormat = new Intl.DateTimeFormat(DISPLAY_LOCALE, {
  day: "numeric",
  month: "short",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
  timeZone: DISPLAY_TIME_ZONE,
});

/** "2 Jan 2026, 01.30.00 WIB" in Asia/Jakarta. */
export function formatDateTimeWIB(date: Date): string {
  return `${absoluteFormat.format(date)} WIB`;
}

const dateFormat = new Intl.DateTimeFormat(DISPLAY_LOCALE, {
  day: "numeric",
  month: "short",
  year: "numeric",
  timeZone: DISPLAY_TIME_ZONE,
});

const monthFormat = new Intl.DateTimeFormat(DISPLAY_LOCALE, { month: "long", year: "numeric", timeZone: DISPLAY_TIME_ZONE });

/** "1 Okt 2026": the WIB calendar day of an instant. */
export function formatDateWIB(date: Date): string {
  return dateFormat.format(date);
}

const dayFormat = new Intl.DateTimeFormat(DISPLAY_LOCALE, { day: "numeric", month: "short", timeZone: DISPLAY_TIME_ZONE });

/** "1 Okt": the WIB calendar day of an instant without the year, for chart axes. */
export function formatDayWIB(date: Date): string {
  return dayFormat.format(date);
}

/** "Oktober 2026": the WIB month of an instant. */
export function formatMonthWIB(date: Date): string {
  return monthFormat.format(date);
}

const relativeFormat = new Intl.RelativeTimeFormat(RELATIVE_LOCALE, { numeric: "auto" });

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 24 * 3600],
  ["month", 30 * 24 * 3600],
  ["week", 7 * 24 * 3600],
  ["day", 24 * 3600],
  ["hour", 3600],
  ["minute", 60],
  ["second", 1],
];

/** "5 minutes ago", "in 2 hours", "now". */
export function formatRelative(date: Date, now: Date): string {
  const seconds = Math.round((date.getTime() - now.getTime()) / 1000);
  const abs = Math.abs(seconds);
  if (abs < 45) return relativeFormat.format(0, "second");
  for (const [unit, size] of UNITS) {
    if (abs >= size) return relativeFormat.format(Math.round(seconds / size), unit);
  }
  return relativeFormat.format(0, "second");
}
