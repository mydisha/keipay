import { BanIcon, EllipsisIcon, RefreshCwIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import type { ApiKey } from "../api/apps";

/** How a key is shown after creation: `{prefix}…{last4}`. */
export function keyHint(key: Pick<ApiKey, "prefix" | "last4">): string {
  return `${key.prefix}…${key.last4}`;
}

export type KeyAction = "rotate" | "revoke";

/** What can still be done with a key: rotate active ones, revoke anything not revoked yet. */
export function keyActions(key: ApiKey): KeyAction[] {
  const actions: KeyAction[] = [];
  if (key.status === "active") actions.push("rotate");
  if (key.status !== "revoked") actions.push("revoke");
  return actions;
}

export function KeyRowActions({ apiKey, onAction }: { apiKey: ApiKey; onAction: (action: KeyAction, key: ApiKey) => void }) {
  const actions = keyActions(apiKey);
  if (actions.length === 0) return null;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={strings.keys.actionsFor(apiKey.label, keyHint(apiKey))}>
          <EllipsisIcon aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {actions.includes("rotate") && (
          <DropdownMenuItem onSelect={() => onAction("rotate", apiKey)}>
            <RefreshCwIcon aria-hidden="true" />
            {strings.keys.rotate}
          </DropdownMenuItem>
        )}
        {actions.includes("revoke") && (
          <DropdownMenuItem variant="destructive" onSelect={() => onAction("revoke", apiKey)}>
            <BanIcon aria-hidden="true" />
            {strings.keys.revoke}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
