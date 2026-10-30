import { CreditCardIcon } from "lucide-react";
import { Link } from "react-router";
import { AppLabel } from "@/features/apps/components/app-label";
import type { Payment } from "@/features/payments/api/payments";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { MoneyText } from "@/shared/ui/money-text";
import { Section } from "@/shared/ui/section";
import { StatusBadge } from "@/shared/ui/status-badge";

const columns: DataColumn<Payment>[] = [
  {
    id: "id",
    header: strings.payments.payment,
    card: "title",
    className: "max-w-60",
    cell: (p) => <IdText value={p.id} label={strings.payments.payment} />,
  },
  { id: "app", header: strings.common.app, className: "max-w-56", cell: (p) => <AppLabel app={p.app} /> },
  { id: "amount", header: strings.payments.amount, align: "right", cell: (p) => <MoneyText amount={p.amount} currency={p.currency} /> },
  { id: "status", header: strings.payments.status, cell: (p) => <StatusBadge status={p.status} /> },
  { id: "created", header: strings.payments.created, cell: (p) => <DateTimeText value={p.created_at} /> },
];

/** The newest payments, without filters or paging; the Payments page has both. */
export function RecentPayments({ payments }: { payments: Payment[] }) {
  return (
    <Section
      id="recent-payments-heading"
      title={strings.dashboard.recentTitle}
      description={strings.dashboard.recentBody}
      actions={
        <Button asChild variant="outline" size="sm">
          <Link to="/payments">{strings.dashboard.viewAllPayments}</Link>
        </Button>
      }
    >
      <DataTable
        caption={strings.dashboard.recentTitle}
        columns={columns}
        data={payments}
        getRowId={(p) => p.id}
        emptyState={
          <EmptyState
            icon={CreditCardIcon}
            headingLevel={3}
            title={strings.dashboard.recentEmptyTitle}
            description={strings.dashboard.recentEmptyBody}
          />
        }
      />
    </Section>
  );
}
