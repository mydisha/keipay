import { formatMoney } from "@/shared/lib/format";
import { cn } from "@/shared/lib/utils";

interface MoneyTextProps {
  /** Minor units of `currency` (whole rupiah for IDR). */
  amount: number;
  currency?: string;
  className?: string;
}

export function MoneyText({ amount, currency = "IDR", className }: MoneyTextProps) {
  return (
    <span className={cn("block text-right tabular-nums whitespace-nowrap", className)}>
      {formatMoney(amount, currency)}
    </span>
  );
}
