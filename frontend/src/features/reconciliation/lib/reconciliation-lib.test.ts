import { describe, expect, it } from "vitest";
import { ApiError } from "@/shared/api/errors";
import { REC_ID, reconciliationRun } from "@/test/fixtures";
import { reportsExportUrl } from "../api/reports";
import { itemsExportUrl, runRefetchInterval, runsRefetchInterval, RUN_POLL_MS, RUNS_POLL_MS } from "../api/runs";
import { runFormSchema, toCreateRunRequest } from "../schemas/run-form";
import { noteLength, resolveFormSchema } from "../schemas/resolve-form";
import { formatPeriod, formatRunRange } from "./format-range";
import { serverRangeError, summaryRangeError } from "./summary-range";

describe("summaryRangeError", () => {
  it("accepts a valid range", () => {
    expect(summaryRangeError({ from: "2026-10-01", to: "2026-10-31" }, "total")).toBeNull();
    expect(summaryRangeError({ from: "2026-10-01", to: "2026-10-01" }, "day")).toBeNull();
  });

  it("asks for both dates", () => {
    expect(summaryRangeError({ from: "", to: "2026-10-31" }, "total")).toBe("Choose a start date.");
    expect(summaryRangeError({ from: "2026-10-01", to: "" }, "total")).toBe("Choose an end date.");
  });

  it("refuses an end before the start", () => {
    expect(summaryRangeError({ from: "2026-10-02", to: "2026-10-01" }, "total")).toBe(
      "The end date can't be before the start date.",
    );
  });

  it("allows 366 days for total and month, 92 for day", () => {
    expect(summaryRangeError({ from: "2026-01-01", to: "2027-01-01" }, "total")).toBeNull();
    expect(summaryRangeError({ from: "2026-01-01", to: "2027-01-02" }, "month")).toBe(
      "Choose a range of at most 366 days.",
    );
    expect(summaryRangeError({ from: "2026-10-01", to: "2026-12-31" }, "day")).toBeNull();
    expect(summaryRangeError({ from: "2026-10-01", to: "2027-01-01" }, "day")).toMatch(/at most 92 days/);
  });

  it("turns a server 400 on from/to into the inline message", () => {
    const tooLong = new ApiError({
      status: 400,
      code: "invalid_request",
      message: "invalid request",
      details: { fields: [{ field: "to", rule: "max_range" }] },
    });
    expect(serverRangeError(tooLong, "day")).toMatch(/at most 92 days/);
    expect(serverRangeError(tooLong, "total")).toBe("Choose a range of at most 366 days.");
    const other = new ApiError({
      status: 400,
      code: "invalid_request",
      message: "invalid request",
      details: { fields: [{ field: "app_id", rule: "public_id" }] },
    });
    expect(serverRangeError(other, "total")).toBeNull();
  });
});

describe("export links", () => {
  it("carries the summary filters", () => {
    const url = reportsExportUrl({
      from: "2026-10-01T00:00:00+07:00",
      to: "2026-11-01T00:00:00+07:00",
      environment: "test",
      appId: "app_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
      connectionId: null,
      currency: "IDR",
    });
    const parsed = new URL(url, "http://localhost");
    expect(parsed.pathname).toBe("/admin/payment-reports/export");
    expect(Object.fromEntries(parsed.searchParams)).toEqual({
      from: "2026-10-01T00:00:00+07:00",
      to: "2026-11-01T00:00:00+07:00",
      environment: "test",
      app_id: "app_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
      currency: "IDR",
    });
  });

  it("carries the item filters", () => {
    expect(itemsExportUrl(REC_ID, { result: null, resolved: null })).toBe(`/admin/reconciliation-runs/${REC_ID}/items/export`);
    const parsed = new URL(itemsExportUrl(REC_ID, { result: "fee_mismatch", resolved: false }), "http://localhost");
    expect(Object.fromEntries(parsed.searchParams)).toEqual({ result: "fee_mismatch", resolved: "false" });
  });
});

describe("run form", () => {
  const schema = runFormSchema("2026-10-15");
  const valid = { connection_id: "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0G", from: "2026-10-01", to: "2026-10-14" };

  function messages(values: typeof valid) {
    const result = schema.safeParse(values);
    return result.success ? {} : Object.fromEntries(result.error.issues.map((i) => [i.path.join("."), i.message]));
  }

  it("accepts up to 31 days ending today", () => {
    expect(messages(valid)).toEqual({});
    expect(messages({ ...valid, from: "2026-09-15", to: "2026-10-15" })).toEqual({});
  });

  it("refuses 32 days, a reversed range, a start after today and no connection", () => {
    expect(messages({ ...valid, from: "2026-09-14", to: "2026-10-15" })).toEqual({ to: "Choose a range of at most 31 days." });
    expect(messages({ ...valid, from: "2026-10-10", to: "2026-10-09" })).toEqual({
      to: "The end date can't be before the start date.",
    });
    expect(messages({ ...valid, from: "2026-10-16", to: "2026-10-16" })).toEqual({ from: "The start date can't be after today." });
    expect(messages({ ...valid, connection_id: "" })).toEqual({ connection_id: "Choose a connection." });
  });

  it("sends WIB midnights with an exclusive end", () => {
    expect(toCreateRunRequest(valid)).toEqual({
      connection_id: valid.connection_id,
      from: "2026-10-01T00:00:00+07:00",
      to: "2026-10-15T00:00:00+07:00",
    });
  });
});

describe("resolve form", () => {
  it("trims and counts code points", () => {
    expect(noteLength("  ok  ")).toBe(2);
    expect(noteLength("😀".repeat(1000))).toBe(1000);
    expect(resolveFormSchema.safeParse({ note: "😀".repeat(1000) }).success).toBe(true);
    expect(resolveFormSchema.safeParse({ note: "a".repeat(1001) }).success).toBe(false);
    expect(resolveFormSchema.safeParse({ note: "   " }).success).toBe(false);
  });
});

describe("polling", () => {
  const pages = (status: "queued" | "running" | "succeeded" | "failed") => ({
    pages: [{ data: [reconciliationRun({ status })], next_cursor: null }],
    pageParams: [null],
  });

  it("polls the list only while a run is queued or running", () => {
    expect(runsRefetchInterval(pages("queued"))).toBe(RUNS_POLL_MS);
    expect(runsRefetchInterval(pages("running"))).toBe(RUNS_POLL_MS);
    expect(runsRefetchInterval(pages("succeeded"))).toBe(false);
    expect(runsRefetchInterval(pages("failed"))).toBe(false);
    expect(runsRefetchInterval(undefined)).toBe(false);
  });

  it("polls an open run every 2 seconds while it works", () => {
    expect(runRefetchInterval(reconciliationRun({ status: "running" }))).toBe(RUN_POLL_MS);
    expect(runRefetchInterval(reconciliationRun({ status: "succeeded" }))).toBe(false);
  });
});

describe("formats", () => {
  it("shows a run range as inclusive WIB days", () => {
    expect(formatRunRange("2026-09-30T17:00:00Z", "2026-10-07T17:00:00Z")).toBe("1 Okt 2026 – 7 Okt 2026");
    expect(formatRunRange("2026-10-01T00:00:00+07:00", "2026-10-02T00:00:00+07:00")).toBe("1 Okt 2026");
  });

  it("names summary periods", () => {
    const row = { period_start: "2026-09-30T17:00:00Z" };
    expect(formatPeriod(row, "total")).toBe("Total");
    expect(formatPeriod(row, "day")).toBe("1 Okt 2026");
    expect(formatPeriod(row, "month")).toBe("Oktober 2026");
  });
});
