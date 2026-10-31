import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import { dashboard } from "@/test/fixtures";
import { mockApi } from "@/test/http";
import { stubElementSize } from "@/test/layout";
import { renderApp, renderWithProviders } from "@/test/render";
import { TestModeBanner } from "./test-mode-banner";

function lastDashboardQuery(api: ReturnType<typeof mockApi>) {
  const request = api.to("GET", "/admin/dashboard").at(-1);
  return request ? Object.fromEntries(new URL(request.url).searchParams) : undefined;
}

beforeEach(() => {
  stubElementSize(640, 240);
});

describe("TestModeBanner", () => {
  it("is one status line: the title, then the scope (hidden on phones) and the payments caveat", () => {
    renderWithProviders(<TestModeBanner />);
    const banner = screen.getByRole("status");
    expect(banner).toHaveAttribute("data-testid", "test-mode-banner");
    expect(banner.firstElementChild).toHaveTextContent(/^Test mode$/);
    const scope = screen.getByText("The dashboard, lists and new records follow this switch.");
    // Narrow screens drop the scope sentence so the banner stays on one line.
    expect(scope).toHaveClass("max-sm:hidden");
    expect(banner).toHaveTextContent("Payments still show every environment.");
    expect(banner.querySelectorAll("p")).toHaveLength(1);
  });
});

describe("Test mode switch", () => {
  it("starts in live mode with no banner and asks the dashboard for live", async () => {
    const api = mockApi({ "GET /admin/dashboard": dashboard() });
    renderApp("/");
    expect(await screen.findByRole("switch", { name: "Test mode" })).toHaveAttribute("aria-checked", "false");
    await waitFor(() => expect(lastDashboardQuery(api)).toEqual({ range: "7d", environment: "live" }));
    expect(screen.queryByTestId("test-mode-banner")).not.toBeInTheDocument();
  });

  it("restores test mode from storage, then switching off removes the banner and asks for live", async () => {
    window.localStorage.setItem(storageKeys.mode, "test");
    const api = mockApi({ "GET /admin/dashboard": dashboard() });
    const { user } = renderApp("/");
    const toggle = await screen.findByRole("switch", { name: "Test mode" });
    expect(toggle).toHaveAttribute("aria-checked", "true");
    expect(screen.getByTestId("test-mode-banner")).toBeInTheDocument();
    await waitFor(() => expect(lastDashboardQuery(api)).toEqual({ range: "7d", environment: "test" }));

    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-checked", "false");
    expect(screen.queryByTestId("test-mode-banner")).not.toBeInTheDocument();
    expect(window.localStorage.getItem(storageKeys.mode)).toBe("live");
    await waitFor(() => expect(lastDashboardQuery(api)).toEqual({ range: "7d", environment: "live" }));
  });

  it("falls back to live mode when storage holds an unknown value", async () => {
    window.localStorage.setItem(storageKeys.mode, "staging");
    mockApi({ "GET /admin/dashboard": dashboard() });
    renderApp("/");
    expect(await screen.findByRole("switch", { name: "Test mode" })).toHaveAttribute("aria-checked", "false");
    expect(screen.queryByTestId("test-mode-banner")).not.toBeInTheDocument();
  });
});
