import { CreditCardIcon, ExternalLinkIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { AppFilterSelect } from "@/features/apps/components/app-filter-select";
import { AppLabel } from "@/features/apps/components/app-label";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { MoneyText } from "@/shared/ui/money-text";
import { PageHeader } from "@/shared/ui/page-header";
import { StatusBadge } from "@/shared/ui/status-badge";
import { isPaymentStatus, PAYMENT_STATUSES, usePayments, type Payment, type PaymentFilters } from "../api/payments";

const NO_FILTERS: PaymentFilters = { status: null, appId: null };

export function PaymentsPage() {
  const [filters, setFilters] = useState<PaymentFilters>(NO_FILTERS);
  const payments = usePayments(filters);
  const list = loadMoreList(payments);
  const filtered = filters.status !== null || filters.appId !== null;

  const columns: DataColumn<Payment>[] = [
    {
      id: "id",
      header: strings.payments.payment,
      card: "title",
      className: "max-w-60",
      cell: (p) => <IdText value={p.id} label={strings.payments.payment} />,
    },
    {
      id: "order_id",
      header: strings.payments.orderId,
      className: "max-w-48",
      cell: (p) => <IdText value={p.order_id} label={strings.payments.orderId} />,
    },
    { id: "app", header: strings.common.app, className: "max-w-56", cell: (p) => <AppLabel app={p.app} /> },
    { id: "status", header: strings.payments.status, cell: (p) => <StatusBadge status={p.status} /> },
    {
      id: "amount",
      header: strings.payments.amount,
      align: "right",
      cell: (p) => <MoneyText amount={p.amount} currency={p.currency} />,
    },
    { id: "created", header: strings.payments.created, cell: (p) => <DateTimeText value={p.created_at} /> },
    {
      id: "invoice",
      header: strings.payments.invoice,
      card: "actions",
      align: "right",
      cell: (p) =>
        p.invoice_url ? (
          <Button asChild variant="ghost" size="sm">
            <a href={p.invoice_url} target="_blank" rel="noreferrer noopener">
              {strings.payments.openInvoice}
              <ExternalLinkIcon aria-hidden="true" />
            </a>
          </Button>
        ) : null,
    },
  ];

  const clearFilters = () => setFilters(NO_FILTERS);

  return (
    <>
      <PageHeader title={strings.payments.title} description={strings.payments.subtitle} />
      <DataTable
        caption={strings.payments.title}
        columns={columns}
        data={list.rows}
        getRowId={(p) => p.id}
        isLoading={payments.isPending}
        error={list.error}
        onRetry={() => void payments.refetch()}
        filters={[
          {
            id: "status",
            label: strings.payments.statusFilter,
            options: PAYMENT_STATUSES.map((s) => ({ value: s, label: strings.status[s] ?? s })),
            value: filters.status,
            onChange: (value) => setFilters((f) => ({ ...f, status: isPaymentStatus(value) ? value : null })),
          },
        ]}
        toolbar={
          <AppFilterSelect value={filters.appId} onChange={(appId) => setFilters((f) => ({ ...f, appId }))} />
        }
        hasActiveFilters={filtered}
        onClearFilters={clearFilters}
        loadMore={list.loadMore}
        emptyState={
          filtered ? (
            <EmptyState
              icon={CreditCardIcon}
              title={strings.payments.emptyFilteredTitle}
              description={strings.payments.emptyFilteredBody}
              action={
                <Button variant="outline" onClick={clearFilters}>
                  {strings.common.clearFilters}
                </Button>
              }
            />
          ) : (
            <EmptyState
              icon={CreditCardIcon}
              title={strings.payments.emptyTitle}
              description={strings.payments.emptyBody}
              action={
                <Button asChild variant="outline">
                  <Link to="/apps">{strings.payments.emptyAction}</Link>
                </Button>
              }
            />
          )
        }
      />
    </>
  );
}
