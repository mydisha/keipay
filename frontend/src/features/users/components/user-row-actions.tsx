import { BanIcon, CircleCheckIcon, EllipsisIcon, KeyRoundIcon, PencilIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import type { User } from "../api/users";

export type UserAction = "edit" | "reset" | "disable" | "enable";

interface UserRowActionsProps {
  user: User;
  /** The signed-in user's own row: no disable or reset (the API refuses both). */
  isSelf: boolean;
  onAction: (action: UserAction, user: User) => void;
}

export function UserRowActions({ user, isSelf, onAction }: UserRowActionsProps) {
  const disabled = user.status === "disabled";
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={strings.common.actionsFor(user.email)}>
          <EllipsisIcon aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={() => onAction("edit", user)}>
          <PencilIcon aria-hidden="true" />
          {strings.users.edit}
        </DropdownMenuItem>
        {disabled && (
          <DropdownMenuItem onSelect={() => onAction("enable", user)}>
            <CircleCheckIcon aria-hidden="true" />
            {strings.users.enable}
          </DropdownMenuItem>
        )}
        {!isSelf && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={() => onAction("reset", user)}>
              <KeyRoundIcon aria-hidden="true" />
              {strings.users.resetPassword}
            </DropdownMenuItem>
            {!disabled && (
              <DropdownMenuItem variant="destructive" onSelect={() => onAction("disable", user)}>
                <BanIcon aria-hidden="true" />
                {strings.users.disable}
              </DropdownMenuItem>
            )}
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
