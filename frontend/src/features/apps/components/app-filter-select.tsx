import { useId } from "react";
import { strings } from "@/shared/lib/strings";
import { Label } from "@/shared/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { useAppOptions } from "../api/apps";

const ALL = "all";

interface AppFilterSelectProps {
  /** The selected app's public id (`app_…`), or null for every app. */
  value: string | null;
  onChange: (appId: string | null) => void;
}

/**
 * Filters a list by app; used by payments, webhooks and reconciliation. Lists
 * the newest `APP_OPTIONS_LIMIT` apps only.
 */
export function AppFilterSelect({ value, onChange }: AppFilterSelectProps) {
  const id = useId();
  const { data: apps = [] } = useAppOptions();
  return (
    <div className="flex items-center gap-2">
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {strings.common.appFilterLabel}
      </Label>
      <Select value={value ?? ALL} onValueChange={(next) => onChange(next === ALL ? null : next)}>
        <SelectTrigger id={id} className="h-8 w-56 max-w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{strings.common.allApps}</SelectItem>
          {apps.map((app) => (
            <SelectItem key={app.id} value={app.id}>
              {strings.common.appOption(app.name, strings.environments[app.environment] ?? app.environment)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
