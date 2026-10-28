import { describe, expect, it } from "vitest";
import { strings } from "@/shared/lib/strings";
import { metadataText, metadataToText, parseMetadataText } from "./metadata";

// T-42: the metadata editor's rules mirror the API's Metadata schema.

const keys = (n: number) => JSON.stringify(Object.fromEntries(Array.from({ length: n }, (_, i) => [`k${i}`, i])));
const emoji = "😀"; // one code point, two UTF-16 units, four UTF-8 bytes

function message(text: string) {
  const result = parseMetadataText(text);
  return result.ok ? null : result.message;
}

describe("parseMetadataText (T-42)", () => {
  it.each([
    ["empty", ""],
    ["whitespace", "  \n "],
    ["null", "null"],
  ])("treats %s as {}", (_, text) => {
    expect(parseMetadataText(text)).toEqual({ ok: true, value: {} });
  });

  it.each([
    ["invalid JSON", '{"a": }', strings.apps.metadataJson],
    ["an array", "[1]", strings.apps.metadataObject],
    ["a string", '"x"', strings.apps.metadataObject],
    ["a number", "1", strings.apps.metadataObject],
    ["51 keys", keys(51), strings.apps.metadataMaxKeys],
    ["an empty key", '{"": 1}', strings.apps.metadataKeyEmpty],
    ["a key with a newline", '{"a\\nb": 1}', strings.apps.metadataKeyControl],
    ["a key with U+0000", '{"a\\u0000": 1}', strings.apps.metadataKeyControl],
    ["a 41-character key", JSON.stringify({ ["k".repeat(41)]: 1 }), strings.apps.metadataKeyTooLong("k".repeat(41))],
    ["a 501-character value", JSON.stringify({ note: "é".repeat(501) }), strings.apps.metadataValueTooLong("note")],
    ["a nested object", '{"a": {"b": 1}}', strings.apps.metadataValueType("a")],
    ["a nested array", '{"a": [1]}', strings.apps.metadataValueType("a")],
    ["a value with U+0000", '{"a": "x\\u0000y"}', strings.apps.metadataValueNull("a")],
  ])("rejects %s", (_, text, expected) => {
    expect(message(text)).toBe(expected);
  });

  it("accepts the limits, counting code points not UTF-16 units", () => {
    expect(message(keys(50))).toBeNull();
    expect(message(JSON.stringify({ [emoji.repeat(40)]: 1 }))).toBeNull();
    expect(message(JSON.stringify({ [emoji.repeat(41)]: 1 }))).toBe(strings.apps.metadataKeyTooLong(emoji.repeat(41)));
    expect(message(JSON.stringify({ a: emoji.repeat(500) }))).toBeNull();
    expect(message(JSON.stringify({ a: "漢".repeat(500) }))).toBeNull();
    expect(message(JSON.stringify({ a: "é".repeat(500) }))).toBeNull();
    expect(message('{"s": "x", "n": 1.5, "t": true, "f": false, "z": null}')).toBeNull();
  });

  it("limits compact JSON to 8192 UTF-8 bytes", () => {
    // {"a":"…","b":"…",…}: build exactly 8192 bytes, then one more.
    const values: Record<string, string> = {};
    for (let i = 0; i < 17; i += 1) values[`k${String(i).padStart(2, "0")}`] = "x".repeat(470);
    const base = new TextEncoder().encode(JSON.stringify(values)).length;
    values.pad = "y".repeat(8192 - base - ',"pad":""'.length);
    expect(new TextEncoder().encode(JSON.stringify(values)).length).toBe(8192);
    expect(message(JSON.stringify(values, null, 2))).toBeNull();
    values.pad += "y";
    expect(message(JSON.stringify(values))).toBe(strings.apps.metadataTooLarge);
  });

  it("works as a zod field", () => {
    expect(metadataText.safeParse('{"a": 1}').success).toBe(true);
    const failed = metadataText.safeParse("[1]");
    expect(failed.success).toBe(false);
    expect(failed.error?.issues[0]?.message).toBe(strings.apps.metadataObject);
  });

  it("formats metadata for the editor", () => {
    expect(metadataToText({})).toBe("");
    expect(metadataToText({ a: 1 })).toBe('{\n  "a": 1\n}');
  });
});
