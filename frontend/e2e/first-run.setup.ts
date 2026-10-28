import { expect, test } from "@playwright/test";
import {
  expectNoHorizontalScroll,
  newBrowserContext,
  ownerEmail,
  ownerPassword,
  requireOwnerCredentials,
  signInToDashboard,
} from "./helpers";

// T-54. The gate starts with no user: create the owner through the console's
// first-run setup. On a rerun against the same server (setup already done)
// only check that the owner can sign in.

requireOwnerCredentials();

test("creates the first owner through setup", async ({ page, request, browser }) => {
  const status = (await (await request.get("/admin/setup")).json()) as {
    setup_required: boolean;
    setup_token_required: boolean;
  };

  if (status.setup_required) {
    expect(status.setup_token_required, "the e2e server must run without SETUP_TOKEN").toBe(false);
    await page.goto("/");
    await expect(page).toHaveURL(/\/setup$/);
    await page.goto("/login");
    await expect(page).toHaveURL(/\/setup$/);
    await expect(page.getByRole("heading", { level: 1, name: "Set up KeiPay" })).toBeVisible();
    await expect(page.getByLabel("Setup token")).toHaveCount(0);
    await page.setViewportSize({ width: 375, height: 812 });
    await expectNoHorizontalScroll(page, "/setup at 375px");
    await page.setViewportSize({ width: 1280, height: 800 });

    await page.getByLabel("Name").fill("E2E Owner");
    await page.getByLabel("Email").fill(ownerEmail);
    await page.getByLabel("Password", { exact: true }).fill(ownerPassword);
    await page.getByLabel("Confirm password").fill(ownerPassword);
    await page.getByRole("button", { name: "Create owner account" }).click();

    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByRole("heading", { level: 1, name: "Dashboard" })).toBeVisible();
  } else {
    await signInToDashboard(page);
  }

  // Signed in: /setup leads back to the console.
  await page.goto("/setup");
  await expect(page).toHaveURL(/\/$/);

  // Signed out: /setup leads to sign-in.
  const fresh = await newBrowserContext(browser);
  const anonymous = await fresh.newPage();
  await anonymous.goto("/setup");
  await expect(anonymous).toHaveURL(/\/login$/);
  await expect(anonymous.getByRole("button", { name: "Sign in" })).toBeVisible();
  await fresh.close();

  const after = (await (await request.get("/admin/setup")).json()) as { setup_required: boolean };
  expect(after.setup_required).toBe(false);
});
