import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useId, useState, type FormEvent, type ReactNode } from "react";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { Label } from "@/shared/ui/label";
import { useReplaceChannels, type Connection } from "../api/connections";

interface ChannelsFormProps {
  connection: Pick<Connection, "id" | "channels">;
  readOnly?: boolean;
  onSaved?: (connection: Connection) => void;
  /** Extra buttons next to Save (the wizard's Skip). */
  extraActions?: ReactNode;
}

/** One checkbox per provider channel; saving sends the full list. */
export function ChannelsForm({ connection, readOnly = false, onSaved, extraActions }: ChannelsFormProps) {
  const replace = useReplaceChannels();
  const baseId = useId();
  const [enabled, setEnabled] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(connection.channels.map((c) => [c.channel, c.enabled])),
  );
  const [error, setError] = useState<string | null>(null);
  const pending = replace.isPending;

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const channels = connection.channels.map((c) => ({ channel: c.channel, enabled: enabled[c.channel] ?? false }));
    if (!channels.some((c) => c.enabled)) {
      setError(strings.connections.channelsMinOne);
      return;
    }
    setError(null);
    replace.mutate(
      { id: connection.id, channels },
      {
        onSuccess: (result) => onSaved?.(result.connection),
        onError: (err) => setError(errorMessage(err, strings.connections.channelsFailed)),
      },
    );
  }

  return (
    <form onSubmit={onSubmit} noValidate className="grid gap-4">
      {error && (
        <Alert variant="danger">
          <CircleAlertIcon aria-hidden="true" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <fieldset disabled={pending || readOnly} className="grid gap-1">
        <legend className="sr-only">{strings.connections.channelsLegend}</legend>
        <ul className="grid gap-1 sm:grid-cols-2">
          {connection.channels.map((channel) => {
            const id = `${baseId}-${channel.channel}`;
            return (
              <li key={channel.channel} className="flex items-center gap-3 max-md:min-h-11">
                <Checkbox
                  id={id}
                  checked={enabled[channel.channel] ?? false}
                  onCheckedChange={(checked) => setEnabled((prev) => ({ ...prev, [channel.channel]: checked === true }))}
                  disabled={pending || readOnly}
                />
                <Label htmlFor={id} className="font-normal">
                  {channel.label}
                </Label>
              </li>
            );
          })}
        </ul>
      </fieldset>
      {!readOnly && (
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={pending}>
            {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
            {pending ? strings.connections.savingChannels : strings.connections.saveChannels}
          </Button>
          {extraActions}
        </div>
      )}
    </form>
  );
}
