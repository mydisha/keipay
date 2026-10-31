import { render, screen } from "@testing-library/react";
import { Bar, BarChart, XAxis } from "recharts";
import { beforeEach, describe, expect, it } from "vitest";
import { stubElementSize } from "@/test/layout";
import { ChartContainer, ChartLegend, ChartLegendContent, type ChartConfig } from "./chart";

const config: ChartConfig = {
  created: { label: "Created", color: "var(--chart-5)" },
  successful: { label: "Successful", color: "var(--chart-1)" },
};

const data = [
  { day: "Mon", created: 4, successful: 3 },
  { day: "Tue", created: 6, successful: 5 },
];

beforeEach(() => {
  stubElementSize(640, 240);
});

describe("ChartContainer", () => {
  it("is an image named by its label, exposes the series colours as variables and renders the bars", () => {
    const { container } = render(
      <ChartContainer config={config} label="Payments per day">
        <BarChart data={data}>
          <XAxis dataKey="day" />
          <Bar dataKey="created" fill="var(--color-created)" isAnimationActive={false} />
          <Bar dataKey="successful" fill="var(--color-successful)" isAnimationActive={false} />
        </BarChart>
      </ChartContainer>,
    );
    const chart = screen.getByRole("img", { name: "Payments per day" });
    expect(chart.style.getPropertyValue("--color-created")).toBe("var(--chart-5)");
    expect(chart.style.getPropertyValue("--color-successful")).toBe("var(--chart-1)");
    expect(container.querySelectorAll(".recharts-bar-rectangle")).toHaveLength(4);
  });

  it("renders without a colour for a series that has none", () => {
    render(
      <ChartContainer config={{ created: { label: "Created" } }} label="Empty">
        <BarChart data={[]}>
          <Bar dataKey="created" />
        </BarChart>
      </ChartContainer>,
    );
    expect(screen.getByRole("img", { name: "Empty" }).style.getPropertyValue("--color-created")).toBe("");
  });
});

describe("ChartLegendContent", () => {
  it("lists every series under its configured label", () => {
    render(
      <ChartContainer config={config} label="Payments per day">
        <BarChart data={data}>
          <ChartLegend content={<ChartLegendContent />} />
          <Bar dataKey="created" fill="var(--color-created)" isAnimationActive={false} />
          <Bar dataKey="successful" fill="var(--color-successful)" isAnimationActive={false} />
        </BarChart>
      </ChartContainer>,
    );
    const items = screen.getAllByRole("listitem").map((item) => item.textContent);
    expect(items).toEqual(["Created", "Successful"]);
  });

  it("renders nothing for an empty payload", () => {
    const { container } = render(<ChartLegendContent payload={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
