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

export const navItems: NavItem[] = [
  { to: "/", label: strings.nav.dashboard, icon: LayoutDashboardIcon },
  { to: "/apps", label: strings.nav.apps, icon: BoxesIcon },
  { to: "/connections", label: strings.nav.connections, icon: PlugIcon },
  { to: "/webhooks", label: strings.nav.webhooks, icon: WebhookIcon },
  { to: "/payments", label: strings.nav.payments, icon: CreditCardIcon },
  { to: "/reconciliation", label: strings.nav.reconciliation, icon: ScaleIcon },
  { to: "/settings", label: strings.nav.settings, icon: SettingsIcon },
];
