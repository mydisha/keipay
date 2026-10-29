import { describe, expect, it } from "vitest";
import { strings } from "@/shared/lib/strings";
import { webhookEndpoint } from "@/test/fixtures";
import {
  defaultEndpointValues,
  endpointFormSchema,
  toCreateRequest,
  toUpdateRequest,
  type EndpointFormValues,
} from "./endpoint-form";

// The client rules mirror CreateWebhookEndpointRequest.

const e = strings.endpoints;

function values(overrides: Partial<EndpointFormValues> = {}): EndpointFormValues {
  return { ...defaultEndpointValues(null), url: "https://shop.example.com/hook", events: ["payment.paid"], ...overrides };
}

function issues(input: EndpointFormValues, environment: "test" | "live" = "test") {
  const result = endpointFormSchema(environment).safeParse(input);
  if (result.success) return {};
  return Object.fromEntries(result.error.issues.map((issue) => [issue.path.join("."), issue.message]));
}

describe("endpointFormSchema", () => {
  it("accepts the defaults with a URL and one event", () => {
    expect(issues(values())).toEqual({});
  });

  it.each([
    ["blank", "", e.urlRequired],
    ["spaces only", "   ", e.urlRequired],
    ["not http(s)", "ftp://shop.example.com", e.urlInvalid],
    ["relative", "/hook", e.urlInvalid],
    ["with a space", "https://shop example.com", e.urlInvalid],
  ])("rejects a URL: %s", (_, url, message) => {
    expect(issues(values({ url }))).toEqual({ url: message });
  });

  it("needs https for a live app but allows http for a test app", () => {
    expect(issues(values({ url: "http://127.0.0.1:9000/hook" }), "live")).toEqual({ url: e.urlHttps });
    expect(issues(values({ url: "HTTP://shop.example.com" }), "live")).toEqual({ url: e.urlHttps });
    expect(issues(values({ url: "http://127.0.0.1:9000/hook" }), "test")).toEqual({});
    expect(issues(values({ url: "https://shop.example.com" }), "live")).toEqual({});
  });

  it("limits the description to 500 code points without control characters", () => {
    expect(issues(values({ description: "é".repeat(500) }))).toEqual({});
    expect(issues(values({ description: "x".repeat(501) }))).toEqual({ description: e.descriptionTooLong });
    expect(issues(values({ description: "line one\nline two\tok" }))).toEqual({});
    expect(issues(values({ description: "bell\u0007" }))).toEqual({ description: e.descriptionInvalid });
  });

  it("needs 1-20 events", () => {
    expect(issues(values({ events: [] }))).toEqual({ events: e.eventsRequired });
    expect(issues(values({ events: Array.from({ length: 21 }, (_, i) => `t${i}`) }))).toEqual({ events: e.eventsTooMany });
  });

  it("checks header names: characters, reserved names in any case and case-insensitive duplicates", () => {
    const headers = [
      { name: "Authorization", value: "Bearer token" },
      { name: "bad name", value: "x" },
      { name: "X-KeiPay-Foo", value: "x" },
      { name: "content-TYPE", value: "x" },
      { name: "authorization", value: "again" },
      { name: "x".repeat(65), value: "x" },
      { name: "X-Kei-Event", value: "x" },
    ];
    expect(issues(values({ replace_headers: true, headers }))).toEqual({
      "headers.1.name": e.headerNameInvalid,
      "headers.2.name": e.headerNameReserved,
      "headers.3.name": e.headerNameReserved,
      "headers.4.name": e.headerNameDuplicate,
      "headers.5.name": e.headerNameInvalid,
      "headers.6.name": e.headerNameReserved,
    });
  });

  it("checks header values: required, 1024 code points, no CR/LF", () => {
    const headers = [
      { name: "A", value: "" },
      { name: "B", value: "ü".repeat(1024) },
      { name: "C", value: "x".repeat(1025) },
      { name: "D", value: "a\r\nInjected: yes" },
    ];
    expect(issues(values({ replace_headers: true, headers }))).toEqual({
      "headers.0.value": e.headerValueRequired,
      "headers.2.value": e.headerValueTooLong,
      "headers.3.value": e.headerValueInvalid,
    });
  });

  it("allows at most 10 headers and skips header rules unless replacing", () => {
    const eleven = Array.from({ length: 11 }, (_, i) => ({ name: `X-Custom-${i}`, value: "v" }));
    expect(issues(values({ replace_headers: true, headers: eleven }))).toEqual({ headers: e.headersTooMany });
    expect(issues(values({ replace_headers: false, headers: [{ name: "bad name", value: "" }] }))).toEqual({});
  });

  it.each([
    ["max_attempts 0", { max_attempts: "0" }, "retry_policy.max_attempts", e.maxAttemptsRange],
    ["max_attempts 21", { max_attempts: "21" }, "retry_policy.max_attempts", e.maxAttemptsRange],
    ["max_attempts 2.5", { max_attempts: "2.5" }, "retry_policy.max_attempts", e.maxAttemptsRange],
    ["interval 9", { interval_seconds: "9" }, "retry_policy.interval_seconds", e.intervalRange],
    ["interval 3601", { interval_seconds: "3601" }, "retry_policy.interval_seconds", e.intervalRange],
    ["interval empty", { interval_seconds: "" }, "retry_policy.interval_seconds", e.intervalRange],
  ])("rejects a retry policy with %s", (_, change, path, message) => {
    const retry = { ...values().retry_policy, ...change };
    expect(issues(values({ retry_policy: retry }))).toEqual({ [path]: message });
  });

  it("accepts the retry range edges", () => {
    const low = { max_attempts: "1", backoff: "fixed" as const, interval_seconds: "10" };
    const high = { max_attempts: "20", backoff: "exponential" as const, interval_seconds: "3600" };
    expect(issues(values({ retry_policy: low }))).toEqual({});
    expect(issues(values({ retry_policy: high }))).toEqual({});
  });
});

describe("requests from the form", () => {
  it("creates with numbers for the retry policy and leaves out empty description and headers", () => {
    expect(toCreateRequest(values())).toEqual({
      url: "https://shop.example.com/hook",
      events: ["payment.paid"],
      api_version: "v2",
      legacy_signature: false,
      retry_policy: { max_attempts: 8, backoff: "exponential", interval_seconds: 30 },
    });
    const withHeaders = toCreateRequest(
      values({ description: "Orders", headers: [{ name: " Authorization ", value: " Bearer x " }] }),
    );
    expect(withHeaders.description).toBe("Orders");
    expect(withHeaders.headers).toEqual([{ name: "Authorization", value: " Bearer x " }]);
  });

  it("updates only what changed", () => {
    const endpoint = webhookEndpoint({ headers: [{ name: "Authorization", value_last4: null }] });
    const unchanged = defaultEndpointValues(endpoint);
    expect(toUpdateRequest(endpoint, unchanged)).toEqual({});
    expect(
      toUpdateRequest(endpoint, {
        ...unchanged,
        events: ["payment.settled", "payment.paid"],
        retry_policy: { ...unchanged.retry_policy, interval_seconds: "60" },
      }),
    ).toEqual({
      events: ["payment.settled", "payment.paid"],
      retry_policy: { max_attempts: 8, backoff: "exponential", interval_seconds: 60 },
    });
    expect(toUpdateRequest(endpoint, { ...unchanged, replace_headers: true, headers: [] })).toEqual({ headers: [] });
  });
});
