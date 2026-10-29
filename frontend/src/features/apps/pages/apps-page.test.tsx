import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import { app, APP_ID, me, page } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

beforeEach(() => {
  setViewport("desktop");
});

function listQueries(api: ReturnType<typeof mockApi>) {
  return api.to("GET", "/admin/apps").map((r) => new URL(r.url).searchParams);
}

function lastListQuery(api: ReturnType<typeof mockApi>) {
  const queries = listQueries(api);
  return queries[queries.length - 1]!;
}

function chip(group: string, name: string) {
  return within(screen.getByRole("group", { name: `Filter by ${group.toLowerCase()}` })).getByRole("button", { name });
}

const globex = app({
  id: "app_01JBKQ7Z8M4X2Y6V3N9T5R1W0F",
  name: "Globex",
  slug: "globex",
  environment: "live",
  status: "disabled",
});

describe("Apps page", () => {
  it("lists apps with environment, status and id as text", async () => {
    mockApi({ "GET /admin/apps": page([app(), globex]) });
    window.localStorage.setItem(storageKeys.mode, "live");
    const { user } = renderApp("/apps");
    await user.click(await screen.findByRole("button", { name: "All", pressed: false }));
    const table = await screen.findByRole("table", { name: "Apps" });
    const demoRow = within(table).getByText("Demo App").closest("tr")!;
    expect(within(demoRow).getByText("demo")).toBeInTheDocument();
    expect(within(demoRow).getByText("Test")).toHaveAttribute("data-environment", "test");
    expect(within(demoRow).getByText("Active")).toBeInTheDocument();
    expect(within(demoRow).getByText(APP_ID)).toBeInTheDocument();
    const globexRow = within(table).getByText("Globex").closest("tr")!;
    expect(within(globexRow).getByText("Live")).toBeInTheDocument();
    expect(within(globexRow).getByText("Disabled")).toBeInTheDocument();
    expect(within(demoRow).getByRole("link", { name: "Demo App" })).toHaveAttribute("href", `/apps/${APP_ID}`);
  });

  it("opens the app when a row is clicked", async () => {
    mockApi({ "GET /admin/apps": page([app()]), [`GET /admin/apps/${APP_ID}`]: { app: app() } });
    window.localStorage.setItem(storageKeys.mode, "test");
    const { user, router } = renderApp("/apps");
    await screen.findByText("demo");
    // Copying the id stays on the list.
    await user.click(screen.getByRole("button", { name: `Copy App ID ${APP_ID}` }));
    expect(router.state.location.pathname).toBe("/apps");
    await user.click(screen.getByText("demo"));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/apps/${APP_ID}`));
  });

  it("starts the environment chip at the mode and resets it when the mode switch changes", async () => {
    const api = mockApi({ "GET /admin/apps": page([]) });
    const { user } = renderApp("/apps");
    // Live is the default mode.
    await waitFor(() => expect(lastListQuery(api).get("environment")).toBe("live"));
    expect(chip("Environment", "Live")).toHaveAttribute("aria-pressed", "true");

    await user.click(chip("Environment", "All"));
    await waitFor(() => expect(lastListQuery(api).has("environment")).toBe(false));
    expect(chip("Environment", "All")).toHaveAttribute("aria-pressed", "true");

    await user.click(screen.getByRole("switch", { name: "Test mode" }));
    await waitFor(() => expect(lastListQuery(api).get("environment")).toBe("test"));
    expect(chip("Environment", "Test")).toHaveAttribute("aria-pressed", "true");

    // Back to live mode: the chip follows even though the user had picked Live already.
    await user.click(chip("Environment", "All"));
    expect(chip("Environment", "All")).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("switch", { name: "Test mode" }));
    expect(chip("Environment", "Live")).toHaveAttribute("aria-pressed", "true");
    expect(listQueries(api).map((q) => q.get("environment"))).toEqual(["live", null, "test"]);
  });

  it("filters by status", async () => {
    const api = mockApi({ "GET /admin/apps": page([app()]) });
    const { user } = renderApp("/apps");
    await screen.findByText("Demo App");
    expect(lastListQuery(api).has("status")).toBe(false);
    await user.click(chip("Status", "Disabled"));
    await waitFor(() => expect(lastListQuery(api).get("status")).toBe("disabled"));
    expect(chip("Status", "Disabled")).toHaveAttribute("aria-pressed", "true");
    await user.click(chip("Status", "All"));
    expect(chip("Status", "All")).toHaveAttribute("aria-pressed", "true");
    // The unfiltered page is cached, so it shows again without another request.
    expect(await screen.findByText("Demo App")).toBeInTheDocument();
  });

  it("loads the next page with Load more", async () => {
    const api = mockApi({
      "GET /admin/apps": (request: Request) =>
        new URL(request.url).searchParams.get("cursor") === "c1"
          ? json(200, page([globex]))
          : json(200, page([app()], "c1")),
    });
    const { user } = renderApp("/apps");
    await user.click(await screen.findByRole("button", { name: "All", pressed: false }));
    await screen.findByText("Demo App");
    await user.click(screen.getByRole("button", { name: "Load more" }));
    expect(await screen.findByText("Globex")).toBeInTheDocument();
    expect(screen.getByText("Demo App")).toBeInTheDocument();
    expect(lastListQuery(api).get("cursor")).toBe("c1");
    expect(lastListQuery(api).get("limit")).toBe("50");
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("shows the environment empty state, which can show every environment", async () => {
    const api = mockApi({ "GET /admin/apps": page([]) });
    const { user } = renderApp("/apps");
    const empty = await screen.findByTestId("empty-state");
    expect(empty).toHaveTextContent("No live apps");
    expect(within(empty).getByRole("button", { name: "Create app" })).toBeInTheDocument();
    await user.click(within(empty).getByRole("button", { name: "Show all environments" }));
    await waitFor(() => expect(lastListQuery(api).has("environment")).toBe(false));
    expect(await screen.findByText("No apps yet")).toBeInTheDocument();
    expect(within(screen.getByTestId("empty-state")).getByRole("button", { name: "Create app" })).toBeInTheDocument();
  });

  it("shows the filtered empty state for a status filter", async () => {
    mockApi({ "GET /admin/apps": page([]) });
    const { user } = renderApp("/apps");
    await user.click(await screen.findByRole("button", { name: "All", pressed: false }));
    await user.click(chip("Status", "Disabled"));
    expect(await screen.findByText("No apps match these filters")).toBeInTheDocument();
    await user.click(within(screen.getByTestId("empty-state")).getByRole("button", { name: "Clear filters" }));
    expect(await screen.findByText("No apps yet")).toBeInTheDocument();
  });

  it("shows skeleton rows while loading", async () => {
    mockApi({ "GET /admin/apps": () => new Promise<Response>(() => {}) });
    renderApp("/apps");
    expect((await screen.findAllByTestId("skeleton-row")).length).toBeGreaterThan(0);
  });

  it("shows an error with retry", async () => {
    let calls = 0;
    mockApi({
      "GET /admin/apps": () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "internal server error")) : json(200, page([app()]));
      },
    });
    window.localStorage.setItem(storageKeys.mode, "test");
    const { user } = renderApp("/apps");
    const error = await screen.findByTestId("error-state");
    await user.click(within(error).getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Demo App")).toBeInTheDocument();
  });

  it("renders cards at phone width", async () => {
    setViewport("mobile");
    window.localStorage.setItem(storageKeys.mode, "test");
    mockApi({ "GET /admin/apps": page([app()]) });
    renderApp("/apps");
    const list = await screen.findByRole("list", { name: "Apps" });
    expect(within(list).getByRole("link", { name: "Demo App" })).toBeInTheDocument();
    expect(within(list).getByText("Test")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("hides Create app without apps:write", async () => {
    mockApi({ "GET /admin/me": me({ role: "viewer" }), "GET /admin/apps": page([]) });
    renderApp("/apps");
    const empty = await screen.findByTestId("empty-state");
    expect(within(empty).queryByRole("button", { name: "Create app" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create app" })).not.toBeInTheDocument();
  });
});
