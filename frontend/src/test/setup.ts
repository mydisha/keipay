import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { toast } from "sonner";
import { afterEach, beforeEach, vi } from "vitest";
import { clearCookies } from "./cookies";
import { mockApi } from "./http";

// Feature pages are lazy routes. On a cold Vite transform cache (the gate runs
// `npm ci` first) the first import of a page can take more than the default
// 1s findBy* timeout and 5s test timeout, so allow for it. `/` loads the
// dashboard chunk with recharts, which took over 5s per worker on a busy
// machine, hence 10s.
configure({ asyncUtilTimeout: 10_000 });
vi.setConfig({ testTimeout: 20_000 });

// Every test starts with a signed-in owner; tests call mockApi again to change that.
beforeEach(() => {
  mockApi();
});

afterEach(() => {
  cleanup();
  // Sonner keeps toasts in module state and replays the active ones to the next
  // Toaster, so a toast from one test would otherwise show up in the next.
  toast.dismiss();
  window.localStorage.clear();
  window.sessionStorage.clear();
  clearCookies();
  document.documentElement.className = "";
});

// jsdom lacks these browser APIs that Radix and our media queries use.
if (!window.matchMedia) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: vi.fn((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
}

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= ResizeObserverStub as unknown as typeof ResizeObserver;

Element.prototype.scrollIntoView ??= function scrollIntoView() {};
Element.prototype.hasPointerCapture ??= function hasPointerCapture() {
  return false;
};
Element.prototype.releasePointerCapture ??= function releasePointerCapture() {};
Element.prototype.setPointerCapture ??= function setPointerCapture() {};
