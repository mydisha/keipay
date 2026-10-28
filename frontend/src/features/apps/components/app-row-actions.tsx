import { EllipsisIcon, PencilIcon, PowerIcon, RefreshCwIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import type { Tenant } from "../api/tenants";

interface AppRowActionsProps {
  tenant: Tenant;
  onEdit: (tenant: Tenant) => void;
  onRotate: (tenant: Tenant) => void;
  onToggleActive: (tenant: Tenant) => void;
}

export function AppRowActions({ tenant, onEdit, onRotate, onToggleActive }: AppRowActionsProps) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={strings.common.actionsFor(tenant.name)}>
          <EllipsisIcon aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={() => onEdit(tenant)}>
          <PencilIcon aria-hidden="true" />
          {strings.apps.edit}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => onToggleActive(tenant)}>
          <PowerIcon aria-hidden="true" />
          {tenant.active ? strings.apps.deactivate : strings.apps.activate}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => onRotate(tenant)}>
          <RefreshCwIcon aria-hidden="true" />
          {strings.apps.rotate}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
