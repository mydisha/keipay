import { vi } from "vitest";

/** Makes every element measure `width` × `height`, so size-driven components (charts) render under jsdom. */
export function stubElementSize(width: number, height: number) {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
    width,
    height,
    top: 0,
    left: 0,
    right: width,
    bottom: height,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  });
}
