import { useId } from "react";
import { strings } from "@/shared/lib/strings";
import { Label } from "@/shared/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { useTenants } from "../api/tenants";

const ALL = "all";

interface AppFilterSelectProps {
  value: number | null;
  onChange: (tenantId: number | null) => void;
}

/** Filters a list by app (tenant); used by payments, webhooks and reconciliation. */
export function AppFilterSelect({ value, onChange }: AppFilterSelectProps) {
  const id = useId();
  const { data: tenants = [] } = useTenants();
  return (
    <div className="flex items-center gap-2">
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {strings.common.appFilterLabel}
      </Label>
      <Select
        value={value === null ? ALL : String(value)}
        onValueChange={(next) => onChange(next === ALL ? null : Number(next))}
      >
        <SelectTrigger id={id} className="h-8 w-48 max-w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{strings.common.allApps}</SelectItem>
          {tenants.map((tenant) => (
            <SelectItem key={tenant.id} value={String(tenant.id)}>
              {tenant.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
