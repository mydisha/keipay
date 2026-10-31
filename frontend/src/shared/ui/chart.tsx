import { createContext, useContext, type ComponentProps, type CSSProperties, type ReactNode } from "react";
import * as RechartsPrimitive from "recharts";
import type { LegendPayload } from "recharts";
import { cn } from "@/shared/lib/utils";

/** Series colours come only from the chart tokens, so both themes stay in step. */
export type ChartColor = `var(--chart-${1 | 2 | 3 | 4 | 5})`;

export interface ChartSeriesConfig {
  label?: ReactNode;
  color?: ChartColor;
}

/** One entry per series key; the key is also the `--color-<key>` variable the container exposes. */
export type ChartConfig = Record<string, ChartSeriesConfig>;

// `ChartContainer` hands its config to the legend content inside the chart.
const ChartContext = createContext<{ config: ChartConfig }>({ config: {} });

function useChart() {
  return useContext(ChartContext);
}

function colorVars(config: ChartConfig): CSSProperties {
  const vars: Record<string, string> = {};
  for (const [key, series] of Object.entries(config)) {
    if (series.color) vars[`--color-${key}`] = series.color;
  }
  return vars as CSSProperties;
}

interface ChartContainerProps extends Omit<ComponentProps<"div">, "children" | "aria-label"> {
  config: ChartConfig;
  /** What the chart shows, for assistive technology; the data must also exist as text nearby. */
  label: string;
  children: ComponentProps<typeof RechartsPrimitive.ResponsiveContainer>["children"];
}

// A starting size lets the chart render before the container has been measured.
// jsdom measures every element as 0×0; tests stub the size (see `src/test/layout.ts`).
const INITIAL_DIMENSION = { width: 640, height: 240 };

export function ChartContainer({ config, label, className, style, children, ...props }: ChartContainerProps) {
  return (
    <ChartContext.Provider value={{ config }}>
      <div
        data-slot="chart"
        role="img"
        aria-label={label}
        className={cn(
          "flex aspect-video justify-center text-xs [&_.recharts-cartesian-axis-tick_text]:fill-muted-foreground [&_.recharts-cartesian-grid_line]:stroke-border [&_.recharts-curve.recharts-tooltip-cursor]:stroke-border [&_.recharts-layer]:outline-hidden [&_.recharts-rectangle.recharts-tooltip-cursor]:fill-muted [&_.recharts-sector]:outline-hidden [&_.recharts-surface]:outline-hidden",
          className,
        )}
        style={{ ...colorVars(config), ...style }}
        {...props}
      >
        <RechartsPrimitive.ResponsiveContainer initialDimension={INITIAL_DIMENSION}>{children}</RechartsPrimitive.ResponsiveContainer>
      </div>
    </ChartContext.Provider>
  );
}

export const ChartTooltip = RechartsPrimitive.Tooltip;

export const ChartLegend = RechartsPrimitive.Legend;

interface ChartLegendContentProps {
  payload?: ReadonlyArray<LegendPayload>;
  verticalAlign?: "top" | "bottom" | "middle";
  className?: string;
}

/** The series of a chart with their configured labels and a swatch in each series colour. */
export function ChartLegendContent({ payload, verticalAlign = "bottom", className }: ChartLegendContentProps) {
  const { config } = useChart();
  const items = payload ?? [];
  if (items.length === 0) return null;
  return (
    <ul className={cn("flex flex-wrap items-center justify-center gap-4", verticalAlign === "top" ? "pb-3" : "pt-3", className)}>
      {items.map((item, index) => {
        const key = typeof item.dataKey === "string" ? item.dataKey : String(item.value ?? index);
        return (
          <li key={key} className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span
              aria-hidden="true"
              className="size-2 shrink-0 rounded-[2px] bg-(--indicator-color)"
              style={{ "--indicator-color": item.color } as CSSProperties}
            />
            {config[key]?.label ?? item.value}
          </li>
        );
      })}
    </ul>
  );
}
