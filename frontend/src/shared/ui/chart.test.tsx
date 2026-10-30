import { render, screen, within } from "@testing-library/react";
import { Bar, BarChart, XAxis } from "recharts";
import type { TooltipPayloadEntry } from "recharts";
import { beforeEach, describe, expect, it } from "vitest";
import { stubElementSize } from "@/test/layout";
import {
  ChartConfigProvider,
  ChartContainer,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "./chart";

const config: ChartConfig = {
  created: { label: "Created", color: "var(--chart-5)" },
  successful: { label: "Successful", color: "var(--chart-1)" },
};

const data = [
  { day: "Mon", created: 4, successful: 3 },
  { day: "Tue", created: 6, successful: 5 },
];

function entry(overrides: Partial<TooltipPayloadEntry>): TooltipPayloadEntry {
  return { graphicalItemId: "bar", ...overrides };
}

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
    expect(container.querySelector("svg.recharts-surface")).not.toBeNull();
    expect(container.querySelectorAll(".recharts-bar-rectangle")).toHaveLength(4);
  });

  it("shows the tooltip content for the default index with the configured labels", () => {
    render(
      <ChartContainer config={config} label="Payments per day">
        <BarChart data={data}>
          <XAxis dataKey="day" />
          <ChartTooltip defaultIndex={1} content={<ChartTooltipContent />} />
          <Bar dataKey="created" fill="var(--color-created)" isAnimationActive={false} />
          <Bar dataKey="successful" fill="var(--color-successful)" isAnimationActive={false} />
        </BarChart>
      </ChartContainer>,
    );
    const tooltip = document.querySelector(".recharts-tooltip-wrapper");
    expect(tooltip).toHaveTextContent("Tue");
    expect(tooltip).toHaveTextContent("Created6");
    expect(tooltip).toHaveTextContent("Successful5");
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

describe("ChartTooltipContent", () => {
  function renderTooltip(props: Partial<Parameters<typeof ChartTooltipContent>[0]>) {
    return render(
      <ChartConfigProvider value={{ config }}>
        <ChartTooltipContent active label="Mon" payload={[entry({ dataKey: "created", name: "created", value: 4 })]} {...props} />
      </ChartConfigProvider>,
    );
  }

  it("shows the configured series label and the value formatted for id-ID", () => {
    const { container } = renderTooltip({ payload: [entry({ dataKey: "created", name: "created", value: 1500 })] });
    expect(within(container).getByText("Mon")).toBeInTheDocument();
    expect(within(container).getByText("Created")).toBeInTheDocument();
    expect(within(container).getByText("1.500")).toHaveClass("tabular-nums");
  });

  it("renders nothing while inactive or without items", () => {
    const inactive = renderTooltip({ active: false });
    expect(inactive.container).toBeEmptyDOMElement();
    const empty = renderTooltip({ payload: [] });
    expect(empty.container).toBeEmptyDOMElement();
  });

  it("uses the formatter and hides the label on request", () => {
    const { container } = renderTooltip({
      hideLabel: true,
      formatter: (value, name) => `${name}: Rp${String(value)}`,
    });
    expect(within(container).queryByText("Mon")).not.toBeInTheDocument();
    expect(within(container).getByText("Created: Rp4")).toBeInTheDocument();
  });

  it("falls back to the item's name for a series that is not configured", () => {
    const { container } = renderTooltip({ payload: [entry({ dataKey: "other", name: "other", value: 2 })] });
    expect(within(container).getByText("other")).toBeInTheDocument();
  });
});

describe("ChartLegendContent", () => {
  it("lists the configured labels and nothing for an empty payload", () => {
    render(
      <ChartConfigProvider value={{ config }}>
        <ChartLegendContent payload={[{ value: "created", dataKey: "created", color: "var(--chart-5)" }]} />
      </ChartConfigProvider>,
    );
    expect(screen.getByRole("list")).toHaveTextContent("Created");
    const { container } = render(
      <ChartConfigProvider value={{ config }}>
        <ChartLegendContent payload={[]} />
      </ChartConfigProvider>,
    );
    expect(within(container).queryByRole("list")).not.toBeInTheDocument();
  });
});
