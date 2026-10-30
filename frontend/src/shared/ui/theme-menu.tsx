import { MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { useTheme, type Theme } from "@/shared/lib/theme";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

const themeIcons: Record<Theme, typeof SunIcon> = { light: SunIcon, dark: MoonIcon, system: MonitorIcon };

/** Light, dark or system, from an icon button showing the current choice. */
export function ThemeMenu() {
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
