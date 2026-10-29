import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { setTestCookie } from "@/test/cookies";
import { me } from "@/test/fixtures";
import { errorBody, json, mockApi, signedOut } from "@/test/http";
import { renderApp } from "@/test/render";

type User = ReturnType<typeof renderApp>["user"];

async function submit(user: User, current: string, next: string, confirm: string) {
  if (current) await user.type(await screen.findByLabelText("Current password"), current);
  if (next) await user.type(screen.getByLabelText("New password"), next);
  if (confirm) await user.type(screen.getByLabelText("Confirm new password"), confirm);
  await user.click(screen.getByRole("button", { name: "Change password" }));
}

const NEW = "a new long password";

describe("Change password page", () => {
  it("requires a signed-in session", async () => {
    mockApi({ "GET /admin/me": signedOut });
    const { router } = renderApp("/change-password");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
  });

  it("voluntary mode keeps the way back to the console", async () => {
    renderApp("/change-password");
    expect(await screen.findByRole("heading", { level: 1, name: "Change password" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to the console" })).toHaveAttribute("href", "/");
    expect(screen.queryByRole("button", { name: "Sign out" })).not.toBeInTheDocument();
  });

  it("forced mode explains the temporary password and offers only Sign out", async () => {
    setTestCookie("keipay_csrf", "csrf-1");
    let signedIn = true;
    const api = mockApi({
      "GET /admin/me": () => (signedIn ? json(200, me({ must_change_password: true })) : signedOut()),
      "POST /admin/logout": () => {
        signedIn = false;
        return new Response(null, { status: 204 });
      },
    });
    const { user, router } = renderApp("/change-password");
    expect(await screen.findByRole("heading", { level: 1, name: "Set a new password" })).toBeInTheDocument();
    expect(screen.getByText(/An owner set a temporary password/)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Back to the console" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(api.to("POST", "/admin/logout")).toHaveLength(1);
  });

  it("enforces the password rules, matching confirmation and a different password", async () => {
    const api = mockApi({});
    const { user } = renderApp("/change-password");
    await submit(user, "old password!", "short", "other");
    expect(await screen.findByText("Use at least 12 characters.", { selector: "p[id$=message]" })).toBeInTheDocument();
    expect(screen.getByText("Passwords don't match.")).toBeInTheDocument();
    await user.clear(screen.getByLabelText("New password"));
    await user.type(screen.getByLabelText("New password"), "old password!");
    await user.clear(screen.getByLabelText("Confirm new password"));
    await user.type(screen.getByLabelText("Confirm new password"), "old password!");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText("Choose a password different from the current one.")).toBeInTheDocument();
    expect(api.to("POST", "/admin/me/password")).toHaveLength(0);
  });

  it("shows a wrong current password on its field", async () => {
    mockApi({ "POST /admin/me/password": () => json(401, errorBody("invalid_credentials", "invalid credentials")) });
    const { user, router } = renderApp("/change-password");
    await submit(user, "wrong password", NEW, NEW);
    expect(await screen.findByText("Current password is incorrect.")).toBeInTheDocument();
    expect(screen.getByLabelText("Current password")).toHaveAttribute("aria-invalid", "true");
    expect(router.state.location.pathname).toBe("/change-password");
  });

  it("shows validation_failed on the new password", async () => {
    mockApi({ "POST /admin/me/password": () => json(400, errorBody("validation_failed", "new password equals current")) });
    const { user } = renderApp("/change-password");
    await submit(user, "current password", NEW, NEW);
    expect(await screen.findByText("Choose a password different from the current one.")).toBeInTheDocument();
    expect(screen.getByLabelText("New password")).toHaveAttribute("aria-invalid", "true");
  });

  it("changes the password, updates me and goes to the console", async () => {
    setTestCookie("keipay_csrf", "csrf-1");
    const changed = me({ must_change_password: false, name: "After Change" });
    const api = mockApi({
      "GET /admin/me": me({ must_change_password: true }),
      "POST /admin/me/password": () => json(200, changed),
    });
    const { user, router, queryClient } = renderApp("/change-password");
    await submit(user, "temporary-pass-1", NEW, NEW);
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument();
    expect(await screen.findByText("Password changed")).toBeInTheDocument();
    expect(queryClient.getQueryData(["me"])).toEqual(changed);
    const [request] = api.to("POST", "/admin/me/password");
    expect(await request!.json()).toEqual({ current_password: "temporary-pass-1", new_password: NEW });
    expect(request!.headers.get("X-CSRF-Token")).toBe("csrf-1");
  });
});
