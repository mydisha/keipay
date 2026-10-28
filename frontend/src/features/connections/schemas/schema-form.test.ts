import { describe, expect, it } from "vitest";
import { provider } from "@/test/fixtures";
import type { SchemaField } from "../api/connections";
import {
  codePointLength,
  credentialPayload,
  hasUnsupportedField,
  schemaDefaults,
  schemaFormSchema,
  schemaPayload,
  type SchemaValues,
} from "./schema-form";

// T-50: the zod builder behind every provider form.

function field(overrides: Partial<SchemaField> & Pick<SchemaField, "key" | "type">): SchemaField {
  return { label: overrides.key, description: "", secret: false, required: false, default: null, ...overrides };
}

/** The first error message per key, or {} when the values pass. */
function errors(fields: SchemaField[], values: SchemaValues, mode: "create" | "replace" = "create") {
  const result = schemaFormSchema(fields, mode).safeParse(values);
  if (result.success) return {};
  const out: Record<string, string> = {};
  for (const issue of result.error.issues) {
    const key = String(issue.path[0]);
    out[key] ??= issue.message;
  }
  return out;
}

const credentials = provider().credential_schema.fields;
const settings = provider().settings_schema.fields;

describe("schemaFormSchema (T-50)", () => {
  it("requires required fields in create mode, after trimming", () => {
    expect(errors(credentials, { secret_key: "  ", callback_token: "" })).toEqual({
      secret_key: "This field is required.",
      callback_token: "This field is required.",
    });
  });

  it("leaves optional fields alone when blank", () => {
    expect(errors(settings, { invoice_duration: "", locale: "" })).toEqual({});
  });

  it("counts min and max length in code points", () => {
    const fields = [field({ key: "token", type: "string", min_length: 3, max_length: 4 })];
    // An emoji is one code point but two UTF-16 units; "ñ" is one code point but two UTF-8 bytes.
    expect(codePointLength("😀😀")).toBe(2);
    expect(errors(fields, { token: "😀😀" })).toEqual({ token: "Use at least 3 characters." });
    expect(errors(fields, { token: "😀😀😀😀" })).toEqual({});
    expect(errors(fields, { token: "ñññññ" })).toEqual({ token: "Use at most 4 characters." });
    expect(errors(fields, { token: "ñññ" })).toEqual({});
  });

  it("explains a pattern with pattern_hint, never the regex", () => {
    const message = errors(credentials, { secret_key: "sk_live_123", callback_token: "token-123456" }).secret_key;
    expect(message).toBe("Starts with xnd_development_ (test) or xnd_production_ (live).");
    expect(message).not.toContain("^xnd");
    // The pattern is anchored to the whole value.
    expect(errors(credentials, { secret_key: "xnd_development_abc!", callback_token: "token-123456" })).toHaveProperty(
      "secret_key",
    );
    expect(errors(credentials, { secret_key: "xnd_development_abc", callback_token: "token-123456" })).toEqual({});
  });

  it("falls back to a generic message when a pattern has no hint", () => {
    const fields = [field({ key: "code", type: "string", pattern: "[A-Z]{3}" })];
    expect(errors(fields, { code: "abc" })).toEqual({ code: "This value doesn't have the expected format." });
  });

  it("parses integers and checks minimum and maximum", () => {
    expect(errors(settings, { invoice_duration: "1.5", locale: "" })).toEqual({ invoice_duration: "Enter a whole number." });
    expect(errors(settings, { invoice_duration: "abc", locale: "" })).toEqual({ invoice_duration: "Enter a whole number." });
    expect(errors(settings, { invoice_duration: "59", locale: "" })).toEqual({ invoice_duration: "Use 60 or more." });
    expect(errors(settings, { invoice_duration: "31536001", locale: "" })).toEqual({
      invoice_duration: "Use 31.536.000 or less.",
    });
    expect(errors(settings, { invoice_duration: " 3600 ", locale: "" })).toEqual({});
    const unbounded = [field({ key: "n", type: "integer" })];
    expect(errors(unbounded, { n: "99999999999999999999" })).toEqual({ n: "Enter a whole number." });
  });

  it("accepts only listed enum values", () => {
    expect(errors(settings, { invoice_duration: "", locale: "fr" })).toEqual({ locale: "Choose one of the options." });
    expect(errors(settings, { invoice_duration: "", locale: "en" })).toEqual({});
    const required = [field({ key: "mode", type: "enum", required: true, options: [{ value: "a", label: "A" }] })];
    expect(errors(required, { mode: "" })).toEqual({ mode: "This field is required." });
  });

  it("takes booleans as booleans", () => {
    const fields = [field({ key: "capture", type: "boolean", default: true })];
    expect(errors(fields, { capture: false })).toEqual({});
    expect(schemaFormSchema(fields).safeParse({ capture: "yes" }).success).toBe(false);
  });

  it("in replace mode treats blanks as keep but needs at least one value", () => {
    expect(errors(credentials, { secret_key: "", callback_token: "" }, "replace")).toEqual({
      secret_key: "Enter at least one new value.",
    });
    expect(errors(credentials, { secret_key: "", callback_token: "new-token-123" }, "replace")).toEqual({});
    // Filled values still follow every rule.
    expect(errors(credentials, { secret_key: "", callback_token: "short" }, "replace")).toEqual({
      callback_token: "Use at least 8 characters.",
    });
  });
});

describe("schema defaults and payloads (T-50)", () => {
  it("prefills defaults and current values but never secrets", () => {
    const fields = [
      field({ key: "lifetime", type: "integer", default: 3600 }),
      field({ key: "locale", type: "enum", default: "id", options: [{ value: "id", label: "Indonesian" }] }),
      field({ key: "capture", type: "boolean", default: true }),
      field({ key: "flag", type: "boolean" }),
      field({ key: "secret_key", type: "string", secret: true }),
    ];
    expect(schemaDefaults(fields)).toEqual({ lifetime: "3600", locale: "id", capture: true, flag: false, secret_key: "" });
    expect(schemaDefaults(fields, { lifetime: 60, capture: false, secret_key: "leak" })).toEqual({
      lifetime: "60",
      locale: "id",
      capture: false,
      flag: false,
      secret_key: "",
    });
  });

  it("builds request values: trimmed, blanks left out, integers as numbers", () => {
    expect(schemaPayload(settings, { invoice_duration: " 600 ", locale: "" })).toEqual({ invoice_duration: 600 });
    expect(schemaPayload([field({ key: "capture", type: "boolean" })], { capture: false })).toEqual({ capture: false });
    expect(credentialPayload(credentials, { secret_key: "", callback_token: " tok-123456 " })).toEqual({
      callback_token: "tok-123456",
    });
  });

  it("flags a field type the console doesn't know", () => {
    expect(hasUnsupportedField(credentials)).toBe(false);
    const future = { ...field({ key: "cert", type: "string" }), type: "file" } as unknown as SchemaField;
    expect(hasUnsupportedField([...credentials, future])).toBe(true);
    // An unknown field never blocks validation of the others.
    expect(errors([future], {})).toEqual({});
  });
});
