import type { Control, FieldValues, Path } from "react-hook-form";
import { strings } from "@/shared/lib/strings";
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";

type UrlFieldName = "webhook_url" | "redirect_success_url" | "redirect_failure_url";

const fields: { name: UrlFieldName; label: string; hint?: string }[] = [
  { name: "webhook_url", label: strings.apps.webhookUrl, hint: strings.apps.webhookUrlHint },
  { name: "redirect_success_url", label: strings.apps.redirectSuccessUrl },
  { name: "redirect_failure_url", label: strings.apps.redirectFailureUrl },
];

/** The three URL inputs shared by the create and edit forms. */
export function UrlFields<T extends FieldValues & Record<UrlFieldName, string>>({ control }: { control: Control<T> }) {
  return fields.map((item) => (
    <FormField
      key={item.name}
      control={control}
      name={item.name as Path<T>}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{item.label}</FormLabel>
          <FormControl>
            <Input type="url" inputMode="url" placeholder="https://" autoComplete="off" {...field} />
          </FormControl>
          {item.hint && <FormDescription>{item.hint}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  ));
}
