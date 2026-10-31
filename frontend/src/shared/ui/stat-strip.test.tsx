import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Stat, StatStrip, StatStripSkeleton } from "./stat-strip";

describe("StatStrip", () => {
  it("pairs every label with its value in one definition list", () => {
    render(
      <StatStrip aria-label="Key figures">
        <Stat label="Payments" value="1.234" />
        <Stat label="Success rate" value="80%" />
      </StatStrip>,
    );
    const list = screen.getByLabelText("Key figures");
    expect(list.tagName).toBe("DL");
    expect(screen.getByText("Payments").tagName).toBe("DT");
    expect(screen.getByText("1.234")).toHaveClass("tabular-nums");
    expect(screen.getByText("1.234").closest("dd")).toBeInTheDocument();
  });

  it("writes the change as text with its comparison, coloured by tone", () => {
    render(
      <StatStrip>
        <Stat label="Gross volume" value="Rp150.000" change={{ text: "+12,5%", tone: "positive" }} hint="IDR" />
      </StatStrip>,
    );
    expect(screen.getByText("+12,5%")).toHaveClass("text-success-foreground");
    expect(screen.getByText("IDR")).toBeInTheDocument();
  });

  it("falls back to the neutral tone and renders without a change or hint", () => {
    render(
      <StatStrip>
        <Stat label="Payments" value="0" change={{ text: "No change" }} />
        <Stat label="Delivery success" value="Not yet" />
      </StatStrip>,
    );
    expect(screen.getByText("No change")).toHaveClass("text-muted-foreground");
    expect(screen.getByText("Not yet")).toBeInTheDocument();
  });

  it("shows a busy skeleton with one cell per figure", () => {
    const { container } = render(<StatStripSkeleton count={3} />);
    expect(screen.getByTestId("stat-strip-skeleton")).toHaveAttribute("aria-busy", "true");
    expect(container.querySelectorAll("[data-testid=stat-strip-skeleton] > div")).toHaveLength(3);
  });
});
