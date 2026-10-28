import { KeyRoundIcon, LogOutIcon, MenuIcon, MonitorIcon, MoonIcon, SearchIcon, SunIcon, UserIcon } from "lucide-react";
import { useId } from "react";
import { Link } from "react-router";
import { getTokenSubject } from "@/shared/api/session";
import { useMode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { useTheme, type Theme } from "@/shared/lib/theme";
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

interface TopBarProps {
  onOpenMenu: () => void;
  onOpenPalette: () => void;
  onSignOut: () => void;
}

export function TopBar({ onOpenMenu, onOpenPalette, onSignOut }: TopBarProps) {
  return (
    <header className="sticky top-0 z-40 flex h-14 items-center gap-2 border-b bg-background/95 px-4 backdrop-blur md:px-6">
      <Button variant="ghost" size="icon" className="md:hidden" onClick={onOpenMenu} aria-label={strings.nav.openMenu}>
        <MenuIcon aria-hidden="true" />
      </Button>
      <Button
        variant="outline"
        className="h-9 justify-start gap-2 text-muted-foreground max-sm:w-11 max-sm:px-0 max-sm:justify-center sm:w-56"
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
        <UserMenu onSignOut={onSignOut} />
      </div>
    </header>
  );
}

function ModeSwitch() {
  const { mode, setMode } = useMode();
  const id = useId();
  return (
    <div className="flex items-center gap-2 rounded-md px-1.5 max-md:min-h-11">
      <Switch id={id} checked={mode === "test"} onCheckedChange={(checked) => setMode(checked ? "test" : "live")} />
      <Label htmlFor={id} className="cursor-pointer text-xs whitespace-nowrap">
        {strings.shell.modeSwitchLabel}
      </Label>
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

function UserMenu({ onSignOut }: { onSignOut: () => void }) {
  const name = getTokenSubject() ?? strings.shell.admin;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={strings.shell.userMenu}>
          <UserIcon aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel className="font-normal">{strings.shell.signedInAs(name)}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/change-password">
            <KeyRoundIcon aria-hidden="true" />
            {strings.shell.changePassword}
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={onSignOut}>
          <LogOutIcon aria-hidden="true" />
          {strings.shell.signOut}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
