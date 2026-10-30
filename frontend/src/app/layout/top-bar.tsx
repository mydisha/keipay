import { KeyRoundIcon, LogOutIcon, MenuIcon, SearchIcon, SettingsIcon } from "lucide-react";
import { useId } from "react";
import { Link } from "react-router";
import { useMe, useSignOut } from "@/features/auth/api/auth";
import { useMode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Label } from "@/shared/ui/label";
import { Switch } from "@/shared/ui/switch";
import { ThemeMenu } from "@/shared/ui/theme-menu";

/** The first letters of up to two words of a name. */
export function initialsOf(name: string): string {
  return name
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0]?.toUpperCase() ?? "")
    .join("");
}

interface TopBarProps {
  onOpenMenu: () => void;
  onOpenPalette: () => void;
}

export function TopBar({ onOpenMenu, onOpenPalette }: TopBarProps) {
  return (
    <header className="sticky top-0 z-40 flex h-14 items-center gap-2 border-b bg-background px-4 md:px-6 lg:px-8">
      <Button variant="ghost" className="gap-1.5 px-2 md:hidden" onClick={onOpenMenu} aria-label={strings.nav.openMenu}>
        <MenuIcon aria-hidden="true" />
        <span aria-hidden="true">{strings.nav.menu}</span>
      </Button>
      <Button
        variant="outline"
        className="h-9 justify-start gap-2 text-muted-foreground max-sm:w-11 max-sm:px-0 max-sm:justify-center sm:w-56 md:w-40 lg:w-72"
        onClick={onOpenPalette}
        aria-label={strings.shell.openPalette}
        aria-keyshortcuts="Meta+K Control+K"
      >
        <SearchIcon aria-hidden="true" />
        <span className="max-sm:sr-only">{strings.shell.search}</span>
        <kbd className="ml-auto hidden rounded-sm border px-1.5 font-mono text-[0.6875rem] sm:inline">⌘K</kbd>
      </Button>
      <div className="ml-auto flex items-center gap-1 sm:gap-2">
        <ModeSwitch />
        <ThemeMenu />
        <UserMenu />
      </div>
    </header>
  );
}

function ModeSwitch() {
  const { mode, setMode } = useMode();
  const id = useId();
  const testing = mode === "test";
  return (
    <div data-mode={mode} className="flex h-9 items-center gap-2 px-1.5 max-md:min-h-11">
      <Label
        htmlFor={id}
        className={cn(
          "cursor-pointer text-xs whitespace-nowrap transition-colors",
          testing ? "text-warning-foreground" : "font-normal text-muted-foreground",
        )}
      >
        {/* A phone shows "Test"; the switch keeps its full name, which contains the visible word. */}
        <span className="sm:hidden">{strings.shell.modeSwitchShort}</span>
        <span className="max-sm:hidden">{strings.shell.modeSwitchLabel}</span>
      </Label>
      <Switch
        id={id}
        aria-label={strings.shell.modeSwitchLabel}
        checked={testing}
        onCheckedChange={(checked) => setMode(checked ? "test" : "live")}
        className="data-[state=checked]:bg-warning-foreground"
      />
    </div>
  );
}

function UserMenu() {
  const { data: me } = useMe();
  const signOut = useSignOut();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={strings.shell.userMenu} className="rounded-full">
          <span
            aria-hidden="true"
            className="flex size-8 items-center justify-center rounded-full border bg-secondary text-xs font-medium text-secondary-foreground"
          >
            {me ? initialsOf(me.user.name) : ""}
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64 max-w-[calc(100vw-2rem)]">
        {me && (
          <DropdownMenuLabel className="grid gap-0.5 font-normal" data-testid="user-menu-identity">
            <span className="truncate font-medium" title={me.user.name}>
              {me.user.name}
            </span>
            <span className="truncate text-xs text-muted-foreground" title={me.user.email}>
              {me.user.email}
            </span>
            <span className="text-xs text-muted-foreground">{strings.shell.roleLabel(strings.roles[me.user.role] ?? me.user.role)}</span>
          </DropdownMenuLabel>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/settings">
            <SettingsIcon aria-hidden="true" />
            {strings.shell.accountSettings}
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to="/change-password">
            <KeyRoundIcon aria-hidden="true" />
            {strings.shell.changePassword}
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => signOut.mutate()} disabled={signOut.isPending}>
          <LogOutIcon aria-hidden="true" />
          {strings.shell.signOut}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
