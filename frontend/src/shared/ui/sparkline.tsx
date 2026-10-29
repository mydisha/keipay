import { cn } from "@/shared/lib/utils";

const WIDTH = 100;
const HEIGHT = 32;
const PAD = 2;

/** The polyline points for `values`, scaled to the viewBox; a flat series draws a mid-height line. */
export function sparklinePoints(values: number[]): string {
  if (values.length === 0) return "";
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min;
  const stepX = values.length > 1 ? (WIDTH - PAD * 2) / (values.length - 1) : 0;
  return values
    .map((value, index) => {
      const x = values.length > 1 ? PAD + index * stepX : WIDTH / 2;
      const y = span === 0 ? HEIGHT / 2 : HEIGHT - PAD - ((value - min) / span) * (HEIGHT - PAD * 2);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
}

/** A small decorative trend line; the numbers it shows must also appear as text nearby. */
export function Sparkline({ values, className }: { values: number[]; className?: string }) {
  if (values.length === 0) return null;
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      preserveAspectRatio="none"
      aria-hidden="true"
      className={cn("h-8 w-full text-chart-1", className)}
    >
      <polyline
        points={sparklinePoints(values)}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}
