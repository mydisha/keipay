import { act, renderHook } from "@testing-library/react";
import { useForm } from "react-hook-form";
import { describe, expect, it } from "vitest";
import { ApiError, type ApiErrorCode, type FieldError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { applyServerErrors } from "./apply-server-errors";

interface Values {
  name: string;
  slug: string;
  webhook_url: string;
}

const CREATE_FIELDS = ["name", "slug", "webhook_url"] as const;
const EDIT_FIELDS = ["name", "webhook_url"] as const;

function apiError(code: ApiErrorCode, fields?: FieldError[]) {
  return new ApiError({ status: code === "conflict" ? 409 : 400, code, message: code, details: { fields } });
}

function run(error: unknown, fieldNames: readonly string[]) {
  const { result } = renderHook(() => useForm<Values>({ defaultValues: { name: "", slug: "", webhook_url: "" } }));
  let placed = false;
  act(() => {
    placed = applyServerErrors(error, result.current.setError, fieldNames);
  });
  const messages = Object.fromEntries(
    CREATE_FIELDS.map((field) => [field, result.current.getFieldState(field).error?.message]),
  );
  return { placed, messages };
}

describe("applyServerErrors", () => {
  it.each([
    {
      name: "places every invalid_request field and reports it handled",
      error: apiError("invalid_request", [
        { field: "name", rule: "required" },
        { field: "webhook_url", rule: "url" },
      ]),
      fieldNames: CREATE_FIELDS,
      placed: true,
      messages: { name: strings.common.invalidField("required"), webhook_url: strings.common.invalidField("url") },
    },
    {
      name: "places known fields but leaves the alert when one is not on the form",
      error: apiError("invalid_request", [
        { field: "name", rule: "max" },
        { field: "api_version", rule: "oneof" },
      ]),
      fieldNames: CREATE_FIELDS,
      placed: false,
      messages: { name: strings.common.invalidField("max") },
    },
    {
      name: "reports unhandled when no field is on the form",
      error: apiError("invalid_request", [{ field: "api_version", rule: "oneof" }]),
      fieldNames: CREATE_FIELDS,
      placed: false,
      messages: {},
    },
    {
      name: "reports unhandled for invalid_request without fields",
      error: apiError("invalid_request", []),
      fieldNames: CREATE_FIELDS,
      placed: false,
      messages: {},
    },
    {
      name: "reports unhandled for invalid_request with no details",
      error: new ApiError({ status: 400, code: "invalid_request", message: "bad" }),
      fieldNames: CREATE_FIELDS,
      placed: false,
      messages: {},
    },
    {
      name: "ignores fields on codes other than invalid_request",
      error: apiError("internal_error", [{ field: "name", rule: "required" }]),
      fieldNames: CREATE_FIELDS,
      placed: false,
      messages: {},
    },
    {
      name: "puts conflict on the slug field when the form has one",
      error: apiError("conflict"),
      fieldNames: CREATE_FIELDS,
      placed: true,
      messages: { slug: strings.apps.slugTaken },
    },
    {
      name: "leaves conflict to the alert when the form has no slug",
      error: apiError("conflict"),
      fieldNames: EDIT_FIELDS,
      placed: false,
      messages: {},
    },
    {
      name: "reports unhandled for a non-API error",
      error: new Error("boom"),
      fieldNames: CREATE_FIELDS,
      placed: false,
      messages: {},
    },
  ])("$name", ({ error, fieldNames, placed, messages }) => {
    const result = run(error, fieldNames);

    expect(result.placed).toBe(placed);
    expect(result.messages).toEqual({ name: undefined, slug: undefined, webhook_url: undefined, ...messages });
  });
});
