import { vi } from "vitest";

/** Makes `(max-width: 767px)` match, i.e. a phone-width viewport. */
export function setViewport(kind: "mobile" | "desktop") {
  vi.stubGlobal(
    "matchMedia",
    vi.fn((query: string) => ({
      matches: kind === "mobile" && query.includes("max-width: 767px"),
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  );
}
