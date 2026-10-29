import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { app, APP_ID, appSummary, page, payment, PAY_ID } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

// Payments use the cursor list, `pay_` ids and the app summary.

const GLOBEX_ID = "app_01JBKQ7Z8M4X2Y6V3N9T5R1W0F";
const globex = app({ id: GLOBEX_ID, name: "Globex", slug: "globex", environment: "live" });

beforeEach(() => {
  setViewport("desktop");
});

function lastQuery(api: ReturnType<typeof mockApi>) {
  const requests = api.to("GET", "/admin/payments");
  return new URL(requests[requests.length - 1]!.url).searchParams;
}

describe("Payments page", () => {
  it("shows pay_ ids, the app name and environment, money and status text", async () => {
    mockApi({
      "GET /admin/apps": page([app()]),
      "GET /admin/payments": page([payment()]),
    });
    renderApp("/payments");
    expect(await screen.findByText("Rp150.000")).toBeInTheDocument();
    const table = screen.getByRole("table", { name: "Payments" });
    expect(within(table).getByText(PAY_ID)).toBeInTheDocument();
    expect(within(table).getByRole("link", { name: "Demo App" })).toHaveAttribute("href", `/apps/${APP_ID}`);
    expect(within(table).getByText("Test")).toBeInTheDocument();
    expect(within(table).getByText("Paid")).toBeInTheDocument();
    expect(within(table).getByText("order-001")).toBeInTheDocument();
    expect(screen.queryByText(/Showing \d+ of/)).not.toBeInTheDocument();
  });

  it("filters by status chip and by app public id, and clears filters", async () => {
    const api = mockApi({
      "GET /admin/apps": page([app(), globex]),
      "GET /admin/payments": (request: Request) => {
        const status = new URL(request.url).searchParams.get("status");
        return json(200, status === "FAILED" ? page([]) : page([payment()]));
      },
    });
    const { user } = renderApp("/payments");
    await screen.findByText("Rp150.000");
    expect(lastQuery(api).has("status")).toBe(false);
    expect(lastQuery(api).has("app_id")).toBe(false);
    expect(new URL(api.to("GET", "/admin/apps")[0]!.url).searchParams.get("limit")).toBe("200");

    await user.click(screen.getByRole("button", { name: "Failed" }));
    await waitFor(() => expect(lastQuery(api).get("status")).toBe("FAILED"));
    expect(await screen.findByText("No payments match these filters")).toBeInTheDocument();

    await user.click(screen.getByRole("combobox", { name: "App" }));
    await user.click(await screen.findByRole("option", { name: "Globex (Live)" }));
    await waitFor(() => expect(lastQuery(api).get("app_id")).toBe(GLOBEX_ID));
    expect(lastQuery(api).get("status")).toBe("FAILED");

    await user.click(within(screen.getByTestId("empty-state")).getByRole("button", { name: "Clear filters" }));
    expect(await screen.findByText("Rp150.000")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Failed" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("combobox", { name: "App" })).toHaveTextContent("All apps");
  });

  it("loads the next page with Load more", async () => {
    const second = payment({
      id: "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0Z",
      order_id: "order-002",
      app: appSummary({ id: GLOBEX_ID, name: "Globex", slug: "globex", environment: "live" }),
    });
    const api = mockApi({
      "GET /admin/apps": page([]),
      "GET /admin/payments": (request: Request) =>
        new URL(request.url).searchParams.get("cursor") === "p2"
          ? json(200, page([second]))
          : json(200, page([payment()], "p2")),
    });
    const { user } = renderApp("/payments");
    await user.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByText("order-002")).toBeInTheDocument();
    expect(screen.getByText("order-001")).toBeInTheDocument();
    expect(screen.getByText("Live")).toBeInTheDocument();
    expect(lastQuery(api).get("cursor")).toBe("p2");
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("keeps the rows and shows a toast when the next page fails", async () => {
    mockApi({
      "GET /admin/apps": page([]),
      "GET /admin/payments": (request: Request) =>
        new URL(request.url).searchParams.get("cursor")
          ? json(500, errorBody("internal_error", "boom"))
          : json(200, page([payment()], "p2")),
    });
    const { user } = renderApp("/payments");
    await user.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByText("Couldn't load more. Try again.")).toBeInTheDocument();
    expect(screen.getByText("order-001")).toBeInTheDocument();
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });

  it("guides to apps when there are no payments", async () => {
    mockApi({ "GET /admin/apps": page([]), "GET /admin/payments": page([]) });
    renderApp("/payments");
    expect(await screen.findByText("No payments yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View apps" })).toHaveAttribute("href", "/apps");
  });

  it("shows an error with retry", async () => {
    let calls = 0;
    mockApi({
      "GET /admin/apps": page([]),
      "GET /admin/payments": () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "boom")) : json(200, page([payment()]));
      },
    });
    const { user } = renderApp("/payments");
    await user.click(within(await screen.findByTestId("error-state")).getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Rp150.000")).toBeInTheDocument();
  });
});
