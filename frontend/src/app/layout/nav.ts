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

export interface NavGroup {
  label: string;
  items: NavItem[];
}

/** The sidebar's groups, in order. */
export const navGroups: NavGroup[] = [
  { label: strings.nav.groupOverview, items: [{ to: "/", label: strings.nav.dashboard, icon: LayoutDashboardIcon }] },
  {
    label: strings.nav.groupOperate,
    items: [
      { to: "/apps", label: strings.nav.apps, icon: BoxesIcon },
      { to: "/connections", label: strings.nav.connections, icon: PlugIcon },
      { to: "/webhooks", label: strings.nav.webhooks, icon: WebhookIcon },
      { to: "/payments", label: strings.nav.payments, icon: CreditCardIcon },
    ],
  },
  {
    label: strings.nav.groupControl,
    items: [
      { to: "/reconciliation", label: strings.nav.reconciliation, icon: ScaleIcon },
      { to: "/settings", label: strings.nav.settings, icon: SettingsIcon },
    ],
  },
];

/** Every page link in sidebar order, for the command palette. */
export const navItems: NavItem[] = navGroups.flatMap((group) => group.items);
