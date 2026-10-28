import { expect, test, type Page } from "@playwright/test";

const username = process.env.E2E_USERNAME ?? "admin";
const password = process.env.E2E_PASSWORD ?? "";

const pages = [
  { link: "Dashboard", heading: "Dashboard", path: "/" },
  { link: "Apps", heading: "Apps", path: "/apps", expectText: "Demo App" },
  { link: "Connections", heading: "Connections", path: "/connections" },
  { link: "Webhooks", heading: "Webhooks", path: "/webhooks" },
  { link: "Payments", heading: "Payments", path: "/payments" },
  { link: "Reconciliation", heading: "Reconciliation", path: "/reconciliation" },
  { link: "Settings", heading: "Settings", path: "/settings", expectText: "tenant.create" },
];

test.skip(!password, "E2E_PASSWORD is not set; run through `make e2e` or set E2E_BASE_URL, E2E_USERNAME and E2E_PASSWORD");

async function signIn(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { level: 1, name: "Dashboard" })).toBeVisible();
}

async function expectPageReady(page: Page, item: (typeof pages)[number]) {
  await expect(page).toHaveURL(new RegExp(`${item.path === "/" ? "/" : item.path}$`));
  await expect(page.getByRole("heading", { level: 1, name: item.heading })).toBeVisible();
  // Wait for every list to finish loading before judging the page.
  await expect(page.locator("[aria-busy=true]")).toHaveCount(0);
  if (item.expectText) await expect(page.getByText(item.expectText).first()).toBeVisible();
  await expect(page.getByTestId("error-state")).toHaveCount(0);
}

async function signOut(page: Page) {
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("signs in, visits every section and signs out", async ({ page }) => {
    await signIn(page);
    const nav = page.getByRole("navigation", { name: "Main" });
    for (const item of pages) {
      await nav.getByRole("link", { name: item.link, exact: true }).click();
      await expectPageReady(page, item);
    }
    await signOut(page);
  });
});

test.describe("deep links", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("every page and old URL loads directly, as after a reload", async ({ page }) => {
    await signIn(page);
    for (const item of pages) {
      await page.goto(item.path);
      await expectPageReady(page, item);
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
    await signIn(page);
    for (const item of pages) {
      await page.getByRole("button", { name: "Open navigation" }).click();
      const sheet = page.getByRole("dialog");
      await sheet.getByRole("link", { name: item.link, exact: true }).click();
      await expect(sheet).toBeHidden();
      await expectPageReady(page, item);
      const overflow = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      }));
      expect(overflow.scrollWidth, `${item.path} scrolls horizontally`).toBeLessThanOrEqual(overflow.clientWidth);
    }
    await signOut(page);
  });
});
