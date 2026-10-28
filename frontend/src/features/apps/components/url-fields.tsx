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

/** The webhook URL input, then the redirect URLs in an optional group; shared by the create and edit forms. */
export function UrlFields<T extends FieldValues & Record<UrlFieldName, string>>({ control }: { control: Control<T> }) {
  const [webhook, ...redirects] = fields;
  return (
    <>
      <UrlInputs control={control} items={webhook ? [webhook] : []} />
      <fieldset className="grid gap-4">
        <legend className="mb-1 text-sm font-medium">{strings.apps.redirectUrls}</legend>
        <p className="-mt-2 text-xs text-muted-foreground">{strings.apps.redirectUrlsHint}</p>
        <UrlInputs control={control} items={redirects} />
      </fieldset>
    </>
  );
}

function UrlInputs<T extends FieldValues & Record<UrlFieldName, string>>({
  control,
  items,
}: {
  control: Control<T>;
  items: typeof fields;
}) {
  return items.map((item) => (
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
