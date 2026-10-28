import { Navigate, type RouteObject } from "react-router";
import { DashboardPage } from "@/features/dashboard/pages/dashboard-page";
import { AppShell } from "./layout/app-shell";
import { RequireAuth } from "./require-auth";

export const routes: RouteObject[] = [
  {
    path: "/login",
    lazy: async () => ({ Component: (await import("@/features/auth/pages/login-page")).LoginPage }),
  },
  {
    path: "/change-password",
    lazy: async () => {
      const { ChangePasswordPage } = await import("@/features/auth/pages/change-password-page");
      return {
        Component: () => (
          <RequireAuth>
            <ChangePasswordPage />
          </RequireAuth>
        ),
      };
    },
  },
  {
    path: "/",
    element: (
      <RequireAuth>
        <AppShell />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <DashboardPage /> },
      // Feature pages load on first visit to keep the initial bundle small.
      {
        path: "apps",
        lazy: async () => ({ Component: (await import("@/features/apps/pages/apps-page")).AppsPage }),
      },
      {
        path: "connections",
        lazy: async () => ({
          Component: (await import("@/features/connections/pages/connections-page")).ConnectionsPage,
        }),
      },
      {
        path: "webhooks",
        lazy: async () => ({ Component: (await import("@/features/webhooks/pages/webhooks-page")).WebhooksPage }),
      },
      {
        path: "payments",
        lazy: async () => ({ Component: (await import("@/features/payments/pages/payments-page")).PaymentsPage }),
      },
      {
        path: "reconciliation",
        lazy: async () => ({
          Component: (await import("@/features/reconciliation/pages/reconciliation-page")).ReconciliationPage,
        }),
      },
      {
        path: "settings",
        lazy: async () => ({ Component: (await import("@/features/settings/pages/settings-page")).SettingsPage }),
      },
      // URLs from the previous console.
      { path: "tenants", element: <Navigate to="/apps" replace /> },
      { path: "deliveries", element: <Navigate to="/webhooks" replace /> },
      { path: "ledger", element: <Navigate to="/reconciliation" replace /> },
    ],
  },
  { path: "*", element: <Navigate to="/" replace /> },
];
