import { Outlet } from "react-router";
import { strings } from "@/shared/lib/strings";
import { PageHeader } from "@/shared/ui/page-header";
import { SettingsTabs } from "../components/settings-tabs";

/** `/settings` layout: the page heading, the tabs the role can open, and the active tab. */
export function SettingsPage() {
  return (
    <>
      <PageHeader title={strings.settings.title} description={strings.settings.subtitle} />
      <SettingsTabs />
      <Outlet />
    </>
  );
}
