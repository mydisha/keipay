import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import { CON_ID, connectionListItem, me, page, provider } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

// T-52: the Connections list.

const LIVE_ID = "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0H";
const providers = { data: [provider(), provider({ name: "acmepay", display_name: "Acme Pay" })] };

beforeEach(() => {
  setViewport("desktop");
});

function listQueries(api: ReturnType<typeof mockApi>) {
  return api.to("GET", "/admin/connections").map((r) => new URL(r.url).searchParams);
}

function lastListQuery(api: ReturnType<typeof mockApi>) {
  const queries = listQueries(api);
  return queries[queries.length - 1]!;
}

function chip(group: string, name: string) {
  return within(screen.getByRole("group", { name: `Filter by ${group.toLowerCase()}` })).getByRole("button", { name });
}

const imported = connectionListItem({
  id: LIVE_ID,
  name: "xendit-imported",
  environment: "live",
  legacy_webhook_alias: true,
  last_test_status: "failed",
  last_tested_at: "2026-01-01T00:00:00Z",
});

describe("Connections page (T-52)", () => {
  it("lists connections with text statuses and the last test", async () => {
    mockApi({
      "GET /admin/providers": providers,
      "GET /admin/connections": page([
        connectionListItem(),
        imported,
        connectionListItem({
          id: "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0J",
          name: "acme-test",
          provider: "acmepay",
          status: "disabled",
          last_test_status: "ok",
          last_tested_at: "2026-01-01T00:00:00Z",
        }),
      ]),
    });
    window.localStorage.setItem(storageKeys.mode, "test");
    const { user } = renderApp("/connections");
    await user.click(await screen.findByRole("button", { name: "All", pressed: false }));
    const table = await screen.findByRole("table", { name: "Connections" });

    const first = within(table).getByRole("link", { name: "xendit-test" }).closest("tr")!;
    expect(within(first).getByRole("link", { name: "xendit-test" })).toHaveAttribute("href", `/connections/${CON_ID}`);
    expect(within(first).getByText(CON_ID)).toBeInTheDocument();
    expect(within(first).getByText("Xendit")).toBeInTheDocument();
    expect(within(first).getByText("Test")).toHaveAttribute("data-environment", "test");
    expect(within(first).getByText("Active")).toBeInTheDocument();
    expect(within(first).getByText("Not tested")).toBeInTheDocument();

    const legacy = within(table).getByRole("link", { name: "xendit-imported" }).closest("tr")!;
    expect(within(legacy).getByText("Legacy URL")).toBeInTheDocument();
    expect(within(legacy).getByText("Live")).toBeInTheDocument();
    expect(within(legacy).getByText("Failed")).toBeInTheDocument();
    expect(within(legacy).getAllByRole("time").length).toBeGreaterThan(0);

    const acme = within(table).getByRole("link", { name: "acme-test" }).closest("tr")!;
    expect(within(acme).getByText("Acme Pay")).toBeInTheDocument();
    expect(within(acme).getByText("Disabled")).toBeInTheDocument();
    expect(within(acme).getByText("Passed")).toBeInTheDocument();
  });

  it("falls back to the raw provider name until providers load", async () => {
    mockApi({
      "GET /admin/providers": () => new Promise<Response>(() => {}),
      "GET /admin/connections": page([connectionListItem()]),
    });
    window.localStorage.setItem(storageKeys.mode, "test");
    renderApp("/connections");
    const row = (await screen.findByRole("link", { name: "xendit-test" })).closest("tr")!;
    expect(within(row).getByText("xendit")).toBeInTheDocument();
  });

  it("starts the environment chip at the mode and filters by status and provider", async () => {
    const api = mockApi({ "GET /admin/providers": providers, "GET /admin/connections": page([connectionListItem()]) });
    window.localStorage.setItem(storageKeys.mode, "test");
    const { user } = renderApp("/connections");
    await waitFor(() => expect(lastListQuery(api).get("environment")).toBe("test"));
    expect(chip("Environment", "Test")).toHaveAttribute("aria-pressed", "true");

    await user.click(chip("Status", "Disabled"));
    await waitFor(() => expect(lastListQuery(api).get("status")).toBe("disabled"));
    await user.click(chip("Provider", "Acme Pay"));
    await waitFor(() => expect(lastListQuery(api).get("provider")).toBe("acmepay"));
    expect(lastListQuery(api).get("environment")).toBe("test");
    expect(lastListQuery(api).get("limit")).toBe("50");

    await user.click(screen.getByRole("switch", { name: "Test mode" }));
    await waitFor(() => expect(lastListQuery(api).get("environment")).toBe("live"));
    expect(chip("Environment", "Live")).toHaveAttribute("aria-pressed", "true");
  });

  it("loads the next page with Load more", async () => {
    const api = mockApi({
      "GET /admin/providers": providers,
      "GET /admin/connections": (request: Request) =>
        new URL(request.url).searchParams.get("cursor") === "c1"
          ? json(200, page([imported]))
          : json(200, page([connectionListItem()], "c1")),
    });
    const { user } = renderApp("/connections");
    await user.click(await screen.findByRole("button", { name: "All", pressed: false }));
    await screen.findByText("xendit-test");
    await user.click(screen.getByRole("button", { name: "Load more" }));
    expect(await screen.findByText("xendit-imported")).toBeInTheDocument();
    expect(lastListQuery(api).get("cursor")).toBe("c1");
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("shows an empty state with Add connection for writers", async () => {
    mockApi({ "GET /admin/providers": providers, "GET /admin/connections": page([]) });
    const { user } = renderApp("/connections");
    const empty = await screen.findByTestId("empty-state");
    expect(empty).toHaveTextContent("No live connections");
    await user.click(within(empty).getByRole("button", { name: "Show all environments" }));
    const all = await screen.findByText("No connections yet");
    const allEmpty = all.closest("[data-testid=empty-state]") as HTMLElement;
    expect(allEmpty).toHaveTextContent("Add a gateway account");
    expect(within(allEmpty).getByRole("link", { name: "Add connection" })).toHaveAttribute("href", "/connections/new");
    expect(screen.getAllByRole("link", { name: "Add connection" })).toHaveLength(2);
  });

  it("links Add connection to the filtered environment", async () => {
    mockApi({ "GET /admin/providers": providers, "GET /admin/connections": page([]) });
    window.localStorage.setItem(storageKeys.mode, "test");
    renderApp("/connections");
    const empty = await screen.findByTestId("empty-state");
    expect(within(empty).getByRole("link", { name: "Add connection" })).toHaveAttribute(
      "href",
      "/connections/new?environment=test",
    );
  });

  it("shows text only to viewers when empty", async () => {
    mockApi({
      "GET /admin/me": me({ role: "viewer" }),
      "GET /admin/providers": providers,
      "GET /admin/connections": page([]),
    });
    const { user } = renderApp("/connections");
    await user.click(await screen.findByRole("button", { name: "All", pressed: false }));
    const empty = await screen.findByText("No connections yet");
    expect(empty.closest("[data-testid=empty-state]")).toHaveTextContent("once an owner or admin adds them");
    expect(screen.queryByRole("link", { name: "Add connection" })).not.toBeInTheDocument();
  });

  it("shows the filtered empty state with Clear filters", async () => {
    mockApi({ "GET /admin/providers": providers, "GET /admin/connections": page([]) });
    const { user } = renderApp("/connections");
    await screen.findByTestId("empty-state");
    await user.click(chip("Status", "Disabled"));
    expect(await screen.findByText("No connections match these filters")).toBeInTheDocument();
    await user.click(within(screen.getByTestId("empty-state")).getByRole("button", { name: "Clear filters" }));
    expect(await screen.findByText("No connections yet")).toBeInTheDocument();
    expect(chip("Environment", "All")).toHaveAttribute("aria-pressed", "true");
  });

  it("shows skeleton rows while loading", async () => {
    mockApi({ "GET /admin/providers": providers, "GET /admin/connections": () => new Promise<Response>(() => {}) });
    renderApp("/connections");
    expect((await screen.findAllByTestId("skeleton-row")).length).toBeGreaterThan(0);
  });

  it("shows an error with retry", async () => {
    let calls = 0;
    mockApi({
      "GET /admin/providers": providers,
      "GET /admin/connections": () => {
        calls += 1;
        return calls === 1
          ? json(500, errorBody("internal_error", "internal server error"))
          : json(200, page([connectionListItem({ environment: "live" })]));
      },
    });
    const { user } = renderApp("/connections");
    const error = await screen.findByTestId("error-state");
    await user.click(within(error).getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("xendit-test")).toBeInTheDocument();
  });

  it("shows ForbiddenState on 403", async () => {
    mockApi({
      "GET /admin/providers": providers,
      "GET /admin/connections": () => json(403, errorBody("forbidden", "forbidden")),
    });
    renderApp("/connections");
    expect(await screen.findByTestId("forbidden-state")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("renders cards at phone width", async () => {
    setViewport("mobile");
    window.localStorage.setItem(storageKeys.mode, "test");
    mockApi({ "GET /admin/providers": providers, "GET /admin/connections": page([connectionListItem()]) });
    renderApp("/connections");
    const list = await screen.findByRole("list", { name: "Connections" });
    expect(within(list).getByRole("link", { name: "xendit-test" })).toBeInTheDocument();
    expect(within(list).getByText("Not tested")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
});
