import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { setToken } from "@/shared/api/session";
import { tenant } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp, TEST_TOKEN } from "@/test/render";

const credentials = { api_key: "kei_new_key", api_secret: "super-secret-value" };

beforeEach(() => {
  setToken(TEST_TOKEN);
  setViewport("desktop");
});

describe("Apps page", () => {
  it("lists apps with status text and API key", async () => {
    mockApi({ "GET /admin/tenants": { tenants: [tenant(), tenant({ id: 2, name: "Globex", slug: "globex", api_key: "kei_globex_key", active: false })] } });
    renderApp("/apps");
    await screen.findByText("Demo App");
    const table = screen.getByRole("table", { name: "Apps" });
    expect(within(table).getByText("kei_demo_key_0001")).toBeInTheDocument();
    expect(within(table).getByText("Active")).toBeInTheDocument();
    expect(within(table).getByText("Inactive")).toBeInTheDocument();
  });

  it("shows the empty state with a create action", async () => {
    mockApi({ "GET /admin/tenants": { tenants: [] } });
    renderApp("/apps");
    expect(await screen.findByText("No apps yet")).toBeInTheDocument();
    expect(within(screen.getByTestId("empty-state")).getByRole("button", { name: "Create app" })).toBeInTheDocument();
  });

  it("shows an error with retry", async () => {
    let calls = 0;
    mockApi({
      "GET /admin/tenants": () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "internal server error")) : json(200, { tenants: [tenant()] });
      },
    });
    const { user } = renderApp("/apps");
    const error = await screen.findByTestId("error-state");
    await user.click(within(error).getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Demo App")).toBeInTheDocument();
  });

  it("creates an app and shows the secret once", async () => {
    const api = mockApi({
      "GET /admin/tenants": { tenants: [] },
      "POST /admin/tenants": () => json(201, { tenant: tenant({ name: "Acme", slug: "acme" }), credentials }),
    });
    const { user } = renderApp("/apps");
    await user.click((await screen.findAllByRole("button", { name: "Create app" }))[0]!);
    const dialog = await screen.findByRole("dialog", { name: "Create app" });
    await user.type(within(dialog).getByLabelText("Name"), "Acme");
    await user.type(within(dialog).getByLabelText("Slug"), "Bad Slug");
    await user.type(within(dialog).getByLabelText("Webhook URL"), "ftp://nope");
    await user.click(within(dialog).getByRole("button", { name: "Create app" }));
    expect(await within(dialog).findByText(/Use 3-32 lowercase letters/)).toBeInTheDocument();
    expect(within(dialog).getByText(/Enter a full URL/)).toBeInTheDocument();
    expect(api.to("POST", "/admin/tenants")).toHaveLength(0);

    await user.clear(within(dialog).getByLabelText("Slug"));
    await user.type(within(dialog).getByLabelText("Slug"), "acme");
    await user.clear(within(dialog).getByLabelText("Webhook URL"));
    await user.type(within(dialog).getByLabelText("Webhook URL"), "https://acme.example.com/hook");
    await user.click(within(dialog).getByRole("button", { name: "Create app" }));

    const secret = await screen.findByRole("dialog", { name: "Save these credentials" });
    expect(within(secret).getByText("super-secret-value")).toBeInTheDocument();
    expect(await api.to("POST", "/admin/tenants")[0]!.json()).toEqual({
      name: "Acme",
      slug: "acme",
      webhook_url: "https://acme.example.com/hook",
      redirect_success_url: "",
      redirect_failure_url: "",
    });
    expect(within(secret).getByRole("button", { name: "Done" })).toBeDisabled();
    await user.click(within(secret).getByRole("checkbox", { name: "I have saved this" }));
    await user.click(within(secret).getByRole("button", { name: "Done" }));
    await waitFor(() => expect(screen.queryByText("super-secret-value")).not.toBeInTheDocument());
  });

  it("puts a duplicate slug error on the slug field", async () => {
    mockApi({
      "GET /admin/tenants": { tenants: [] },
      "POST /admin/tenants": () => json(409, errorBody("conflict", "conflict")),
    });
    const { user } = renderApp("/apps");
    await user.click((await screen.findAllByRole("button", { name: "Create app" }))[0]!);
    const dialog = await screen.findByRole("dialog", { name: "Create app" });
    await user.type(within(dialog).getByLabelText("Name"), "Demo");
    await user.type(within(dialog).getByLabelText("Slug"), "demo");
    await user.click(within(dialog).getByRole("button", { name: "Create app" }));
    expect(await within(dialog).findByText("This slug is already in use.")).toBeInTheDocument();
  });

  it("rotates credentials only after typing the slug", async () => {
    const api = mockApi({
      "GET /admin/tenants": { tenants: [tenant()] },
      "POST /admin/tenants/1/rotate": { tenant: tenant(), credentials },
    });
    const { user } = renderApp("/apps");
    await user.click(await screen.findByRole("button", { name: "Actions for Demo App" }));
    await user.click(await screen.findByRole("menuitem", { name: "Rotate credentials" }));
    const confirm = await screen.findByRole("dialog", { name: "Rotate credentials for Demo App?" });
    const button = within(confirm).getByRole("button", { name: "Rotate credentials" });
    expect(button).toBeDisabled();
    await user.type(within(confirm).getByRole("textbox", { name: "Type demo to confirm" }), "demo");
    await user.click(button);
    expect(await screen.findByRole("dialog", { name: "Save these credentials" })).toHaveTextContent("super-secret-value");
    expect(api.to("POST", "/admin/tenants/1/rotate")).toHaveLength(1);
  });

  it("deactivates after confirmation, sending every field", async () => {
    const api = mockApi({
      "GET /admin/tenants": { tenants: [tenant()] },
      "PUT /admin/tenants/1": { tenant: tenant({ active: false }) },
    });
    const { user } = renderApp("/apps");
    await user.click(await screen.findByRole("button", { name: "Actions for Demo App" }));
    await user.click(await screen.findByRole("menuitem", { name: "Deactivate" }));
    const confirm = await screen.findByRole("dialog", { name: "Deactivate Demo App?" });
    await user.click(within(confirm).getByRole("button", { name: "Deactivate" }));
    await waitFor(() => expect(api.to("PUT", "/admin/tenants/1")).toHaveLength(1));
    expect(await api.to("PUT", "/admin/tenants/1")[0]!.json()).toEqual({
      name: "Demo App",
      webhook_url: "https://demo.example.com/webhooks/kei",
      redirect_success_url: "",
      redirect_failure_url: "",
      active: false,
    });
  });

  it("activates an inactive app without a confirmation", async () => {
    const api = mockApi({
      "GET /admin/tenants": { tenants: [tenant({ active: false })] },
      "PUT /admin/tenants/1": { tenant: tenant() },
    });
    const { user } = renderApp("/apps");
    await user.click(await screen.findByRole("button", { name: "Actions for Demo App" }));
    await user.click(await screen.findByRole("menuitem", { name: "Activate" }));
    await waitFor(() => expect(api.to("PUT", "/admin/tenants/1")).toHaveLength(1));
    expect((await api.to("PUT", "/admin/tenants/1")[0]!.json()).active).toBe(true);
  });

  it("edits an app", async () => {
    const api = mockApi({
      "GET /admin/tenants": { tenants: [tenant()] },
      "PUT /admin/tenants/1": { tenant: tenant({ name: "Demo Renamed" }) },
    });
    const { user } = renderApp("/apps");
    await user.click(await screen.findByRole("button", { name: "Actions for Demo App" }));
    await user.click(await screen.findByRole("menuitem", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Demo App" });
    const name = within(dialog).getByLabelText("Name");
    await user.clear(name);
    await user.type(name, "Demo Renamed");
    await user.click(within(dialog).getByRole("checkbox", { name: "Active" }));
    await user.click(within(dialog).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(await api.to("PUT", "/admin/tenants/1")[0]!.json()).toMatchObject({ name: "Demo Renamed", active: false });
  });

  it("renders cards at phone width", async () => {
    setViewport("mobile");
    mockApi({ "GET /admin/tenants": { tenants: [tenant()] } });
    renderApp("/apps");
    const list = await screen.findByRole("list", { name: "Apps" });
    expect(within(list).getByText("Demo App")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
});
