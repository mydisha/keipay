import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { setTestCookie } from "@/test/cookies";
import { user as userFixture } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

const self = userFixture();
const viewer = userFixture({
  id: "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0D",
  email: "vera@example.com",
  name: "Vera Viewer",
  role: "viewer",
  must_change_password: true,
  last_login_at: null,
});
const disabledAdmin = userFixture({
  id: "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0E",
  email: "üser@example.com",
  name: "Üser Admin",
  role: "admin",
  status: "disabled",
  disabled_at: "2026-01-02T00:00:00Z",
});

const page = (data: unknown[], nextCursor: string | null = null) => ({ data, next_cursor: nextCursor });
const TEMP = "Abcdefghjkmnpqrstuv2";

beforeEach(() => {
  setViewport("desktop");
  setTestCookie("keipay_csrf", "csrf-1");
});

type User = ReturnType<typeof renderApp>["user"];

async function openActions(user: User, email: string) {
  await user.click(await screen.findByRole("button", { name: `Actions for ${email}` }));
  return screen.findByRole("menu");
}

describe("Users page", () => {
  it("lists users with role and status as text, the own row marked", async () => {
    mockApi({ "GET /admin/users": page([self, viewer, disabledAdmin]) });
    renderApp("/settings/users");
    await screen.findByText(disabledAdmin.email);
    const table = screen.getByRole("table", { name: "Users" });
    const rows = within(table).getAllByRole("row");
    const own = rows.find((r) => r.textContent?.includes("owner@example.com"))!;
    const vera = rows.find((r) => r.textContent?.includes("vera@example.com"))!;
    const disabled = rows.find((r) => r.textContent?.includes("üser@example.com"))!;

    expect(within(own).getByText("You")).toBeInTheDocument();
    expect(within(own).getByText("Owner")).toBeInTheDocument();
    expect(within(own).getByText("Active")).toBeInTheDocument();
    expect(within(vera).queryByText("You")).not.toBeInTheDocument();
    expect(within(vera).getByText("Viewer")).toBeInTheDocument();
    expect(within(vera).getByText("Password change pending")).toBeInTheDocument();
    expect(within(vera).getByText("Never")).toBeInTheDocument();
    expect(within(disabled).getByText("Admin")).toBeInTheDocument();
    expect(within(disabled).getByText("Disabled")).toBeInTheDocument();
  });

  it("offers no Disable or Reset password on your own row", async () => {
    mockApi({ "GET /admin/users": page([self, viewer]) });
    const { user } = renderApp("/settings/users");
    const menu = await openActions(user, self.email);
    expect(within(menu).getByRole("menuitem", { name: "Edit" })).toBeInTheDocument();
    expect(within(menu).queryByRole("menuitem", { name: "Disable" })).not.toBeInTheDocument();
    expect(within(menu).queryByRole("menuitem", { name: "Reset password" })).not.toBeInTheDocument();
    await user.keyboard("{Escape}");

    const other = await openActions(user, viewer.email);
    expect(within(other).getByRole("menuitem", { name: "Disable" })).toBeInTheDocument();
    expect(within(other).getByRole("menuitem", { name: "Reset password" })).toBeInTheDocument();
    expect(within(other).queryByRole("menuitem", { name: "Enable" })).not.toBeInTheDocument();
  });

  it("offers Enable instead of Disable on a disabled user", async () => {
    mockApi({ "GET /admin/users": page([self, disabledAdmin]) });
    const { user } = renderApp("/settings/users");
    const menu = await openActions(user, disabledAdmin.email);
    expect(within(menu).getByRole("menuitem", { name: "Enable" })).toBeInTheDocument();
    expect(within(menu).queryByRole("menuitem", { name: "Disable" })).not.toBeInTheDocument();
  });

  it("shows skeleton rows while loading", async () => {
    mockApi({ "GET /admin/users": () => new Promise<Response>(() => {}) });
    renderApp("/settings/users");
    expect((await screen.findAllByTestId("skeleton-row")).length).toBeGreaterThan(0);
  });

  it("shows the empty state with Add user", async () => {
    mockApi({ "GET /admin/users": page([]) });
    renderApp("/settings/users");
    const empty = await screen.findByTestId("empty-state");
    expect(within(empty).getByText("No users yet")).toBeInTheDocument();
    expect(within(empty).getByRole("button", { name: "Add user" })).toBeInTheDocument();
  });

  it("shows an error with retry", async () => {
    let calls = 0;
    mockApi({
      "GET /admin/users": () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "boom")) : json(200, page([self]));
      },
    });
    const { user } = renderApp("/settings/users");
    const error = await screen.findByTestId("error-state");
    await user.click(within(error).getByRole("button", { name: "Try again" }));
    expect(await screen.findByText(self.email)).toBeInTheDocument();
  });

  it("pages with cursors", async () => {
    const api = mockApi({
      "GET /admin/users": (request: Request) =>
        json(200, new URL(request.url).searchParams.get("cursor") === "c2" ? page([viewer]) : page([self], "c2")),
    });
    const { user } = renderApp("/settings/users");
    await screen.findByText(self.email);
    await user.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText(viewer.email)).toBeInTheDocument();
    expect(new URL(api.to("GET", "/admin/users").at(-1)!.url).searchParams.get("cursor")).toBe("c2");
  });

  it("becomes cards below 768px", async () => {
    setViewport("mobile");
    mockApi({ "GET /admin/users": page([self, viewer]) });
    renderApp("/settings/users");
    const cards = await screen.findByTestId("data-cards");
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(within(cards).getByText("vera@example.com")).toBeInTheDocument();
    expect(within(cards).getByText("Password change pending")).toBeInTheDocument();
    expect(within(cards).getByText("You")).toBeInTheDocument();
  });
});

