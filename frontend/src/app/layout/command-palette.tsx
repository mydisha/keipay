import { FlaskConicalIcon, MonitorIcon, MoonIcon, RadioIcon, SunIcon } from "lucide-react";
import { useEffect } from "react";
import { useNavigate } from "react-router";
import { useMode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { useTheme, type Theme } from "@/shared/lib/theme";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/shared/ui/command";
import { navItems } from "./nav";

const themeOptions: { value: Theme; label: string; icon: typeof SunIcon }[] = [
  { value: "light", label: strings.shell.themeLight, icon: SunIcon },
  { value: "dark", label: strings.shell.themeDark, icon: MoonIcon },
  { value: "system", label: strings.shell.themeSystem, icon: MonitorIcon },
];

/** Opens on ⌘K / Ctrl+K anywhere in the shell. */
export function useCommandPaletteShortcut(toggle: () => void) {
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key.toLowerCase() === "k" && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        toggle();
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [toggle]);
}

export function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const navigate = useNavigate();
  const { setTheme } = useTheme();
  const { mode, setMode } = useMode();

  function run(action: () => void) {
    onOpenChange(false);
    action();
  }

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title={strings.palette.title}
      description={strings.palette.description}
    >
      <CommandInput placeholder={strings.palette.placeholder} aria-label={strings.palette.placeholder} />
      <p className="border-b px-3 py-2 text-xs text-muted-foreground">{strings.palette.searchHint}</p>
      <CommandList>
        <CommandEmpty>{strings.palette.empty}</CommandEmpty>
        <CommandGroup heading={strings.palette.navigation}>
          {navItems.map((item) => (
            <CommandItem key={item.to} value={`go ${item.label}`} onSelect={() => run(() => navigate(item.to))}>
              <item.icon aria-hidden="true" />
              {item.label}
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandSeparator />
        <CommandGroup heading={strings.palette.theme}>
          {themeOptions.map((option) => (
            <CommandItem
              key={option.value}
              value={`theme ${option.label}`}
              onSelect={() => run(() => setTheme(option.value))}
            >
              <option.icon aria-hidden="true" />
              {strings.palette.useTheme(option.label)}
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandSeparator />
        <CommandGroup heading={strings.palette.mode}>
          {mode === "live" ? (
            <CommandItem value="mode test" onSelect={() => run(() => setMode("test"))}>
              <FlaskConicalIcon aria-hidden="true" />
              {strings.palette.switchToTest}
            </CommandItem>
          ) : (
            <CommandItem value="mode live" onSelect={() => run(() => setMode("live"))}>
              <RadioIcon aria-hidden="true" />
              {strings.palette.switchToLive}
            </CommandItem>
          )}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
