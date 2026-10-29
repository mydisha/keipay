import {
  FlaskConicalIcon,
  KeyRoundIcon,
  LogOutIcon,
  MenuIcon,
  MonitorIcon,
  MoonIcon,
  SearchIcon,
  SettingsIcon,
  SunIcon,
} from "lucide-react";
import { useId } from "react";
import { Link } from "react-router";
import { useMe, useSignOut } from "@/features/auth/api/auth";
import { useMode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { useTheme, type Theme } from "@/shared/lib/theme";
import { cn } from "@/shared/lib/utils";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Label } from "@/shared/ui/label";
import { Switch } from "@/shared/ui/switch";
import { initialsOf } from "./sidebar";

interface TopBarProps {
  onOpenMenu: () => void;
  onOpenPalette: () => void;
}

export function TopBar({ onOpenMenu, onOpenPalette }: TopBarProps) {
  return (
    <header className="sticky top-0 z-40 flex h-14 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur md:px-6 lg:px-8">
      <Button variant="ghost" size="icon" className="md:hidden" onClick={onOpenMenu} aria-label={strings.nav.openMenu}>
        <MenuIcon aria-hidden="true" />
      </Button>
      <Button
        variant="outline"
        className="h-9 justify-start gap-2 bg-card text-muted-foreground shadow-xs max-sm:w-11 max-sm:px-0 max-sm:justify-center sm:w-72"
        onClick={onOpenPalette}
        aria-label={strings.shell.openPalette}
        aria-keyshortcuts="Meta+K Control+K"
      >
        <SearchIcon aria-hidden="true" />
        <span className="max-sm:sr-only">{strings.shell.search}</span>
        <kbd className="ml-auto hidden rounded border bg-muted px-1.5 font-mono text-[0.6875rem] sm:inline">⌘K</kbd>
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
    <div
      data-mode={mode}
      className={cn(
        "flex h-9 items-center gap-2 rounded-full border px-2.5 transition-colors max-md:min-h-11",
        testing ? "border-test-mode-border bg-test-mode text-test-mode-foreground" : "border-border bg-card text-muted-foreground shadow-xs",
      )}
    >
      <FlaskConicalIcon className="size-3.5 shrink-0" aria-hidden="true" />
      <Label htmlFor={id} className="cursor-pointer text-xs whitespace-nowrap">
        {strings.shell.modeSwitchLabel}
      </Label>
      <Switch id={id} checked={testing} onCheckedChange={(checked) => setMode(checked ? "test" : "live")} />
    </div>
  );
}

const themeIcons: Record<Theme, typeof SunIcon> = { light: SunIcon, dark: MoonIcon, system: MonitorIcon };

function ThemeMenu() {
  const { theme, setTheme } = useTheme();
  const Icon = themeIcons[theme];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={strings.shell.toggleTheme}>
          <Icon aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{strings.shell.theme}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={theme} onValueChange={(value) => setTheme(value as Theme)}>
          <DropdownMenuRadioItem value="light">{strings.shell.themeLight}</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">{strings.shell.themeDark}</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">{strings.shell.themeSystem}</DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
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
            className="flex size-8 items-center justify-center rounded-full bg-sidebar-primary/15 text-xs font-semibold text-sidebar-primary"
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
