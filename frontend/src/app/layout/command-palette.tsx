import { useQueryClient } from "@tanstack/react-query";
import { FlaskConicalIcon, MonitorIcon, MoonIcon, RadioIcon, SearchIcon, SunIcon, UsersIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { usePermissions } from "@/features/auth/components/can";
import { fetchWebhookEndpoint } from "@/features/webhooks/api/endpoints";
import { deliveryPath, eventDeliveriesPath, inboundPath } from "@/features/webhooks/lib/webhooks-search";
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

// Public ids are a prefix and a 26-character ULID; lower case is accepted.
const RECORD_ID = /^(dlv|inb|evt|whe)_[0-9a-z]{26}$/i;

type RecordKind = "dlv" | "inb" | "evt" | "whe";

/** The webhook record a pasted id points to, or null. */
export function parseRecordId(text: string): { kind: RecordKind; id: string } | null {
  const id = text.trim();
  const match = RECORD_ID.exec(id);
  if (!match) return null;
  return { kind: match[1]!.toLowerCase() as RecordKind, id };
}

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
  const queryClient = useQueryClient();
  const can = usePermissions();
  const canManageUsers = can("users:manage");
  const [search, setSearch] = useState("");
  const record = parseRecordId(search);
  const recordAllowed = record !== null && (record.kind === "whe" ? can("apps:read") : can("deliveries:read"));

  function run(action: () => void) {
    setSearch("");
    onOpenChange(false);
    action();
  }

  function openRecord(target: { kind: RecordKind; id: string }) {
    switch (target.kind) {
      case "dlv":
        return void navigate(deliveryPath(target.id));
      case "inb":
        return void navigate(inboundPath(target.id));
      case "evt":
        return void navigate(eventDeliveriesPath(target.id));
      case "whe":
        fetchWebhookEndpoint(queryClient, target.id).then(
          (endpoint) => void navigate(`/apps/${endpoint.app.id}/webhooks`),
          () => toast.error(strings.palette.endpointNotFound),
        );
    }
  }

  const recordLabel = record
    ? {
        dlv: strings.palette.openDelivery,
        inb: strings.palette.openInbound,
        evt: strings.palette.openEvent,
        whe: strings.palette.openEndpoint,
      }[record.kind](record.id)
    : "";

  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setSearch("");
        onOpenChange(next);
      }}
      title={strings.palette.title}
      description={strings.palette.description}
    >
      <CommandInput
        placeholder={strings.palette.placeholder}
        aria-label={strings.palette.placeholder}
        value={search}
        onValueChange={setSearch}
      />
      <p className="border-b px-3 py-2 text-xs text-muted-foreground">{strings.palette.searchHint}</p>
      <CommandList>
        <CommandEmpty>{strings.palette.empty}</CommandEmpty>
        {record && recordAllowed && (
          <>
            <CommandGroup heading={strings.palette.records} forceMount>
              <CommandItem value={`open ${record.id}`} forceMount onSelect={() => run(() => openRecord(record))}>
                <SearchIcon aria-hidden="true" />
                <span className="truncate">{recordLabel}</span>
              </CommandItem>
            </CommandGroup>
            <CommandSeparator />
          </>
        )}
        <CommandGroup heading={strings.palette.navigation}>
          {navItems.map((item) => (
            <CommandItem key={item.to} value={`go ${item.label}`} onSelect={() => run(() => navigate(item.to))}>
              <item.icon aria-hidden="true" />
              {item.label}
            </CommandItem>
          ))}
          {canManageUsers && (
            <CommandItem value="go users" onSelect={() => run(() => navigate("/settings/users"))}>
              <UsersIcon aria-hidden="true" />
              {strings.palette.users}
            </CommandItem>
          )}
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
