import { expect, test, type Page } from "@playwright/test";
import {
  expectAuditAction,
  expectNoHorizontalScroll,
  requireOwnerCredentials,
  settled,
  signInToDashboard,
  signOut,
} from "./helpers";

// T-55: sign in with email, visit every section, sign out; mobile fits.

requireOwnerCredentials();

const pages = [
  { link: "Dashboard", heading: "Dashboard", path: "/" },
  { link: "Apps", heading: "Apps", path: "/apps", expectText: "Demo App" },
  { link: "Connections", heading: "Connections", path: "/connections" },
  { link: "Webhooks", heading: "Webhooks", path: "/webhooks" },
  { link: "Payments", heading: "Payments", path: "/payments" },
  { link: "Reconciliation", heading: "Reconciliation", path: "/reconciliation" },
  { link: "Settings", heading: "Settings", path: "/settings", expectText: "Change password" },
];

async function expectPageReady(page: Page, item: (typeof pages)[number]) {
  await expect(page).toHaveURL(new RegExp(`${item.path === "/" ? "/" : item.path}$`));
  await expect(page.getByRole("heading", { level: 1, name: item.heading })).toBeVisible();
  await settled(page);
  if (item.expectText) await expect(page.getByText(item.expectText).first()).toBeVisible();
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("signs in, visits every section and signs out", async ({ page }) => {
    await signInToDashboard(page);
    const nav = page.getByRole("navigation", { name: "Main" });
    for (const item of pages) {
      await nav.getByRole("link", { name: item.link, exact: true }).click();
      await expectPageReady(page, item);
    }
    await expectAuditAction(page, "tenant.create");
    await expectAuditAction(page, "user.setup");
    await signOut(page);
  });
});

test.describe("deep links", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("every page and old URL loads directly, as after a reload", async ({ page }) => {
    await signInToDashboard(page);
    for (const item of pages) {
      await page.goto(item.path);
      await expectPageReady(page, item);
    }
    for (const path of ["/settings/users", "/settings/audit-log"]) {
      await page.goto(path);
      await settled(page);
      await expect(page.getByRole("heading", { level: 1, name: "Settings" })).toBeVisible();
      await expect(page.getByTestId("forbidden-state")).toHaveCount(0);
    }
    for (const [from, to] of [
      ["/tenants", pages[1]!],
      ["/deliveries", pages[3]!],
      ["/ledger", pages[5]!],
    ] as const) {
      await page.goto(from);
      await expectPageReady(page, to);
    }
  });
});

test.describe("mobile 390x844", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("navigates through the mobile menu without horizontal scroll", async ({ page }) => {
    await signInToDashboard(page);
    for (const item of pages) {
      await page.getByRole("button", { name: "Open navigation" }).click();
      const sheet = page.getByRole("dialog");
      await sheet.getByRole("link", { name: item.link, exact: true }).click();
      await expect(sheet).toBeHidden();
      await expectPageReady(page, item);
      await expectNoHorizontalScroll(page, item.path);
    }
    for (const path of ["/settings/users", "/settings/audit-log"]) {
      await page.goto(path);
      await settled(page);
      await expectNoHorizontalScroll(page, path);
    }
    // The users table is cards on a phone.
    await page.goto("/settings/users");
    await settled(page);
    await expect(page.locator("table")).toHaveCount(0);
    await expect(page.getByTestId("data-cards")).toBeVisible();
    await signOut(page);
  });
});
