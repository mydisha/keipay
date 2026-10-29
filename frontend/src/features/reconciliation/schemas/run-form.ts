import { z } from "zod";
import { strings } from "@/shared/lib/strings";
import { daysInRange, isDateString, wibRange } from "@/shared/lib/wib-dates";
import type { CreateRunRequest } from "../api/runs";

/** Mirrors the server's `max_range` for a run (31 days). */
export const MAX_RUN_DAYS = 31;

/**
 * The run dialog's fields: a connection and whole WIB days. `today` is the
 * WIB date the dialog opened on; a run can't start after it (rule `past`).
 */
export function runFormSchema(today: string) {
  return z
    .object({
      connection_id: z.string().min(1, strings.reconciliation.field.connectionRequired),
      from: z.string().refine(isDateString, strings.reconciliation.rangeFromRequired),
      to: z.string().refine(isDateString, strings.reconciliation.rangeToRequired),
    })
    .superRefine((values, ctx) => {
      if (!isDateString(values.from) || !isDateString(values.to)) return;
      if (values.from > today) {
        ctx.addIssue({ code: "custom", path: ["from"], message: strings.reconciliation.field.fromPast });
      }
      const days = daysInRange(values.from, values.to);
      if (days < 1) ctx.addIssue({ code: "custom", path: ["to"], message: strings.reconciliation.field.toAfter });
      else if (days > MAX_RUN_DAYS) ctx.addIssue({ code: "custom", path: ["to"], message: strings.reconciliation.rangeTooLong(MAX_RUN_DAYS) });
    });
}

export type RunFormValues = z.infer<ReturnType<typeof runFormSchema>>;

export function toCreateRunRequest(values: RunFormValues): CreateRunRequest {
  return { connection_id: values.connection_id, ...wibRange(values.from, values.to) };
}

/** Server rules on POST /admin/reconciliation-runs, by field then rule. */
export const runRuleMessages: Record<string, Record<string, string>> = {
  connection_id: {
    required: strings.reconciliation.field.connectionRequired,
    public_id: strings.reconciliation.field.connectionExists,
    exists: strings.reconciliation.field.connectionExists,
    unsupported: strings.reconciliation.field.connectionUnsupported,
  },
  from: {
    required: strings.reconciliation.rangeFromRequired,
    datetime: strings.reconciliation.rangeFromRequired,
    past: strings.reconciliation.field.fromPast,
  },
  to: {
    required: strings.reconciliation.rangeToRequired,
    datetime: strings.reconciliation.rangeToRequired,
    after: strings.reconciliation.field.toAfter,
    max_range: strings.reconciliation.rangeTooLong(MAX_RUN_DAYS),
  },
};