describe("Users flows", () => {
  it("adds a user and reveals the temporary password once", async () => {
    let created = false;
    const added = userFixture({ id: "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0F", email: "ada@example.com", name: "Ada", role: "admin" });
    const api = mockApi({
      "GET /admin/users": () => json(200, page(created ? [added, self] : [self])),
      "POST /admin/users": () => {
        created = true;
        return json(201, { user: added, temporary_password: TEMP });
      },
    });
    const { user } = renderApp("/settings/users");
    await screen.findByText(self.email);
    await user.click(screen.getByRole("button", { name: "Add user" }));
    const dialog = await screen.findByRole("dialog", { name: "Add user" });
    await user.type(within(dialog).getByLabelText("Email"), "ada@example.com");
    await user.type(within(dialog).getByLabelText("Name"), "Ada");
    await user.click(within(dialog).getByRole("combobox", { name: "Role" }));
    const option = await screen.findByRole("option", { name: /^Admin/ });
    expect(option).toHaveTextContent("No user management");
    await user.click(option);
    await user.click(within(dialog).getByRole("button", { name: "Add user" }));

    const secret = await screen.findByRole("dialog", { name: "Save the temporary password" });
    expect(within(secret).getByText(TEMP)).toBeInTheDocument();
    expect(within(secret).getByText(/ada@example.com/)).toBeInTheDocument();
    const done = within(secret).getByRole("button", { name: "Done" });
    expect(done).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog", { name: "Save the temporary password" })).toBeInTheDocument();
    await user.click(within(secret).getByRole("checkbox", { name: "I have saved this" }));
    await user.click(done);
    await waitFor(() => expect(screen.queryByText(TEMP)).not.toBeInTheDocument());

    expect(await screen.findByText("ada@example.com", { selector: "span" })).toBeInTheDocument();
    const [request] = api.to("POST", "/admin/users");
    expect(await request!.json()).toEqual({ email: "ada@example.com", name: "Ada", role: "admin" });
    expect(request!.headers.get("X-CSRF-Token")).toBe("csrf-1");
  });

  it("shows a duplicate email on the Email field", async () => {
    mockApi({
      "GET /admin/users": page([self]),
      "POST /admin/users": () => json(409, errorBody("conflict", "email already in use")),
    });
    const { user } = renderApp("/settings/users");
    await screen.findByText(self.email);
    await user.click(screen.getByRole("button", { name: "Add user" }));
    const dialog = await screen.findByRole("dialog", { name: "Add user" });
    await user.type(within(dialog).getByLabelText("Email"), "Owner@Example.com");
    await user.type(within(dialog).getByLabelText("Name"), "Again");
    await user.click(within(dialog).getByRole("button", { name: "Add user" }));
    expect(await within(dialog).findByText("This email is already in use.")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Email")).toHaveAttribute("aria-invalid", "true");
  });

  it("edits only the changed fields and explains last_owner", async () => {
    const api = mockApi({
      "GET /admin/users": page([self, viewer]),
      "PATCH /admin/users/usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0C": () =>
        json(409, errorBody("last_owner", "would leave no active owner")),
    });
    const { user } = renderApp("/settings/users");
    const menu = await openActions(user, self.email);
    await user.click(within(menu).getByRole("menuitem", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog", { name: `Edit ${self.name}` });
    expect(within(dialog).getByLabelText("Email")).toHaveValue(self.email);
    await user.click(within(dialog).getByRole("combobox", { name: "Role" }));
    await user.click(await screen.findByRole("option", { name: /^Viewer/ }));
    await user.click(within(dialog).getByRole("button", { name: "Save changes" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("This would leave no active owner.");
    expect(await api.to("PATCH", `/admin/users/${self.id}`)[0]!.json()).toEqual({ role: "viewer" });
  });

  it("closes without a request when nothing changed", async () => {
    const api = mockApi({ "GET /admin/users": page([self, viewer]) });
    const { user } = renderApp("/settings/users");
    const menu = await openActions(user, viewer.email);
    await user.click(within(menu).getByRole("menuitem", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog", { name: `Edit ${viewer.name}` });
    await user.click(within(dialog).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(api.requests.filter((r) => r.method === "PATCH")).toHaveLength(0);
  });

  it("disables only after the exact email is typed (case-sensitive, unicode)", async () => {
    const enabledUnicode = { ...disabledAdmin, status: "active" as const, disabled_at: null };
    const api = mockApi({
      "GET /admin/users": page([self, enabledUnicode]),
      [`POST /admin/users/${disabledAdmin.id}/disable`]: () => json(200, { user: disabledAdmin }),
    });
    const { user } = renderApp("/settings/users");
    const menu = await openActions(user, enabledUnicode.email);
    await user.click(within(menu).getByRole("menuitem", { name: "Disable" }));
    const dialog = await screen.findByRole("dialog", { name: `Disable ${enabledUnicode.name}?` });
    expect(dialog).toHaveTextContent(/signed out everywhere/);
    const confirm = within(dialog).getByRole("button", { name: "Disable" });
    const typed = within(dialog).getByLabelText(`Type ${enabledUnicode.email} to confirm`);
    expect(confirm).toBeDisabled();
    await user.type(typed, "ÜSER@example.com");
    expect(confirm).toBeDisabled();
    await user.clear(typed);
    await user.type(typed, "üser@example.com");
    expect(confirm).toBeEnabled();
    await user.click(confirm);
    expect(await screen.findByText("Üser Admin disabled")).toBeInTheDocument();
    expect(api.to("POST", `/admin/users/${disabledAdmin.id}/disable`)).toHaveLength(1);
  });

  it("shows last_owner on disable as a toast and keeps the dialog", async () => {
    const otherOwner = { ...viewer, role: "owner" as const };
    mockApi({
      "GET /admin/users": page([self, otherOwner]),
      [`POST /admin/users/${viewer.id}/disable`]: () => json(409, errorBody("last_owner", "last owner")),
    });
    const { user } = renderApp("/settings/users");
    const menu = await openActions(user, viewer.email);
    await user.click(within(menu).getByRole("menuitem", { name: "Disable" }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(`Type ${viewer.email} to confirm`), viewer.email);
    await user.click(within(dialog).getByRole("button", { name: "Disable" }));
    expect(await screen.findByText("This would leave no active owner.")).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("resets a password after typing the email and reveals it once", async () => {
    const api = mockApi({
      "GET /admin/users": page([self, viewer]),
      [`POST /admin/users/${viewer.id}/reset-password`]: () => json(200, { user: viewer, temporary_password: TEMP }),
    });
    const { user } = renderApp("/settings/users");
    const menu = await openActions(user, viewer.email);
    await user.click(within(menu).getByRole("menuitem", { name: "Reset password" }));
    const dialog = await screen.findByRole("dialog", { name: `Reset the password for ${viewer.name}?` });
    const confirm = within(dialog).getByRole("button", { name: "Reset password" });
    expect(confirm).toBeDisabled();
    await user.type(within(dialog).getByLabelText(`Type ${viewer.email} to confirm`), viewer.email);
    await user.click(confirm);
    const secret = await screen.findByRole("dialog", { name: "Save the temporary password" });
    expect(within(secret).getByText(TEMP)).toBeInTheDocument();
    expect(within(secret).getByRole("button", { name: "Done" })).toBeDisabled();
    expect(api.to("POST", `/admin/users/${viewer.id}/reset-password`)).toHaveLength(1);
  });

  it("enables with a plain confirmation", async () => {
    const api = mockApi({
      "GET /admin/users": page([self, disabledAdmin]),
      [`POST /admin/users/${disabledAdmin.id}/enable`]: () => json(200, { user: { ...disabledAdmin, status: "active" } }),
    });
    const { user } = renderApp("/settings/users");
    const menu = await openActions(user, disabledAdmin.email);
    await user.click(within(menu).getByRole("menuitem", { name: "Enable" }));
    const dialog = await screen.findByRole("dialog", { name: `Enable ${disabledAdmin.name}?` });
    expect(within(dialog).queryByRole("textbox")).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Enable" }));
    expect(await screen.findByText("Üser Admin enabled")).toBeInTheDocument();
    expect(api.to("POST", `/admin/users/${disabledAdmin.id}/enable`)).toHaveLength(1);
  });
});
