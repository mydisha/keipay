import {
  BoxesIcon,
  CreditCardIcon,
  LayoutDashboardIcon,
  PlugIcon,
  ScaleIcon,
  SettingsIcon,
  WebhookIcon,
  type LucideIcon,
} from "lucide-react";
import { strings } from "@/shared/lib/strings";

export interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
}

/** The pages people work in, top of the sidebar. */
export const workItems: NavItem[] = [
  { to: "/", label: strings.nav.dashboard, icon: LayoutDashboardIcon },
  { to: "/apps", label: strings.nav.apps, icon: BoxesIcon },
  { to: "/connections", label: strings.nav.connections, icon: PlugIcon },
  { to: "/webhooks", label: strings.nav.webhooks, icon: WebhookIcon },
  { to: "/payments", label: strings.nav.payments, icon: CreditCardIcon },
  { to: "/reconciliation", label: strings.nav.reconciliation, icon: ScaleIcon },
];

/** Settings sits apart at the bottom of the sidebar. */
export const settingsItem: NavItem = { to: "/settings", label: strings.nav.settings, icon: SettingsIcon };

/** Every page link in sidebar order, for the command palette. */
export const navItems: NavItem[] = [...workItems, settingsItem];
