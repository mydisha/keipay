import { z } from "zod";
import { strings } from "@/shared/lib/strings";

/** Mirrors ResolveReconciliationItemRequest: trimmed, 1-1000 characters (code points). */
export const MAX_NOTE_LENGTH = 1000;

export function noteLength(note: string): number {
  return [...note.trim()].length;
}

export const resolveFormSchema = z.object({
  note: z
    .string()
    .refine((note) => noteLength(note) > 0, strings.reconciliation.noteRequired)
    .refine((note) => noteLength(note) <= MAX_NOTE_LENGTH, strings.reconciliation.noteTooLong),
});

export type ResolveFormValues = z.infer<typeof resolveFormSchema>;

export const resolveRuleMessages: Record<string, Record<string, string>> = {
  note: { required: strings.reconciliation.noteRequired, max: strings.reconciliation.noteTooLong },
};
