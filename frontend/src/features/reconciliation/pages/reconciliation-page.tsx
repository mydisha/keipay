import { ScaleIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { AppFilterSelect } from "@/features/apps/components/app-filter-select";
import { AppLabel } from "@/features/apps/components/app-label";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { MoneyText } from "@/shared/ui/money-text";
import { PageHeader } from "@/shared/ui/page-header";
import { useLedger, type LedgerEntry } from "../api/ledger";
import { LedgerSummary } from "../components/ledger-summary";

export function ReconciliationPage() {
  const [appId, setAppId] = useState<string | null>(null);
  const ledger = useLedger(appId);

  const columns: DataColumn<LedgerEntry>[] = [
    {
      id: "id",
      header: strings.reconciliation.entry,
      card: "title",
      cell: (e) => <IdText value={e.id} prefix="#" label={strings.reconciliation.entry} />,
    },
    {
      id: "payment",
      header: strings.reconciliation.payment,
      className: "max-w-60",
      cell: (e) => <IdText value={e.payment_id} label={strings.reconciliation.payment} />,
    },
    { id: "app", header: strings.common.app, className: "max-w-56", cell: (e) => <AppLabel app={e.app} /> },
    { id: "type", header: strings.reconciliation.type, cell: (e) => <span className="font-mono text-xs">{e.type}</span> },
    { id: "gross", header: strings.reconciliation.gross, align: "right", cell: (e) => <MoneyText amount={e.amount} /> },
    { id: "fee", header: strings.reconciliation.fee, align: "right", cell: (e) => <MoneyText amount={e.xendit_fee} /> },
    {
      id: "net",
      header: strings.reconciliation.net,
      align: "right",
      cell: (e) => <MoneyText amount={e.net_amount} className="font-medium" />,
    },
    { id: "created", header: strings.reconciliation.created, cell: (e) => <DateTimeText value={e.created_at} /> },
  ];

  return (
    <>
      <PageHeader title={strings.reconciliation.title} description={strings.reconciliation.subtitle} />
      <div className="flex flex-wrap items-center gap-2">
        <AppFilterSelect value={appId} onChange={setAppId} />
      </div>
      {!ledger.error && (ledger.isPending || (ledger.data?.summary.length ?? 0) > 0) && (
        <section className="grid gap-3" aria-labelledby="ledger-summary-heading">
          <h2 id="ledger-summary-heading" className="text-base font-semibold">
            {strings.reconciliation.summaryTitle}
          </h2>
          <LedgerSummary summary={ledger.data?.summary} isLoading={ledger.isPending} />
        </section>
      )}
      <section className="grid gap-3" aria-labelledby="ledger-entries-heading">
        <h2 id="ledger-entries-heading" className="text-base font-semibold">
          {strings.reconciliation.entriesTitle}
        </h2>
        <DataTable
          caption={strings.reconciliation.entriesTitle}
          columns={columns}
          data={ledger.data?.entries}
          getRowId={(e) => String(e.id)}
          isLoading={ledger.isPending}
          error={ledger.error}
          onRetry={() => void ledger.refetch()}
          hasActiveFilters={appId !== null}
          onClearFilters={() => setAppId(null)}
          emptyState={
            <EmptyState
              icon={ScaleIcon}
              headingLevel={3}
              title={strings.reconciliation.emptyTitle}
              description={strings.reconciliation.emptyBody}
              action={
                <Button asChild variant="outline">
                  <Link to="/payments">{strings.reconciliation.emptyAction}</Link>
                </Button>
              }
            />
          }
        />
      </section>
    </>
  );
}
