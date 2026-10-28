import { describe, expect, it } from "vitest";
import { errorMessage } from "./error-message";
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
});
