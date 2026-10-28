import { describe, expect, it } from "vitest";
import {
  canonicalEmail,
  codePointLength,
  emailField,
  hasControlChars,
  isValidEmail,
  nameField,
  newPasswordField,
  textField,
  utf8Length,
} from "./credentials";

describe("canonicalEmail", () => {
  it("trims, applies NFC and lower-cases", () => {
    expect(canonicalEmail("  Ops@Example.COM ")).toBe("ops@example.com");
    expect(canonicalEmail("ÜSER@Example.COM")).toBe("üser@example.com");
    expect(canonicalEmail("üser@example.com")).toBe(canonicalEmail("üser@example.com"));
  });
});

describe("isValidEmail", () => {
  it.each(["ops@example.com", "üser@example.com", "a@b.co", "  Ops@Example.COM ", "用户@例子.广告"])("accepts %s", (email) => {
    expect(isValidEmail(email)).toBe(true);
  });

  it.each([
    "",
    "a",
    "a@b",
    "a@@b.com",
    "a b@c.com",
    "a@.com",
    "a@b..com",
    "a@b.com.",
    '"a"@b.com',
    "a<b>@c.com",
    "a@b.com\u0007",
    `${"a".repeat(65)}@example.com`,
    `a@${"b".repeat(250)}.com`,
  ])("rejects %j", (email) => {
    expect(isValidEmail(email)).toBe(false);
  });

  it("allows a 64-byte local part", () => {
    expect(isValidEmail(`${"a".repeat(64)}@example.com`)).toBe(true);
  });
});

describe("text helpers", () => {
  it("counts code points and UTF-8 bytes", () => {
    expect(codePointLength("")).toBe(0);
    expect(codePointLength("a😀é")).toBe(3);
    expect(utf8Length("a😀é")).toBe(7);
  });

  it("finds C0, DEL and C1 control characters only", () => {
    for (const c of ["\u0000", "\u0007", "\u001f", "\u007f", "\u0085", "\u009f"]) {
      expect(hasControlChars(`a${c}b`)).toBe(true);
    }
    for (const text of ["", "Acme Store", "\u00a0", "\u200b", "😀"]) {
      expect(hasControlChars(text)).toBe(false);
    }
  });
});

describe("form fields", () => {
  it("text: uses the given limit and messages", () => {
    const field = textField({ max: 3, messages: { required: "req", tooLong: "long", invalid: "bad" } });
    expect(field.parse(" 😀😀😀 ")).toBe("😀😀😀");
    expect(field.safeParse("  ").error?.issues[0]?.message).toBe("req");
    expect(field.safeParse("abcd").error?.issues[0]?.message).toBe("long");
    expect(field.safeParse("a\u0085").error?.issues[0]?.message).toBe("bad");
  });

  it("email: required, then format", () => {
    expect(emailField().safeParse("").error?.issues[0]?.message).toBe("Enter your email.");
    expect(emailField().safeParse("nope").error?.issues[0]?.message).toMatch(/Enter an email address/);
    expect(emailField().parse("  ops@example.com ")).toBe("ops@example.com");
  });

  it("name: trimmed, 1..100 code points, no control characters", () => {
    expect(nameField().parse("  Ops Lead ")).toBe("Ops Lead");
    expect(nameField().safeParse("   ").success).toBe(false);
    expect(nameField().safeParse("🙂".repeat(100)).success).toBe(true);
    expect(nameField().safeParse("a".repeat(101)).error?.issues[0]?.message).toBe("Use at most 100 characters.");
    expect(nameField().safeParse("Ops\u0000Lead").success).toBe(false);
  });

  it("password: at least 12 code points and at most 72 UTF-8 bytes, kept as typed", () => {
    expect(newPasswordField().safeParse("a".repeat(11)).error?.issues[0]?.message).toBe("Use at least 12 characters.");
    expect(newPasswordField().parse(" twelve chars ")).toBe(" twelve chars ");
    expect(newPasswordField().safeParse("ééééééééééé1").success).toBe(true);
    expect(newPasswordField().safeParse("a".repeat(72)).success).toBe(true);
    expect(newPasswordField().safeParse("a".repeat(73)).error?.issues[0]?.message).toMatch(/at most 72 bytes/);
    // 25 three-byte characters: 25 code points but 75 bytes.
    expect(newPasswordField().safeParse("€".repeat(25)).success).toBe(false);
  });
});
