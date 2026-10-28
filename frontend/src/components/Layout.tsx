import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { clearToken } from "../api/client";
import {
  Buildings,
  CreditCard,
  Receipt,
  WebhooksLogo,
  SignOut,
  User,
} from "@phosphor-icons/react";

const links = [
  { to: "/tenants", label: "Tenants", icon: Buildings },
  { to: "/payments", label: "Payments", icon: CreditCard },
  { to: "/ledger", label: "Ledger", icon: Receipt },
  { to: "/deliveries", label: "Webhooks", icon: WebhooksLogo },
];

export default function Layout() {
  const navigate = useNavigate();

  function logout() {
    clearToken();
    navigate("/login", { replace: true });
  }

  return (
    <div className="min-h-screen bg-surface-soft text-body">
      {/* Header */}
      <header className="sticky top-0 z-40 border-b border-hairline bg-canvas">
        <div className="mx-auto flex h-16 max-w-[1400px] items-center justify-between px-6">
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-ink">
              <span className="text-caption font-medium text-canvas">K</span>
            </div>
            <span className="text-title-sm text-ink">KeiPayment</span>
          </div>

          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2 rounded-lg bg-surface-soft px-3 py-2">
              <User size={16} weight="fill" className="text-muted" />
              <span className="text-body-md text-body">Admin</span>
            </div>
            <button
              onClick={logout}
              className="btn-ghost gap-2"
              aria-label="Sign out"
            >
              <SignOut size={18} weight="regular" />
              <span className="hidden sm:inline">Sign out</span>
            </button>
          </div>
        </div>
      </header>

      {/* Main content area */}
      <div className="mx-auto flex max-w-[1400px] gap-6 px-6 py-8">
        {/* Sidebar navigation */}
        <nav className="w-56 shrink-0">
          <div className="sticky top-24 space-y-1">
            {links.map((link) => (
              <NavLink
                key={link.to}
                to={link.to}
                className={({ isActive }) =>
                  `flex items-center gap-3 rounded-lg px-4 py-2.5 text-body-md transition-all duration-200 ease-smooth ${
                    isActive
                      ? "bg-ink text-canvas font-medium"
                      : "text-body hover:bg-surface-soft hover:text-ink"
                  }`
                }
              >
                <link.icon
                  size={18}
                  weight="regular"
                  className="shrink-0"
                />
                <span>{link.label}</span>
              </NavLink>
            ))}
          </div>
        </nav>

        {/* Main content */}
        <main className="min-w-0 flex-1">
          <Outlet />
        </main>
      </div>
    </div>
  );
}