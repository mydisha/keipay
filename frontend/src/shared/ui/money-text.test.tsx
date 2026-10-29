import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MoneyText } from "./money-text";

describe("MoneyText", () => {
  it("renders rupiah right-aligned with tabular figures", () => {
    render(<MoneyText amount={150000} />);
    const el = screen.getByText("Rp150.000");
    expect(el).toHaveClass("text-right", "tabular-nums");
  });

  it("renders negative and zero amounts", () => {
    render(
      <>
        <MoneyText amount={-2500} />
        <MoneyText amount={0} currency="IDR" />
      </>,
    );
    expect(screen.getByText("-Rp2.500")).toBeInTheDocument();
    expect(screen.getByText("Rp0")).toBeInTheDocument();
  });
});
