import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { setToken } from "@/shared/api/session";
import { payment, tenant } from "@/test/fixtures";
import { mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp, TEST_TOKEN } from "@/test/render";

beforeEach(() => {
  setToken(TEST_TOKEN);
  setViewport("desktop");
});

function lastQuery(api: ReturnType<typeof mockApi>) {
  const requests = api.to("GET", "/admin/payments");
  return new URL(requests[requests.length - 1]!.url).searchParams;
}

describe("Payments page", () => {
  it("shows payments with app name, money, status text and total", async () => {
    mockApi({
      "GET /admin/tenants": { tenants: [tenant()] },
      "GET /admin/payments": { payments: [payment()], total: 120 },
    });
    renderApp("/payments");
    expect(await screen.findByText("Rp150.000")).toBeInTheDocument();
    const table = screen.getByRole("table", { name: "Payments" });
    expect(within(table).getByText("Paid")).toBeInTheDocument();
    expect(await within(table).findByText("Demo App")).toBeInTheDocument();
    expect(within(table).getByText("order-001")).toBeInTheDocument();
    expect(screen.getByText("Showing 1 of 120 payments")).toBeInTheDocument();
  });

  it("filters by status chip and app, and clears filters", async () => {
    const api = mockApi({
      "GET /admin/tenants": { tenants: [tenant(), tenant({ id: 2, name: "Globex", slug: "globex" })] },
      "GET /admin/payments": (request: Request) => {
        const status = new URL(request.url).searchParams.get("status");
        return new Response(
          JSON.stringify(status === "FAILED" ? { payments: [], total: 0 } : { payments: [payment()], total: 1 }),
          { headers: { "Content-Type": "application/json" } },
        );
      },
    });
    const { user } = renderApp("/payments");
    await screen.findByText("Rp150.000");
    expect(lastQuery(api).has("status")).toBe(false);

    await user.click(screen.getByRole("button", { name: "Failed" }));
    await waitFor(() => expect(lastQuery(api).get("status")).toBe("FAILED"));
    expect(await screen.findByText("No payments match these filters")).toBeInTheDocument();

    await user.click(screen.getByRole("combobox", { name: "App" }));
    await user.click(await screen.findByRole("option", { name: "Globex" }));
    await waitFor(() => expect(lastQuery(api).get("tenant_id")).toBe("2"));
    expect(lastQuery(api).get("status")).toBe("FAILED");

    await user.click(within(screen.getByTestId("empty-state")).getByRole("button", { name: "Clear filters" }));
    // The unfiltered page is still cached, so it shows without a new request.
    expect(await screen.findByText("Rp150.000")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Failed" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("combobox", { name: "App" })).toHaveTextContent("All apps");
  });

  it("guides to apps when there are no payments", async () => {
    mockApi({ "GET /admin/tenants": { tenants: [] }, "GET /admin/payments": { payments: [], total: 0 } });
    renderApp("/payments");
    expect(await screen.findByText("No payments yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View apps" })).toHaveAttribute("href", "/apps");
  });
});
