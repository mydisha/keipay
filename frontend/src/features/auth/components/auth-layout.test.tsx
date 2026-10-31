import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import { json, mockApi, signedOut } from "@/test/http";
import { renderApp, renderWithProviders } from "@/test/render";
import { AuthLayout, AuthSkeleton } from "./auth-layout";

/** The sheet's top edge: the product name and the theme button, nothing else. */
function expectSheetHeader() {
  const header = screen.getByRole("banner");
  expect(within(header).getByText("KeiPay")).toBeInTheDocument();
  expect(within(header).getByRole("button", { name: "Change theme" })).toBeInTheDocument();
  expect(within(header).getAllByRole("button")).toHaveLength(1);
  return header;
}

describe("AuthLayout", () => {
  it("puts one heading, its subtitle and the form in the sheet's main area", () => {
    renderWithProviders(
      <AuthLayout title="Sign in to KeiPay" subtitle="Use your console account.">
        <form aria-label="Sign in form" />
      </AuthLayout>,
    );
    expectSheetHeader();
    const main = screen.getByRole("main");
    expect(within(main).getByRole("heading", { level: 1, name: "Sign in to KeiPay" })).toBeInTheDocument();
    expect(within(main).getByText("Use your console account.")).toBeInTheDocument();
    expect(within(main).getByRole("form", { name: "Sign in form" })).toBeInTheDocument();
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(main).not.toHaveAttribute("aria-busy");
    expect(screen.queryByTestId("auth-skeleton")).not.toBeInTheDocument();
  });

  it("changes the theme from the sheet's theme menu and remembers it", async () => {
    const { user } = renderWithProviders(
      <AuthLayout title="Sign in to KeiPay" subtitle="Use your console account.">
        <p>form</p>
      </AuthLayout>,
    );
    const header = expectSheetHeader();
    await user.click(within(header).getByRole("button", { name: "Change theme" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Dark" }));
    expect(document.documentElement).toHaveClass("dark");
    expect(window.localStorage.getItem(storageKeys.theme)).toBe("dark");

    await user.click(within(header).getByRole("button", { name: "Change theme" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Light" }));
    expect(document.documentElement).not.toHaveClass("dark");
    expect(window.localStorage.getItem(storageKeys.theme)).toBe("light");
  });
});

describe("AuthSkeleton", () => {
  it("keeps the sheet and its theme menu while marking the main area busy", () => {
    renderWithProviders(<AuthSkeleton />);
    expectSheetHeader();
    const main = screen.getByRole("main");
    expect(main).toHaveAttribute("aria-busy", "true");
    expect(main).toHaveAttribute("data-testid", "auth-skeleton");
    expect(within(main).getByRole("status")).toHaveTextContent(/loading/i);
    expect(screen.queryByRole("heading", { level: 1 })).not.toBeInTheDocument();
  });
});

describe("Auth pages on the sheet", () => {
  it("renders first-run setup on the sheet with the theme menu", async () => {
    mockApi({
      "GET /admin/me": signedOut,
      "GET /admin/setup": () => json(200, { setup_required: true, setup_token_required: false }),
    });
    renderApp("/setup");
    expect(await screen.findByRole("button", { name: "Create owner account" })).toBeInTheDocument();
    expectSheetHeader();
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
  });

  it("sends a finished setup back to the sign-in sheet", async () => {
    mockApi({
      "GET /admin/me": signedOut,
      "GET /admin/setup": () => json(200, { setup_required: false, setup_token_required: false }),
    });
    const { router } = renderApp("/setup");
    expect(await screen.findByRole("heading", { level: 1, name: "Sign in to KeiPay" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
    expectSheetHeader();
  });

  it("renders the change-password page on the sheet with the theme menu", async () => {
    renderApp("/change-password");
    expect(await screen.findByRole("heading", { level: 1, name: "Change password" })).toBeInTheDocument();
    expectSheetHeader();
    // The console shell (sidebar, test-mode switch) stays out of the way.
    expect(screen.queryByRole("switch", { name: "Test mode" })).not.toBeInTheDocument();
  });
});
