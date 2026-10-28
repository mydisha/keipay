import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { setToken } from "@/shared/api/session";
import { errorBody, json, mockApi } from "@/test/http";
import { renderApp, TEST_TOKEN } from "@/test/render";

async function submit(user: ReturnType<typeof renderApp>["user"], current: string, next: string, confirm: string) {
  if (current) await user.type(await screen.findByLabelText("Current password"), current);
  if (next) await user.type(screen.getByLabelText("New password"), next);
  if (confirm) await user.type(screen.getByLabelText("Confirm new password"), confirm);
  await user.click(screen.getByRole("button", { name: "Change password" }));
}

describe("Change password page", () => {
  it("requires a signed-in session", async () => {
    const { router } = renderApp("/change-password");
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
  });

  it("enforces min length, matching confirmation and a different password", async () => {
    setToken(TEST_TOKEN);
    const api = mockApi({});
    const { user } = renderApp("/change-password");
    await submit(user, "oldpassword", "short", "other");
    expect(await screen.findByText("Use at least 8 characters.", { selector: "p[id$=message]" })).toBeInTheDocument();
    expect(screen.getByText("Passwords don't match.")).toBeInTheDocument();
    await user.clear(screen.getByLabelText("New password"));
    await user.type(screen.getByLabelText("New password"), "oldpassword");
    await user.clear(screen.getByLabelText("Confirm new password"));
    await user.type(screen.getByLabelText("Confirm new password"), "oldpassword");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText("Choose a password different from the current one.")).toBeInTheDocument();
    expect(api.requests).toHaveLength(0);
  });

  it("shows a wrong current password on its field", async () => {
    setToken(TEST_TOKEN);
    mockApi({ "POST /admin/change-password": () => json(401, errorBody("invalid_credentials", "invalid credentials")) });
    const { user, router } = renderApp("/change-password");
    await submit(user, "wrongpass", "newpassword1", "newpassword1");
    expect(await screen.findByText("Current password is incorrect.")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/change-password");
  });

  it("changes the password and goes to the dashboard", async () => {
    setToken(TEST_TOKEN);
    const api = mockApi({ "POST /admin/change-password": { message: "password changed" } });
    const { user, router } = renderApp("/change-password");
    await submit(user, "oldpassword", "newpassword1", "newpassword1");
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await api.to("POST", "/admin/change-password")[0]!.json()).toEqual({
      current_password: "oldpassword",
      new_password: "newpassword1",
    });
  });
});
