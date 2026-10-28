import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { app, APP_ID, delivery, page, PAY_ID } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

beforeEach(() => {
  setViewport("desktop");
});

describe("Webhooks page", () => {
  it("lists outbound deliveries and replays one with feedback", async () => {
    const api = mockApi({
      "GET /admin/apps": page([app()]),
      "GET /admin/webhook-deliveries": { deliveries: [delivery()] },
      "POST /admin/webhook-deliveries/7/replay": () => json(202, { requeued: true }),
    });
    const { user } = renderApp("/webhooks");
    expect(await screen.findByRole("heading", { level: 1, name: "Webhooks" })).toBeInTheDocument();
    expect(await screen.findByText("connection refused")).toBeInTheDocument();
    expect(screen.getByText("Failed", { selector: "[data-tone]" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Replay delivery 7" }));
    expect(await screen.findByText("Delivery 7 queued for another attempt")).toBeInTheDocument();
    expect(api.to("POST", "/admin/webhook-deliveries/7/replay")).toHaveLength(1);
  });

  it("shows the app from the row's summary and the pay_ payment id (T-47)", async () => {
    mockApi({ "GET /admin/apps": page([]), "GET /admin/webhook-deliveries": { deliveries: [delivery()] } });
    renderApp("/webhooks");
    await screen.findByText("connection refused");
    const table = screen.getByRole("table", { name: "Outbound" });
    expect(within(table).getByRole("link", { name: "Demo App" })).toHaveAttribute("href", `/apps/${APP_ID}`);
    expect(within(table).getByText("Test")).toBeInTheDocument();
    expect(within(table).getByText(PAY_ID)).toBeInTheDocument();
  });

  it("filters by app public id (T-47)", async () => {
    const api = mockApi({
      "GET /admin/apps": page([app()]),
      "GET /admin/webhook-deliveries": { deliveries: [delivery()] },
    });
    const { user } = renderApp("/webhooks");
    await screen.findByText("connection refused");
    await user.click(screen.getByRole("combobox", { name: "App" }));
    await user.click(await screen.findByRole("option", { name: "Demo App (Test)" }));
    await waitFor(() => {
      const requests = api.to("GET", "/admin/webhook-deliveries");
      expect(new URL(requests[requests.length - 1]!.url).searchParams.get("app_id")).toBe(APP_ID);
    });
  });

  it("shows a toast when replay fails", async () => {
    mockApi({
      "GET /admin/apps": page([]),
      "GET /admin/webhook-deliveries": { deliveries: [delivery()] },
      "POST /admin/webhook-deliveries/7/replay": () => json(404, errorBody("not_found", "not found")),
    });
    const { user } = renderApp("/webhooks");
    await user.click(await screen.findByRole("button", { name: "Replay delivery 7" }));
    expect(await screen.findByText("Not found.")).toBeInTheDocument();
  });

  it("filters by delivery status", async () => {
    const api = mockApi({
      "GET /admin/apps": page([]),
      "GET /admin/webhook-deliveries": { deliveries: [] },
    });
    const { user } = renderApp("/webhooks");
    expect(await screen.findByText("No deliveries yet")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Delivered" }));
    await waitFor(() => {
      const requests = api.to("GET", "/admin/webhook-deliveries");
      expect(new URL(requests[requests.length - 1]!.url).searchParams.get("status")).toBe("DELIVERED");
    });
    expect(await screen.findByText("No deliveries match these filters")).toBeInTheDocument();
  });

  it("shows inbound as coming soon", async () => {
    mockApi({ "GET /admin/apps": page([]), "GET /admin/webhook-deliveries": { deliveries: [] } });
    const { user } = renderApp("/webhooks");
    await user.click(await screen.findByRole("tab", { name: "Inbound" }));
    const panel = screen.getByRole("tabpanel");
    expect(within(panel).getByText("Inbound webhook log is coming soon")).toBeInTheDocument();
  });
});
