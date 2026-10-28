import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatusBadge, statusTone } from "./status-badge";

describe("StatusBadge (T-24)", () => {
  it.each([
    ["PENDING", "Pending", "warning"],
    ["PAID", "Paid", "success"],
    ["SETTLED", "Settled", "info"],
    ["EXPIRED", "Expired", "neutral"],
    ["FAILED", "Failed", "danger"],
    ["DELIVERED", "Delivered", "success"],
    ["active", "Active", "success"],
    ["inactive", "Inactive", "neutral"],
  ])("%s renders the text %s with the %s tone", (status, label, tone) => {
    render(<StatusBadge status={status} />);
    const badge = screen.getByText(label);
    expect(badge).toHaveAttribute("data-tone", tone);
    expect(statusTone(status)).toBe(tone);
  });

  it("renders an unknown status as raw text in a neutral tone", () => {
    render(<StatusBadge status="REFUNDED_PARTIALLY" />);
    expect(screen.getByText("REFUNDED_PARTIALLY")).toHaveAttribute("data-tone", "neutral");
  });
});
