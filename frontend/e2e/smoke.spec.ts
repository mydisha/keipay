import { expect, test, type Page } from "@playwright/test";
import {
  expectAuditAction,
  expectNoHorizontalScroll,
  requireOwnerCredentials,
  settled,
  showAllEnvironments,
  signInToDashboard,
  signOut,
} from "./helpers";

// T-55 / T-60: sign in with email, visit every section and the app detail
// tabs, sign out; mobile fits. The seeded Demo App is a test app, and the
// Apps list starts at the live mode, so the specs pick All first.

requireOwnerCredentials();

const pages = [
  { link: "Dashboard", heading: "Dashboard", path: "/" },
  { link: "Apps", heading: "Apps", path: "/apps" },
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

const appTabs = [
  { name: "Overview", suffix: "", expectText: "Details" },
  { name: "API keys", suffix: "/keys", expectText: "kp_test_" },
  { name: "Webhooks", suffix: "/webhooks", expectText: "Webhook endpoints" },
  { name: "Routing", suffix: "/routing", expectText: "Routing is coming soon" },
];

/** On /apps: shows the Demo App (after choosing All) and opens it; returns its detail path. */
async function openDemoApp(page: Page) {
  await showAllEnvironments(page);
  await page.getByRole("link", { name: "Demo App" }).first().click();
  await expect(page).toHaveURL(/\/apps\/app_[0-9A-Z]{26}$/);
  await expect(page.getByRole("heading", { level: 1, name: "Demo App" })).toBeVisible();
  return new URL(page.url()).pathname;
}

/** Visits each app tab through its link and checks the content and the layout. */
async function visitAppTabs(page: Page, detailPath: string, checkScroll: boolean) {
  for (const tab of appTabs) {
    await page.getByRole("navigation", { name: "App sections" }).getByRole("link", { name: tab.name }).click();
    await expect(page).toHaveURL(new RegExp(`${detailPath}${tab.suffix}$`));
    await settled(page);
    await expect(page.getByText(tab.expectText).first()).toBeVisible();
    if (checkScroll) await expectNoHorizontalScroll(page, `${detailPath}${tab.suffix}`);
  }
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
    await page.goto("/apps");
    await settled(page);
    await visitAppTabs(page, await openDemoApp(page), false);
    await expectAuditAction(page, "app.create");
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
    await page.goto("/apps");
    await settled(page);
    const detail = await openDemoApp(page);
    for (const tab of appTabs) {
      await page.goto(`${detail}${tab.suffix}`);
      await expect(page.getByRole("heading", { level: 1, name: "Demo App" })).toBeVisible();
      await settled(page);
      await expect(page.getByText(tab.expectText).first()).toBeVisible();
    }
    await page.goto("/apps/app_00000000000000000000000000");
    await expect(page.getByText("App not found")).toBeVisible();
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
    await page.goto("/apps");
    await settled(page);
    const detail = await openDemoApp(page);
    await expectNoHorizontalScroll(page, detail);
    await visitAppTabs(page, detail, true);
    await expect(page.locator("table")).toHaveCount(0);
    // The users table is cards on a phone.
    await page.goto("/settings/users");
    await settled(page);
    await expect(page.locator("table")).toHaveCount(0);
    await expect(page.getByTestId("data-cards")).toBeVisible();
    await signOut(page);
  });
});
