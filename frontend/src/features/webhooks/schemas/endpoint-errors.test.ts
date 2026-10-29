import { act, renderHook } from "@testing-library/react";
import { useForm } from "react-hook-form";
import { describe, expect, it } from "vitest";
import { ApiError, type FieldError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { applyEndpointServerErrors } from "./endpoint-errors";
import { defaultEndpointValues, type EndpointFormValues } from "./endpoint-form";

function run(fields: FieldError[], headerRows: number, code: "invalid_request" | "validation_failed" = "invalid_request") {
  const { result } = renderHook(() => useForm<EndpointFormValues>({ defaultValues: defaultEndpointValues(null) }));
  let placed = false;
  act(() => {
    placed = applyEndpointServerErrors(
      new ApiError({ status: 400, code, message: code, details: { fields } }),
      result.current.setError,
      headerRows,
    );
  });
  const message = (name: Parameters<typeof result.current.getFieldState>[0]) =>
    result.current.getFieldState(name).error?.message;
  return { placed, message };
}

describe("applyEndpointServerErrors", () => {
  it("puts element errors of events on the picker and retry errors on their inputs", () => {
    const { placed, message } = run(
      [
        { field: "url", rule: "https" },
        { field: "events.3", rule: "oneof" },
        { field: "retry_policy.backoff", rule: "oneof" },
        { field: "retry_policy.max_attempts", rule: "type" },
      ],
      0,
    );
    expect(placed).toBe(true);
    expect(message("url")).toBe(strings.endpoints.urlHttps);
    expect(message("events")).toBe(strings.endpoints.eventUnknown);
    expect(message("retry_policy.backoff")).toBe(strings.endpoints.backoffInvalid);
    expect(message("retry_policy.max_attempts")).toBe(strings.endpoints.maxAttemptsRange);
  });

  it("places header errors only on rows the form shows", () => {
    const shown = run([{ field: "headers.0.value", rule: "header_value" }], 1);
    expect(shown.placed).toBe(true);
    expect(shown.message("headers.0.value")).toBe(strings.endpoints.headerValueInvalid);

    const hidden = run([{ field: "headers.2.name", rule: "duplicate" }], 1);
    expect(hidden.placed).toBe(false);
  });

  it("leaves unknown fields and other codes to the alert", () => {
    expect(run([{ field: "status", rule: "immutable" }], 0).placed).toBe(false);
    expect(run([], 0, "validation_failed").placed).toBe(false);
  });

  it("falls back to a generic message for an unknown rule", () => {
    const { placed, message } = run([{ field: "description", rule: "brand_new" }], 0);
    expect(placed).toBe(true);
    expect(message("description")).toBe(strings.common.invalidField("brand_new"));
  });
});
