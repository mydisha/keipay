import { describe, expect, it } from "vitest";
import { withRawMetadata } from "./apps";

describe("withRawMetadata", () => {
  it("sends the typed metadata text so large integers keep every digit", () => {
    const text = '{"order_seq": 12345678901234567890}';
    const body = { name: "Acme", metadata: JSON.parse(text) as Record<string, number> };
    const sent = withRawMetadata(text)(body);
    expect(sent).toBe('{"name":"Acme","metadata":{"order_seq": 12345678901234567890}}');
  });

  it("handles a body with only metadata", () => {
    expect(withRawMetadata('{"a":1}')({ metadata: { a: 1 } })).toBe('{"metadata":{"a":1}}');
  });

  it("falls back to JSON.stringify without text or without metadata in the body", () => {
    expect(withRawMetadata(undefined)({ name: "A", metadata: { a: 1 } })).toBe('{"name":"A","metadata":{"a":1}}');
    expect(withRawMetadata("  ")({ name: "A" })).toBe('{"name":"A"}');
    expect(withRawMetadata('{"a":1}')({ name: "A" })).toBe('{"name":"A"}');
  });
});
