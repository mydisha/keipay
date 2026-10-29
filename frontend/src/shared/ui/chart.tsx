import { createContext, useContext, type ComponentProps, type ComponentType, type CSSProperties, type ReactNode } from "react";
import * as RechartsPrimitive from "recharts";
import type { LegendPayload, TooltipPayloadEntry, TooltipValueType } from "recharts";
import { cn } from "@/shared/lib/utils";

/** Series colours come only from the chart tokens, so both themes stay in step. */
export type ChartColor = `var(--chart-${1 | 2 | 3 | 4 | 5})`;

export interface ChartSeriesConfig {
  label?: ReactNode;
  icon?: ComponentType;
  color?: ChartColor;
}

/** One entry per series key; the key is also the `--color-<key>` variable the container exposes. */
export type ChartConfig = Record<string, ChartSeriesConfig>;

const ChartContext = createContext<{ config: ChartConfig }>({ config: {} });

/** Gives tooltip and legend content their series config; `ChartContainer` does this for a whole chart. */
export const ChartConfigProvider = ChartContext.Provider;

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

type Keyed = Record<string, unknown>;

function isKeyed(value: unknown): value is Keyed {
  return typeof value === "object" && value !== null;
}

/** The series config for a tooltip or legend item: by `key`, or by the string that item carries under `key`. */
function seriesFor(config: ChartConfig, item: unknown, key: string): ChartSeriesConfig | undefined {
  if (!isKeyed(item)) return undefined;
  const inner = isKeyed(item.payload) ? item.payload : undefined;
  let configKey = key;
  if (typeof item[key] === "string") configKey = item[key];
  else if (inner && typeof inner[key] === "string") configKey = inner[key];
  return config[configKey] ?? config[key];
}

function formatValue(value: TooltipValueType | undefined): ReactNode {
  if (typeof value === "number") return value.toLocaleString("id-ID");
  if (Array.isArray(value)) return value.join(" – ");
  return value ?? null;
}

interface ChartTooltipContentProps {
  active?: boolean;
  payload?: ReadonlyArray<TooltipPayloadEntry>;
  label?: ReactNode;
  className?: string;
  indicator?: "dot" | "line";
  hideLabel?: boolean;
  hideIndicator?: boolean;
  /** The payload field that names the series; defaults to each item's `name`/`dataKey`. */
  nameKey?: string;
  /** The config key whose label replaces the axis label. */
  labelKey?: string;
  labelFormatter?: (label: ReactNode, payload: ReadonlyArray<TooltipPayloadEntry>) => ReactNode;
  formatter?: (value: TooltipValueType | undefined, name: string, item: TooltipPayloadEntry, index: number) => ReactNode;
}

export function ChartTooltipContent({
  active,
  payload,
  label,
  className,
  indicator = "dot",
  hideLabel = false,
  hideIndicator = false,
  nameKey,
  labelKey,
  labelFormatter,
  formatter,
}: ChartTooltipContentProps) {
  const { config } = useChart();
  const items = (payload ?? []).filter((item) => item.type !== "none");
  if (!active || items.length === 0) return null;

  let heading: ReactNode = null;
  if (!hideLabel) {
    const first = items[0];
    const key = labelKey ?? (typeof first?.dataKey === "string" ? first.dataKey : "value");
    const series = labelKey && first ? seriesFor(config, first, labelKey) : undefined;
    const text = series?.label ?? (!labelKey && typeof label === "string" ? (config[label]?.label ?? label) : label);
    heading = labelFormatter ? labelFormatter(text, items) : text;
    if (heading === undefined || heading === null) heading = config[key]?.label ?? null;
  }

  return (
    <div
      className={cn(
        "grid min-w-32 items-start gap-1.5 rounded-md border bg-popover px-2.5 py-1.5 text-xs text-popover-foreground shadow-md",
        className,
      )}
    >
      {heading !== null && <div className="font-medium">{heading}</div>}
      <div className="grid gap-1.5">
        {items.map((item, index) => {
          const key = nameKey ?? (typeof item.name === "string" ? item.name : typeof item.dataKey === "string" ? item.dataKey : "value");
          const series = seriesFor(config, item, key);
          const color = item.color ?? (isKeyed(item.payload) && typeof item.payload.fill === "string" ? item.payload.fill : undefined);
          const name = series?.label ?? item.name ?? key;
          return (
            <div key={`${key}-${index}`} className="flex w-full flex-wrap items-center gap-2">
              {series?.icon ? (
                <series.icon />
              ) : (
                !hideIndicator && (
                  <span
                    aria-hidden="true"
                    className={cn("shrink-0 rounded-[2px] bg-(--indicator-color)", indicator === "dot" ? "size-2.5" : "h-3 w-0.5")}
                    style={{ "--indicator-color": color } as CSSProperties}
                  />
                )
              )}
              <span className="text-muted-foreground">{name}</span>
              <span className="ml-auto font-medium tabular-nums">
                {formatter ? formatter(item.value, String(name), item, index) : formatValue(item.value)}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export const ChartLegend = RechartsPrimitive.Legend;

interface ChartLegendContentProps {
  payload?: ReadonlyArray<LegendPayload>;
  verticalAlign?: "top" | "bottom" | "middle";
  className?: string;
  hideIcon?: boolean;
  nameKey?: string;
}

export function ChartLegendContent({ payload, verticalAlign = "bottom", className, hideIcon = false, nameKey }: ChartLegendContentProps) {
  const { config } = useChart();
  const items = payload ?? [];
  if (items.length === 0) return null;
  return (
    <ul className={cn("flex flex-wrap items-center justify-center gap-4", verticalAlign === "top" ? "pb-3" : "pt-3", className)}>
      {items.map((item, index) => {
        const key = nameKey ?? (typeof item.dataKey === "string" ? item.dataKey : (item.value ?? "value"));
        const series = seriesFor(config, item, key);
        return (
          <li key={`${key}-${index}`} className="flex items-center gap-1.5 text-xs text-muted-foreground">
            {series?.icon && !hideIcon ? (
              <series.icon />
            ) : (
              <span
                aria-hidden="true"
                className="size-2 shrink-0 rounded-[2px] bg-(--indicator-color)"
                style={{ "--indicator-color": item.color } as CSSProperties}
              />
            )}
            {series?.label ?? item.value}
          </li>
        );
      })}
    </ul>
  );
}
