import { CopyIcon, SearchXIcon } from "lucide-react";
import type { ReactNode } from "react";
import { usePermission } from "@/features/auth/components/can";
import { AppLabel } from "@/features/apps/components/app-label";
import { formatDateTimeWIB } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { ErrorState } from "@/shared/ui/error-state";
import { IdText } from "@/shared/ui/id-text";
import { JsonViewer } from "@/shared/ui/json-viewer";
import { MoneyText } from "@/shared/ui/money-text";
import { SidePanel } from "@/shared/ui/side-panel";
import { Skeleton } from "@/shared/ui/skeleton";
import { StatusBadge } from "@/shared/ui/status-badge";
import { useReconciliationItem, useResolveItem, type ReconciliationItem, type ReconciliationRun, type ResolveItemMutation } from "../api/runs";
import { isNotFound } from "../lib/is-not-found";
import { ResolutionBadge, ResultBadge } from "./badges";
import { ResolveForm } from "./resolve-form";

type KeipaySide = NonNullable<ReconciliationItem["keipay"]>;
type GatewaySide = NonNullable<ReconciliationItem["gateway"]>;

const SUCCESSFUL = new Set(["PAID", "SETTLED"]);

/** A status KeiPay does not know (`null`) counts as not successful, as in the matcher. */
function isSuccessful(status: string | null): boolean {
  return status !== null && SUCCESSFUL.has(status);
}

export interface ComparisonRow {
  id: string;
  label: string;
  keipay: ReactNode;
  gateway: ReactNode;
  /** Both sides have a value and they disagree in a way the matcher counts. */
  differs: boolean;
}

function absoluteTime(value: string | null): ReactNode {
  if (!value) return <span className="text-muted-foreground">{strings.common.none}</span>;
  const date = new Date(value);
  return <time dateTime={date.toISOString()}>{formatDateTimeWIB(date)}</time>;
}

function fee(amount: number | null, currency: string): ReactNode {
  if (amount === null) return <span className="text-muted-foreground">{strings.reconciliation.unknownFee}</span>;
  return <MoneyText amount={amount} currency={currency} className="text-left" />;
}

/** Field by field, with `differs` set the way the matcher compares (PAID and SETTLED count as the same). */
export function comparisonRows(keipay: KeipaySide | null, gateway: GatewaySide | null): ComparisonRow[] {
  const both = keipay !== null && gateway !== null;
  const missing = (text: string) => <span className="text-muted-foreground">{text}</span>;
  const empty = <span className="text-muted-foreground">{strings.common.none}</span>;
  const k = <T,>(render: (side: KeipaySide) => T, fallback: ReactNode = empty) => (keipay ? render(keipay) : fallback);
  const g = <T,>(render: (side: GatewaySide) => T, fallback: ReactNode = empty) => (gateway ? render(gateway) : fallback);

  return [
    {
      id: "status",
      label: strings.reconciliation.status,
      keipay: k((s) => <StatusBadge status={s.status} />, missing(strings.reconciliation.notInKeipay)),
      gateway: g(
        (s) => (
          <span className="flex flex-wrap items-center gap-2">
            {s.status && <StatusBadge status={s.status} />}
            <span className="font-mono text-xs text-muted-foreground" title={strings.reconciliation.gatewayOwnStatus}>
              {s.provider_status}
            </span>
          </span>
        ),
        missing(strings.reconciliation.notAtGateway),
      ),
      differs: both && isSuccessful(keipay.status) !== isSuccessful(gateway.status),
    },
    {
      id: "amount",
      label: strings.reconciliation.amount,
      keipay: k((s) => <MoneyText amount={s.amount} currency={s.currency} className="text-left" />),
      gateway: g((s) => <MoneyText amount={s.amount} currency={s.currency} className="text-left" />),
      differs: both && keipay.amount !== gateway.amount,
    },
    {
      id: "currency",
      label: strings.reconciliation.currency,
      keipay: k((s) => <span className="font-mono">{s.currency}</span>),
      gateway: g((s) => <span className="font-mono">{s.currency}</span>),
      differs: both && keipay.currency !== gateway.currency,
    },
    {
      id: "fee",
      label: strings.reconciliation.fee,
      keipay: k((s) => fee(s.fee, s.currency)),
      gateway: g((s) => fee(s.fee, s.currency)),
      differs: both && keipay.fee !== null && gateway.fee !== null && keipay.fee !== gateway.fee,
    },
    {
      id: "time",
      label: strings.reconciliation.time,
      keipay: k((s) => absoluteTime(s.paid_at)),
      gateway: g((s) => absoluteTime(s.occurred_at)),
      differs: false,
    },
    {
      id: "id",
      label: strings.reconciliation.recordId,
      keipay: k((s) => <IdText value={s.payment_id} label={strings.reconciliation.payment} />),
      gateway: g((s) => <IdText value={s.provider_payment_id} label={strings.reconciliation.gateway} />),
      differs: false,
    },
  ];
}

