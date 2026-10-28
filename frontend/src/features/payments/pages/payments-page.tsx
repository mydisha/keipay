import { CreditCardIcon, ExternalLinkIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { AppFilterSelect } from "@/features/apps/components/app-filter-select";
import { AppName } from "@/features/apps/components/app-name";
import { useTenantNames } from "@/features/apps/api/tenants";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { MoneyText } from "@/shared/ui/money-text";
import { PageHeader } from "@/shared/ui/page-header";
import { StatusBadge } from "@/shared/ui/status-badge";
import { isPaymentStatus, PAYMENT_STATUSES, usePayments, type Payment, type PaymentFilters } from "../api/payments";

const NO_FILTERS: PaymentFilters = { status: null, tenantId: null };

export function PaymentsPage() {
  const [filters, setFilters] = useState<PaymentFilters>(NO_FILTERS);
  const payments = usePayments(filters);
  const names = useTenantNames();
  const filtered = filters.status !== null || filters.tenantId !== null;

  const columns: DataColumn<Payment>[] = [
    {
      id: "id",
      header: strings.payments.payment,
      card: "title",
      cell: (p) => <IdText value={p.id} prefix="#" label={strings.payments.payment} />,
    },
    {
      id: "order_id",
      header: strings.payments.orderId,
      className: "max-w-48",
      cell: (p) => <IdText value={p.order_id} label={strings.payments.orderId} />,
    },
    { id: "app", header: strings.common.app, cell: (p) => <AppName id={p.tenant_id} names={names} /> },
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
        data={payments.data?.payments}
        getRowId={(p) => String(p.id)}
        isLoading={payments.isPending}
        error={payments.error}
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
          <AppFilterSelect value={filters.tenantId} onChange={(tenantId) => setFilters((f) => ({ ...f, tenantId }))} />
        }
        hasActiveFilters={filtered}
        onClearFilters={clearFilters}
        footer={
          payments.data && payments.data.payments.length > 0
            ? strings.payments.showing(payments.data.payments.length, payments.data.total)
            : null
        }
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
