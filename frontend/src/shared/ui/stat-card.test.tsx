import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Sparkline, sparklinePoints } from "./sparkline";
import { StatCard } from "./stat-card";

describe("StatCard", () => {
  it("shows the label before the value so both read as one figure", () => {
    render(<StatCard label="Payments" value="1.234" />);
    expect(screen.getByText("Payments")).toBeInTheDocument();
    expect(screen.getByText("1.234")).toHaveClass("tabular-nums");
    expect(screen.getByText("Payments").parentElement).toHaveTextContent("Payments1.234");
  });

  it("renders the change as text with an arrow that is hidden from assistive technology", () => {
    const { container } = render(
      <StatCard label="Gross volume" value="Rp150.000" delta={{ direction: "up", text: "+12% vs previous 7 days", tone: "positive" }} hint="IDR" />,
    );
    expect(screen.getByText("+12% vs previous 7 days")).toBeInTheDocument();
    expect(screen.getByText("IDR")).toBeInTheDocument();
    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
    expect(screen.queryByTestId("stat-card-skeleton")).not.toBeInTheDocument();
  });

  it("renders a flat change and an empty value without crashing", () => {
    render(<StatCard label="Success rate" value="—" delta={{ direction: "flat", text: "No change" }} />);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.getByText("No change")).toBeInTheDocument();
  });

  it("shows a busy skeleton instead of the figure while loading", () => {
    render(<StatCard label="Payments" value="1.234" isLoading />);
    expect(screen.getByTestId("stat-card-skeleton")).toHaveAttribute("aria-busy", "true");
    expect(screen.queryByText("1.234")).not.toBeInTheDocument();
  });

  it("draws the sparkline as a decorative svg", () => {
    const { container } = render(<StatCard label="Payments" value="3" sparkline={<Sparkline values={[1, 3, 2]} />} />);
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("aria-hidden", "true");
    expect(svg?.querySelector("polyline")).toHaveAttribute("points", sparklinePoints([1, 3, 2]));
  });
});

describe("Sparkline", () => {
  it("renders nothing for an empty series", () => {
    const { container } = render(<Sparkline values={[]} />);
    expect(container.querySelector("svg")).toBeNull();
  });

  it("draws a flat series as a mid-height line and a single value as one point", () => {
    expect(sparklinePoints([5, 5, 5])).toBe("2.0,16.0 50.0,16.0 98.0,16.0");
    expect(sparklinePoints([7])).toBe("50.0,16.0");
  });

  it("scales a rising series from the bottom to the top of the box", () => {
    expect(sparklinePoints([0, 10])).toBe("2.0,30.0 98.0,2.0");
  });
});
