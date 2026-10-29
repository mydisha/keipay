import type { Control, FieldValues, Path } from "react-hook-form";
import { strings } from "@/shared/lib/strings";
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";

type UrlFieldName = "redirect_success_url" | "redirect_failure_url";

const fields: { name: UrlFieldName; label: string }[] = [
  { name: "redirect_success_url", label: strings.apps.redirectSuccessUrl },
  { name: "redirect_failure_url", label: strings.apps.redirectFailureUrl },
];

/** The optional redirect URLs; shared by the create and edit forms. */
export function UrlFields<T extends FieldValues & Record<UrlFieldName, string>>({ control }: { control: Control<T> }) {
  return (
    <fieldset className="grid gap-4">
      <legend className="mb-1 text-sm font-medium">{strings.apps.redirectUrls}</legend>
      <p className="-mt-2 text-xs text-muted-foreground">{strings.apps.redirectUrlsHint}</p>
      {fields.map((item) => (
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
              <FormMessage />
            </FormItem>
          )}
        />
      ))}
    </fieldset>
  );
}
