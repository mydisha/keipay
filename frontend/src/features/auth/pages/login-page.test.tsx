import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { getToken } from "@/shared/api/session";
import { errorBody, json, mockApi } from "@/test/http";
import { renderApp } from "@/test/render";

async function fillAndSubmit(user: ReturnType<typeof renderApp>["user"], username: string, password: string) {
  const usernameInput = await screen.findByLabelText("Username");
  if (username) await user.type(usernameInput, username);
  if (password) await user.type(screen.getByLabelText("Password"), password);
  await user.click(screen.getByRole("button", { name: "Sign in" }));
}

describe("Login page (T-28)", () => {
  it("validates required fields before calling the API", async () => {
    const api = mockApi({});
    const { user } = renderApp("/login");
    await fillAndSubmit(user, "", "");
    expect(await screen.findByText("Enter your username.")).toBeInTheDocument();
    expect(screen.getByText("Enter your password.")).toBeInTheDocument();
    expect(screen.getByLabelText("Username")).toHaveAttribute("aria-invalid", "true");
    expect(api.requests).toHaveLength(0);
  });

  it("shows 'Invalid username or password' for invalid_credentials", async () => {
    mockApi({ "POST /admin/login": () => json(401, errorBody("invalid_credentials", "invalid credentials")) });
    const { user } = renderApp("/login");
    await fillAndSubmit(user, "admin", "wrong");
    expect(await screen.findByRole("alert")).toHaveTextContent("Invalid username or password");
    expect(getToken()).toBeNull();
  });

  it("shows a generic message for a server error", async () => {
    mockApi({ "POST /admin/login": () => json(500, errorBody("internal_error", "internal server error")) });
    const { user } = renderApp("/login");
    await fillAndSubmit(user, "admin", "pw");
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't sign in. Try again.");
  });

  it("sends the credentials, stores the token and redirects to change-password when required", async () => {
    const api = mockApi({ "POST /admin/login": { token: "tok", must_change_password: true } });
    const { user, router } = renderApp("/login");
    await fillAndSubmit(user, " admin ", "secret");
    await waitFor(() => expect(router.state.location.pathname).toBe("/change-password"));
    expect(getToken()).toBe("tok");
    expect(await api.to("POST", "/admin/login")[0]!.json()).toEqual({ username: "admin", password: "secret" });
  });

  it("redirects to the dashboard otherwise", async () => {
    mockApi({ "POST /admin/login": { token: "tok", must_change_password: false } });
    const { user, router } = renderApp("/login");
    await fillAndSubmit(user, "admin", "secret");
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument();
  });

  it("explains an expired session", async () => {
    renderApp("/login?reason=session_expired");
    expect(await screen.findByRole("alert")).toHaveTextContent("Your session has expired. Sign in again.");
  });
});
