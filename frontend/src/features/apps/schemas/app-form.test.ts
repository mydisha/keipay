import { describe, expect, it } from "vitest";
import { strings } from "@/shared/lib/strings";
import { createAppSchema, editAppSchema, suggestSlug } from "./app-form";
import { createKeySchema, wibInputToDate } from "./key-form";

// T-41 / T-44: the form schemas mirror the contract's field rules.

const valid = {
  name: "Acme",
  slug: "acme",
  environment: "test" as const,
  redirect_success_url: "",
  redirect_failure_url: "",
  metadata: "",
};

function firstMessage(result: { success: boolean; error?: { issues: { message: string }[] } }) {
  return result.success ? null : result.error?.issues[0]?.message;
}

describe("suggestSlug", () => {
  it.each([
    ["Acme Store!", "acme-store"],
    ["  Café Ñandú  ", "cafe-nandu"],
    ["--Weird__Name--", "weird-name"],
    ["日本", ""],
    ["a".repeat(40), "a".repeat(32)],
    [`${"a".repeat(31)} b`, "a".repeat(31)],
  ])("%s → %s", (name, slug) => {
    expect(suggestSlug(name)).toBe(slug);
  });
});

describe("createAppSchema", () => {
  it("trims and lower-cases the slug before checking it", () => {
    const parsed = createAppSchema.parse({ ...valid, slug: "  Acme-Shop " });
    expect(parsed.slug).toBe("acme-shop");
  });

  it.each(["ab", "a".repeat(33), "-ab", "ab-", "ab_c", "ácme", ""])("rejects slug %j", (slug) => {
    expect(firstMessage(createAppSchema.safeParse({ ...valid, slug }))).toBe(strings.apps.slugInvalid);
  });

  it.each(["abc", "a".repeat(32), "a-1"])("accepts slug %j", (slug) => {
    expect(createAppSchema.safeParse({ ...valid, slug }).success).toBe(true);
  });

  it.each([
    ["blank", "  ", strings.auth.nameRequired],
    ["101 characters", "n".repeat(101), strings.auth.nameTooLong],
    ["a control character", "Acme\u0007", strings.auth.nameInvalid],
  ])("rejects a %s name", (_, name, message) => {
    expect(firstMessage(createAppSchema.safeParse({ ...valid, name }))).toBe(message);
  });

  it("accepts a 100-character name of emoji", () => {
    expect(createAppSchema.safeParse({ ...valid, name: "😀".repeat(100) }).success).toBe(true);
  });

  it.each([
    ["ftp", "ftp://x"],
    ["relative", "/relative"],
    ["no host", "https://"],
    ["with a space", "http://exa mple.com"],
    ["2049 bytes", `https://a.example/${"x".repeat(2031)}`],
  ])(
    "rejects a URL: %s",
    (_, url) => {
      expect(firstMessage(createAppSchema.safeParse({ ...valid, redirect_success_url: url }))).toBe(strings.apps.urlInvalid);
    },
  );

  it("accepts empty and http(s) URLs up to 2048 bytes", () => {
    const longest = `https://a.example/${"x".repeat(2048 - "https://a.example/".length)}`;
    for (const url of ["", "https://a.example/x", "http://localhost:3000/hook", longest]) {
      expect(createAppSchema.safeParse({ ...valid, redirect_success_url: url }).success).toBe(true);
    }
  });

  it("rejects an environment outside test and live", () => {
    expect(createAppSchema.safeParse({ ...valid, environment: "staging" }).success).toBe(false);
  });

  it("validates metadata text", () => {
    expect(firstMessage(editAppSchema.safeParse({ ...valid, metadata: "[]" }))).toBe(strings.apps.metadataObject);
  });
});

describe("createKeySchema", () => {
  it.each([
    ["blank", "  ", strings.keys.labelRequired],
    ["61 characters", "l".repeat(61), strings.keys.labelTooLong],
    ["a control character", "C\u0007I", strings.keys.labelInvalid],
  ])("rejects a %s label", (_, label, message) => {
    expect(firstMessage(createKeySchema.safeParse({ label, expires_at: "" }))).toBe(message);
  });

  it("accepts no expiry or a future one, and rejects the past", () => {
    expect(createKeySchema.safeParse({ label: "CI", expires_at: "" }).success).toBe(true);
    expect(createKeySchema.safeParse({ label: "CI", expires_at: "2099-01-01T00:00" }).success).toBe(true);
    expect(firstMessage(createKeySchema.safeParse({ label: "CI", expires_at: "2020-01-01T00:00" }))).toBe(
      strings.keys.expiresFuture,
    );
    expect(firstMessage(createKeySchema.safeParse({ label: "CI", expires_at: "not a date" }))).toBe(
      strings.keys.expiresFuture,
    );
  });
});

describe("wibInputToDate", () => {
  it("reads datetime-local values as UTC+7", () => {
    expect(wibInputToDate("2026-01-01T07:00")?.toISOString()).toBe("2026-01-01T00:00:00.000Z");
    expect(wibInputToDate("2026-01-01T07:00:30")?.toISOString()).toBe("2026-01-01T00:00:30.000Z");
  });

  it.each(["", "2026-01-01", "2026-13-01T00:00", "garbage"])("returns null for %j", (value) => {
    expect(wibInputToDate(value)).toBeNull();
  });
});
