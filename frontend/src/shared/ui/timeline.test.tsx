import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Timeline } from "./timeline";
import { TooltipProvider } from "./tooltip";

describe("Timeline", () => {
  it("renders ordered items with time, title, status text and body", () => {
    render(
      <TooltipProvider>
        <Timeline
          now={new Date("2026-01-01T10:10:00Z")}
          items={[
            { id: "a", time: "2026-01-01T10:00:00Z", title: "Payment created", tone: "neutral" },
            { id: "b", time: "2026-01-01T10:05:00Z", title: "Paid", tone: "success", statusLabel: "Paid", body: "Via QRIS" },
          ]}
        />
      </TooltipProvider>,
    );
    const items = within(screen.getByRole("list")).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent("Payment created");
    expect(items[0]).toHaveTextContent("10 minutes ago");
    expect(within(items[1]!).getAllByText("Paid")).toHaveLength(2);
    expect(items[1]).toHaveTextContent("Via QRIS");
  });
});
