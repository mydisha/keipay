import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import { setTestCookie } from "@/test/cookies";
import { me } from "@/test/fixtures";
import { json, mockApi, signedOut } from "@/test/http";
import { renderApp } from "@/test/render";

// Only display preferences may live in web storage; the session is an
// HttpOnly cookie the console never reads.
const ALLOWED_KEYS = new Set<string>(Object.values(storageKeys));

function storageEntries(storage: Storage): [string, string][] {
  return Array.from({ length: storage.length }, (_, i) => {
    const key = storage.key(i)!;
    return [key, storage.getItem(key) ?? ""];
  });
}

describe("Web storage holds no auth data (T-51, AC-21)", () => {
  it("has only preference keys, none about the session", () => {
    expect(Object.keys(storageKeys).sort()).toEqual(["mode", "sidebarCollapsed", "theme"]);
  });

  it("stays free of auth data through sign-in, use and sign-out", async () => {
    setTestCookie("keipay_csrf", "csrf-secret-value");
    let session: unknown = null;
    mockApi({
      "GET /admin/me": () => (session ? json(200, session) : signedOut()),
      "POST /admin/login": () => {
        session = me();
        return json(200, session);
      },
      "POST /admin/logout": () => {
        session = null;
        return new Response(null, { status: 204 });
      },
    });
    const { user, router } = renderApp("/login");
    await user.type(await screen.findByLabelText("Email"), "owner@example.com");
    await user.type(screen.getByLabelText("Password"), "a long password");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await screen.findByRole("heading", { level: 1, name: "Dashboard" });
    // A preference write is fine; it must stay the only kind of entry.
    await user.click(screen.getByRole("button", { name: "Collapse sidebar" }));

    await user.click(screen.getByRole("button", { name: "Account menu" }));
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));

    const entries = [...storageEntries(window.localStorage), ...storageEntries(window.sessionStorage)];
    expect(entries.length).toBeGreaterThan(0);
    for (const [key, value] of entries) {
      expect(ALLOWED_KEYS.has(key), `unexpected storage key ${key}`).toBe(true);
      expect(value).not.toMatch(/owner@example\.com|password|csrf|token|session/i);
    }
    expect(storageEntries(window.sessionStorage)).toEqual([]);
  });
});
