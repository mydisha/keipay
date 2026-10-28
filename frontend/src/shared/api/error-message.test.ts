import { describe, expect, it } from "vitest";
import { errorMessage, retryAfterMinutes } from "./error-message";
import { ApiError } from "./errors";

const err = (code: ApiError["code"], message = "x") => new ApiError({ status: 400, code, message });

describe("errorMessage", () => {
  it("explains network and unknown errors in plain words", () => {
    expect(errorMessage(err("network_error"))).toMatch(/Can't reach the KeiPay server/);
    expect(errorMessage(err("unknown_error"))).toMatch(/unexpected response/);
  });

  it("never shows internal error text", () => {
    expect(errorMessage(err("internal_error", "internal server error"), "Couldn't save.")).toBe("Couldn't save.");
  });

  it("sentence-cases server messages", () => {
    expect(errorMessage(err("validation_failed", "invalid input: invalid slug"))).toBe("Invalid input: invalid slug.");
  });

  it("falls back for non-API errors", () => {
    expect(errorMessage(new Error("boom"), "Fallback")).toBe("Fallback");
  });

  it("gives friendly copy for the session, CSRF and permission codes", () => {
    expect(errorMessage(err("forbidden", "forbidden"))).toBe("Your role doesn't allow this action.");
    expect(errorMessage(err("invalid_csrf_token", "invalid csrf token"))).toMatch(/Reload the page/);
    expect(errorMessage(err("session_expired", "session expired"))).toBe("Your session has expired. Sign in again.");
  });
});

describe("retryAfterMinutes", () => {
  const limited = (seconds: unknown) =>
    new ApiError({ status: 429, code: "rate_limited", message: "x", details: { retry_after_seconds: seconds } });

  it.each([
    [1, 1],
    [60, 1],
    [61, 2],
    [900, 15],
  ])("rounds %i seconds up to %i minutes", (seconds, minutes) => {
    expect(retryAfterMinutes(limited(seconds))).toBe(minutes);
  });

  it.each([[undefined], [0], [-5], ["60"]])("falls back to 1 minute for %s", (seconds) => {
    expect(retryAfterMinutes(limited(seconds))).toBe(1);
  });

  it("puts the minutes in the message", () => {
    expect(errorMessage(limited(61))).toBe("Too many attempts. Try again in 2 minutes.");
    expect(errorMessage(limited(30))).toBe("Too many attempts. Try again in 1 minute.");
  });
});
