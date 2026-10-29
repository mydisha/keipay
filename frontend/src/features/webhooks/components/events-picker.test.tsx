import { describe, expect, it } from "vitest";
import { toggleSubscription } from "./events-picker";

describe("toggleSubscription", () => {
  it("appends types in the order they are ticked", () => {
    expect(toggleSubscription(["payment.settled"], "payment.paid", true)).toEqual(["payment.settled", "payment.paid"]);
  });

  it("makes * the only subscription", () => {
    expect(toggleSubscription(["payment.paid", "payment.*"], "*", true)).toEqual(["*"]);
  });

  it("replaces the types a group wildcard covers", () => {
    expect(toggleSubscription(["payment.paid", "refund.created"], "payment.*", true)).toEqual(["refund.created", "payment.*"]);
  });

  it("removes an unticked entry and never duplicates one", () => {
    expect(toggleSubscription(["payment.paid", "payment.failed"], "payment.paid", false)).toEqual(["payment.failed"]);
    expect(toggleSubscription(["payment.paid"], "payment.paid", true)).toEqual(["payment.paid"]);
  });
});
