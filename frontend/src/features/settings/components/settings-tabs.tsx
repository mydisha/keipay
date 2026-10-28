import { usePermissions } from "@/features/auth/components/can";
import type { Permission } from "@/features/auth/api/auth";
import { strings } from "@/shared/lib/strings";
import { LinkTabs, type LinkTab } from "@/shared/ui/link-tabs";

interface SettingsTab extends LinkTab {
  permission?: Permission;
}

const tabs: SettingsTab[] = [
  { to: "/settings", label: strings.settings.accountTab },
  { to: "/settings/users", label: strings.settings.usersTab, permission: "users:manage" },
  { to: "/settings/audit-log", label: strings.settings.auditTab, permission: "audit:read" },
];

/** Route-backed tabs; a tab the role can't open is not shown at all. */
export function SettingsTabs() {
  const can = usePermissions();
  const visible = tabs.filter((tab) => !tab.permission || can(tab.permission));
  return <LinkTabs label={strings.settings.tabsLabel} tabs={visible} />;
}