function Comparison({ item }: { item: ReconciliationItem }) {
  const rows = comparisonRows(item.keipay, item.gateway);
  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      <table className="w-full table-fixed text-sm">
        <caption className="sr-only">{strings.reconciliation.comparison}</caption>
        <thead className="border-b bg-muted/50">
          <tr className="text-left text-xs text-muted-foreground">
            <th scope="col" className="w-[30%] px-3 py-2 font-medium">
              {strings.reconciliation.fieldColumn}
            </th>
            <th scope="col" className="px-3 py-2 font-medium">
              {strings.reconciliation.keipay}
            </th>
            <th scope="col" className="px-3 py-2 font-medium">
              {strings.reconciliation.gateway}
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.id} data-differs={row.differs || undefined} className="border-b align-top last:border-0">
              <th scope="row" className="px-3 py-2.5 text-left font-medium">
                <span className="grid justify-items-start gap-1">
                  {row.label}
                  {row.differs && <StatusBadge status="differs" label={strings.reconciliation.differs} tone="danger" />}
                </span>
              </th>
              <td className="min-w-0 px-3 py-2.5 break-words">{row.keipay}</td>
              <td className="min-w-0 px-3 py-2.5 break-words">{row.gateway}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Resolution({ item, run, resolve }: { item: ReconciliationItem; run: ReconciliationRun; resolve: ResolveItemMutation }) {
  const canWrite = usePermission("reconciliation:write");
  let body: ReactNode;
  if (item.resolution) {
    body = (
      <dl className="grid gap-3 text-sm">
        <div className="grid gap-1">
          <dt className="text-muted-foreground">{strings.reconciliation.note}</dt>
          <dd className="break-words whitespace-pre-wrap">{item.resolution.note}</dd>
        </div>
        <div className="grid gap-1">
          <dt className="text-muted-foreground">{strings.reconciliation.resolvedBy}</dt>
          <dd>{item.resolution.resolved_by?.name ?? strings.reconciliation.system}</dd>
        </div>
        <div className="grid gap-1">
          <dt className="text-muted-foreground">{strings.reconciliation.resolvedAt}</dt>
          <dd>
            <DateTimeText value={item.resolution.resolved_at} />
          </dd>
        </div>
      </dl>
    );
  } else if (item.result === "matched") {
    body = <p className="text-sm text-muted-foreground">{strings.reconciliation.matchedNoAction}</p>;
  } else if (!canWrite) {
    body = <p className="text-sm text-muted-foreground">{strings.reconciliation.readOnlyUnresolved}</p>;
  } else if (run.status !== "succeeded") {
    body = <p className="text-sm text-muted-foreground">{strings.reconciliation.runNotFinished}</p>;
  } else {
    body = <ResolveForm key={item.id} item={item} resolve={resolve} />;
  }
  return (
    <section className="grid gap-2" aria-labelledby="resolution-heading">
      <h3 id="resolution-heading" className="text-sm font-semibold">
        {strings.reconciliation.resolution}
      </h3>
      {body}
    </section>
  );
}

function ItemDetail({ item, run, resolve }: { item: ReconciliationItem; run: ReconciliationRun; resolve: ResolveItemMutation }) {
  return (
    <div className="grid gap-6">
      <div className="grid gap-2">
        <IdText value={item.reference} label={strings.reconciliation.reference} className="text-sm" />
        <div className="flex flex-wrap items-center gap-2">
          <ResultBadge result={item.result} />
          <ResolutionBadge item={item} />
        </div>
        {item.keipay && (
          <div className="flex min-w-0 items-center gap-2 text-sm">
            <span className="text-muted-foreground">{strings.reconciliation.app}</span>
            <AppLabel app={item.keipay.app} />
          </div>
        )}
      </div>
      {item.gateway && item.gateway.transaction_count > 1 && (
        <Alert variant="warning" role="note">
          <CopyIcon aria-hidden="true" />
          <AlertDescription>{strings.reconciliation.transactionCount(item.gateway.transaction_count)}</AlertDescription>
        </Alert>
      )}
      <Comparison item={item} />
      <Resolution item={item} run={run} resolve={resolve} />
      <div className="grid gap-2">
        <JsonViewer value={item.keipay} label={strings.reconciliation.keipayJson} />
        <JsonViewer value={item.gateway} label={strings.reconciliation.gatewayJson} />
      </div>
    </div>
  );
}

function SheetSkeleton() {
  return (
    <div className="grid gap-4" aria-busy="true" data-testid="item-skeleton">
      <Skeleton className="h-5 w-2/3" />
      <Skeleton className="h-5 w-1/3" />
      <Skeleton className="h-48 w-full" />
    </div>
  );
}

/** The item named by `?item=rci_…`, read on its own so any list filter or page still finds it. */
export function ItemSheet({ run, itemId, onClose }: { run: ReconciliationRun; itemId: string | null; onClose: () => void }) {
  const query = useReconciliationItem(itemId);
  // Owned here because a resolve unmounts the form; see ResolveForm.
  const resolve = useResolveItem();
  // An id from another run's link is not this run's item.
  const item = query.data?.run_id === run.id ? query.data : null;

  let body: ReactNode;
  if (item) body = <ItemDetail item={item} run={run} resolve={resolve} />;
  else if (query.isPending) body = <SheetSkeleton />;
  else if (query.isError && !isNotFound(query.error))
    body = <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  else
    body = (
      <EmptyState
        icon={SearchXIcon}
        headingLevel={3}
        title={strings.reconciliation.itemNotFoundTitle}
        description={strings.reconciliation.itemNotFoundBody}
      />
    );

  return (
    <SidePanel
      open={itemId !== null}
      onOpenChange={(open) => !open && onClose()}
      title={strings.reconciliation.itemTitle}
      description={strings.reconciliation.itemDescription}
      locked={resolve.isPending}
    >
      {body}
    </SidePanel>
  );
}
