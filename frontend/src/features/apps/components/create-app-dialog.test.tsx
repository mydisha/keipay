import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import { app, APP_ID, appCreated, page } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

// T-41: the create app dialog and the one-time secrets.

beforeEach(() => {
  setViewport("desktop");
});

async function openDialog(path = "/apps") {
  const rendered = renderApp(path);
  await rendered.user.click((await screen.findAllByRole("button", { name: "Create app" }))[0]!);
  const dialog = await screen.findByRole("dialog", { name: "Create app" });
  return { ...rendered, dialog };
}

describe("Create app dialog (T-41)", () => {
  it("suggests the slug from the name until the slug is edited", async () => {
    mockApi({ "GET /admin/apps": page([]) });
    const { user, dialog } = await openDialog();
    const name = within(dialog).getByLabelText("Name");
    const slug = within(dialog).getByLabelText("Slug");
    await user.type(name, "Acme Store!");
    expect(slug).toHaveValue("acme-store");
    await user.clear(slug);
    await user.type(slug, "shop");
    await user.type(name, " Two");
    expect(slug).toHaveValue("shop");
    expect(within(dialog).getByText("Lowercase letters, digits and dashes. Cannot be changed later.")).toBeInTheDocument();
  });

  it("defaults the environment to the mode", async () => {
    window.localStorage.setItem(storageKeys.mode, "test");
    mockApi({ "GET /admin/apps": page([]) });
    const { dialog } = await openDialog();
    expect(within(dialog).getByRole("radio", { name: "Test" })).toBeChecked();
    expect(within(dialog).getByRole("radio", { name: "Live" })).not.toBeChecked();
    expect(within(dialog).getByRole("radiogroup", { name: "Environment" })).toBeInTheDocument();
  });

  it("validates slug, URLs and metadata before sending", async () => {
    const api = mockApi({ "GET /admin/apps": page([]) });
    const { user, dialog } = await openDialog();
    await user.click(within(dialog).getByRole("button", { name: "Create app" }));
    expect(await within(dialog).findByText("Enter a name.")).toBeInTheDocument();

    await user.type(within(dialog).getByLabelText("Name"), "Acme");
    await user.clear(within(dialog).getByLabelText("Slug"));
    await user.type(within(dialog).getByLabelText("Slug"), "-bad");
    await user.type(within(dialog).getByLabelText("Webhook URL"), "ftp://nope");
    await user.type(within(dialog).getByLabelText("Success redirect URL"), "/relative");
    await user.type(within(dialog).getByLabelText("Metadata (optional)"), "[[1]");
    await user.click(within(dialog).getByRole("button", { name: "Create app" }));

    expect(await within(dialog).findByText(/Use 3-32 lowercase letters/)).toBeInTheDocument();
    expect(within(dialog).getAllByText(/Enter a full URL/)).toHaveLength(2);
    expect(within(dialog).getByText(/Enter a JSON object/)).toBeInTheDocument();
    expect(api.to("POST", "/admin/apps")).toHaveLength(0);
  });

  it("maps server field errors onto their fields", async () => {
    mockApi({
      "GET /admin/apps": page([]),
      "POST /admin/apps": () =>
        json(
          400,
          errorBody("invalid_request", "invalid request", {
            fields: [
              { field: "slug", rule: "slug" },
              { field: "webhook_url", rule: "url" },
              { field: "metadata.tier", rule: "max" },
            ],
          }),
        ),
    });
    const { user, dialog } = await openDialog();
    await user.type(within(dialog).getByLabelText("Name"), "Acme");
    await user.type(within(dialog).getByLabelText("Webhook URL"), "https://acme.example.com/hook");
    await user.type(within(dialog).getByLabelText("Metadata (optional)"), '{{"tier": "gold"}');
    await user.click(within(dialog).getByRole("button", { name: "Create app" }));
    expect(await within(dialog).findByText(/Use 3-32 lowercase letters/)).toBeInTheDocument();
    expect(within(dialog).getByText(/Enter a full URL/)).toBeInTheDocument();
    expect(within(dialog).getByText('The value of "tier" is longer than 500 characters.')).toBeInTheDocument();
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("puts a slug conflict on the slug field, naming the environment", async () => {
    mockApi({
      "GET /admin/apps": page([]),
      "POST /admin/apps": () => json(409, errorBody("conflict", "conflict")),
    });
    const { user, dialog } = await openDialog();
    await user.type(within(dialog).getByLabelText("Name"), "Demo");
    await user.click(within(dialog).getByRole("radio", { name: "Test" }));
    await user.click(within(dialog).getByRole("button", { name: "Create app" }));
    expect(await within(dialog).findByText("This slug is already used in the test environment.")).toBeInTheDocument();
  });

  it("shows the three secrets once, then opens the new app", async () => {
    const created = appCreated({ name: "Acme", slug: "acme", environment: "live" });
    const api = mockApi({
      "GET /admin/apps": page([]),
      "POST /admin/apps": () => json(201, created),
      [`GET /admin/apps/${APP_ID}`]: { app: app({ name: "Acme", slug: "acme", environment: "live" }) },
    });
    const { user, dialog, router } = await openDialog();
    await user.type(within(dialog).getByLabelText("Name"), "Acme");
    await user.type(within(dialog).getByLabelText("Webhook URL"), "https://acme.example.com/hook");
    // Integers past 2^53 must reach the server digit for digit.
    await user.type(within(dialog).getByLabelText("Metadata (optional)"), '{{"order_seq": 12345678901234567890}');
    await user.click(within(dialog).getByRole("button", { name: "Create app" }));

    const secret = await screen.findByRole("dialog", { name: "Save these credentials" });
    for (const [label, value] of [
      ["API key", created.api_key],
      ["API secret", created.api_secret],
      ["Webhook signing secret", created.webhook_secret],
    ]) {
      expect(within(secret).getByText(label!)).toBeInTheDocument();
      expect(within(secret).getByText(value!)).toBeInTheDocument();
      expect(within(secret).getByRole("button", { name: `Copy ${label}` })).toBeInTheDocument();
    }
    expect(within(secret).getByRole("note")).toHaveTextContent("only time");

    const sent = await api.to("POST", "/admin/apps")[0]!.text();
    expect(sent).toContain('"order_seq": 12345678901234567890');
    expect(JSON.parse(sent)).toMatchObject({
      name: "Acme",
      slug: "acme",
      environment: "live",
      webhook_url: "https://acme.example.com/hook",
      redirect_success_url: "",
      redirect_failure_url: "",
    });

    const done = within(secret).getByRole("button", { name: "Done" });
    expect(done).toBeDisabled();
    await user.click(within(secret).getByRole("checkbox", { name: "I have saved this" }));
    await user.click(done);
    await waitFor(() => expect(router.state.location.pathname).toBe(`/apps/${APP_ID}`));
    expect(screen.queryByText(created.api_secret)).not.toBeInTheDocument();
    expect(await screen.findByRole("heading", { level: 1, name: "Acme" })).toBeInTheDocument();
  });

  it("sends no metadata when the editor is empty", async () => {
    const api = mockApi({
      "GET /admin/apps": page([]),
      "POST /admin/apps": () => json(201, appCreated()),
    });
    const { user, dialog } = await openDialog();
    await user.type(within(dialog).getByLabelText("Name"), "Demo");
    await user.click(within(dialog).getByRole("button", { name: "Create app" }));
    await screen.findByRole("dialog", { name: "Save these credentials" });
    expect(await api.to("POST", "/admin/apps")[0]!.json()).not.toHaveProperty("metadata");
  });
});
