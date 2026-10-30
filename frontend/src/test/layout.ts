import { vi } from "vitest";

const CHART_CONTAINER = ".recharts-responsive-container";

/**
 * Makes elements matching `selector` (by default the recharts responsive
 * container) measure `width` × `height`, so size-driven components render under
 * jsdom. Everything else keeps measuring 0×0, as legends and tooltips must.
 */
export function stubElementSize(width: number, height: number, selector = CHART_CONTAINER) {
  const original = HTMLElement.prototype.getBoundingClientRect;
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    if (!this.matches(selector)) return original.call(this);
    return { width, height, top: 0, left: 0, right: width, bottom: height, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
  });
}
