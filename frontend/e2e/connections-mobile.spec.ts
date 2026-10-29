import { expect, test, type Page } from "@playwright/test";
import {
  expectNoHorizontalScroll,
  requireOwnerCredentials,
  settled,
  showAllEnvironments,
  signInToDashboard,
} from "./helpers";

// The connection screens that connections.spec.ts does not reach at 375px
// (detail, its dialogs, the post-create wizard steps and the app Overview default
// connection) fit at 375px in light and dark mode, with text statuses. Uses
// the imported `xendit-imported` connection and never runs a connection test,
// so nothing calls Xendit and nothing is changed.

requireOwnerCredentials();

async function importedConnectionId(page: Page): Promise<string> {
  await page.goto("/connections");
  await settled(page);
  await showAllEnvironments(page);
  const link = page
    .getByRole("link", { name: "xendit-imported", exact: true })
    .first();
  await expect(link).toBeVisible();
  const href = await link.getAttribute("href");
  const id = href?.match(/con_[0-9A-Za-z]{26}/)?.[0];
  expect(id, `connection link ${href}`).toBeTruthy();
  return id as string;
}

async function expectDialogFits(page: Page, name: string) {
  const dialog = page.getByRole("dialog", { name });
  await expect(dialog).toBeVisible();
  const box = await dialog.boundingBox();
  expect(box, `${name} has a box`).not.toBeNull();
  expect(box!.x, `${name} starts on screen`).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width, `${name} ends on screen`).toBeLessThanOrEqual(
    375,
  );
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
}

test.describe("connection screens at 375px", () => {
  test.use({ viewport: { width: 375, height: 812 }, hasTouch: true });

  for (const theme of ["light", "dark"] as const) {
    test(`detail, dialogs, wizard steps and app default fit in ${theme} mode`, async ({
      page,
    }) => {
      await page.addInitScript(
        (t) => window.localStorage.setItem("keipay.theme", t),
        theme,
      );
      await signInToDashboard(page);
      if (theme === "dark")
        await expect(page.locator("html")).toHaveClass(/\bdark\b/);
      else await expect(page.locator("html")).not.toHaveClass(/\bdark\b/);
      const id = await importedConnectionId(page);

      // Detail: header, text statuses, legacy badge, callback URL, hints only.
      await page.goto(`/connections/${id}`);
      await expect(
        page.getByRole("heading", { level: 1, name: "xendit-imported" }),
      ).toBeVisible();
      await settled(page);
      await expect(
        page.getByText("Active", { exact: true }).first(),
      ).toBeVisible();
      await expect(
        page.getByText("Legacy URL", { exact: true }).first(),
      ).toBeVisible();
      await expect(
        page.getByText(/\/webhooks\/xendit\/con_/).first(),
      ).toBeVisible();
      await expect(
        page.getByText("Also receives callbacks at /webhooks/xendit (legacy)."),
      ).toBeVisible();
      await expectNoHorizontalScroll(page, `/connections/:id (${theme})`);

      await page.getByRole("button", { name: "Replace credentials" }).click();
      await expectDialogFits(page, "Replace credentials");
      await page.getByRole("button", { name: "Disable", exact: true }).click();
      await expectDialogFits(page, "Disable xendit-imported?");

      // Wizard steps after creation resume from the URL and fit.
      await page.goto(`/connections/new?connection=${id}&step=4`);
      await expect(page.getByText("Set up the callback URL")).toBeVisible();
      await expect(
        page.getByText(/\/webhooks\/xendit\/con_/).first(),
      ).toBeVisible();
      await expectNoHorizontalScroll(page, `wizard step 4 (${theme})`);
      await page.goto(`/connections/new?connection=${id}&step=5`);
      await expect(
        page.getByRole("button", { name: "Save channels" }),
      ).toBeVisible();
      await expect(page.getByRole("checkbox", { name: "QRIS" })).toBeVisible();
      await expectNoHorizontalScroll(page, `wizard step 5 (${theme})`);

      // App Overview: the seeded demo app uses the imported connection.
      await page.goto("/apps");
      await settled(page);
      await showAllEnvironments(page);
      await page.getByRole("link", { name: "Demo App" }).first().click();
      await expect(
        page.getByRole("heading", { level: 1, name: "Demo App" }),
      ).toBeVisible();
      await settled(page);
      await expect(
        page.getByRole("combobox", { name: "Default connection" }),
      ).toHaveText("xendit-imported");
      await expectNoHorizontalScroll(page, `app overview (${theme})`);
    });
  }
});
