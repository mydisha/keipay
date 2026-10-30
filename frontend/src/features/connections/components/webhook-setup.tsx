import { ExternalLinkIcon } from "lucide-react";
import { useId } from "react";
import { strings } from "@/shared/lib/strings";
import { CopyButton } from "@/shared/ui/copy-button";
import type { Connection, Provider } from "../api/connections";

interface WebhookSetupProps {
  connection: Pick<Connection, "webhook_url" | "legacy_webhook_alias">;
  provider: Provider | undefined;
}

/** The connection's callback URL with copy, and the provider's steps for pasting it into its dashboard. */
export function WebhookSetup({ connection, provider }: WebhookSetupProps) {
  const labelId = useId();
  const setup = provider?.webhook_setup;
  return (
    <div className="grid gap-4">
      <div className="grid gap-2">
        <span id={labelId} className="text-sm font-medium">
          {strings.connections.callbackUrl}
        </span>
        <div className="flex min-w-0 items-center gap-2">
          <code
            aria-labelledby={labelId}
            data-testid="callback-url"
            className="min-w-0 flex-1 font-mono text-[0.8125rem] break-all"
          >
            {connection.webhook_url}
          </code>
          <CopyButton value={connection.webhook_url} label={strings.common.copyValue(strings.connections.callbackUrl)} />
        </div>
        {connection.legacy_webhook_alias && (
          <p className="text-xs text-muted-foreground">{strings.connections.legacyAliasNote}</p>
        )}
      </div>
      {setup && setup.instructions.length > 0 && (
        <ol className="grid list-decimal gap-1.5 pl-5 text-sm">
          {setup.instructions.map((step, index) => (
            <li key={index}>{step}</li>
          ))}
        </ol>
      )}
      {(setup?.dashboard_url || provider?.docs_url) && (
        <div className="flex flex-wrap gap-x-4 gap-y-2 text-sm">
          {setup?.dashboard_url && <ExternalLink href={setup.dashboard_url} label={strings.connections.openDashboard} />}
          {provider?.docs_url && <ExternalLink href={provider.docs_url} label={strings.connections.openDocs} />}
        </div>
      )}
    </div>
  );
}

function ExternalLink({ href, label }: { href: string; label: string }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer noopener"
      className="inline-flex items-center gap-1 rounded-sm text-link underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:min-h-11"
    >
      {label}
      <ExternalLinkIcon className="size-3.5" aria-hidden="true" />
    </a>
  );
}
