import {
  BanIcon,
  CirclePlayIcon,
  EllipsisIcon,
  ListIcon,
  PencilIcon,
  RefreshCwIcon,
  SendIcon,
  Trash2Icon,
} from "lucide-react";
import { Link } from "react-router";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import type { WebhookEndpoint } from "../api/endpoints";

export type EndpointAction = "edit" | "test" | "rotate" | "disable" | "enable" | "delete";

/** Where the deliveries of one endpoint are listed. */
export function endpointDeliveriesPath(endpoint: Pick<WebhookEndpoint, "id">): string {
  return `/webhooks?tab=outbound&endpoint_id=${encodeURIComponent(endpoint.id)}`;
}

export function EndpointRowActions({
  endpoint,
  onAction,
}: {
  endpoint: WebhookEndpoint;
  onAction: (action: EndpointAction, endpoint: WebhookEndpoint) => void;
}) {
  const run = (action: EndpointAction) => () => onAction(action, endpoint);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={strings.endpoints.actionsFor(endpoint.url)}>
          <EllipsisIcon aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={run("edit")}>
          <PencilIcon aria-hidden="true" />
          {strings.endpoints.edit}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={run("test")}>
          <SendIcon aria-hidden="true" />
          {strings.endpoints.sendTest}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={run("rotate")}>
          <RefreshCwIcon aria-hidden="true" />
          {strings.endpoints.rotate}
        </DropdownMenuItem>
        {endpoint.status === "active" ? (
          <DropdownMenuItem onSelect={run("disable")}>
            <BanIcon aria-hidden="true" />
            {strings.endpoints.disable}
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem onSelect={run("enable")}>
            <CirclePlayIcon aria-hidden="true" />
            {strings.endpoints.enable}
          </DropdownMenuItem>
        )}
        <DropdownMenuItem asChild>
          <Link to={endpointDeliveriesPath(endpoint)}>
            <ListIcon aria-hidden="true" />
            {strings.endpoints.viewDeliveries}
          </Link>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={run("delete")}>
          <Trash2Icon aria-hidden="true" />
          {strings.endpoints.delete}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
