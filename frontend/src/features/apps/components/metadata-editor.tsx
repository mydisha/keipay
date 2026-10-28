import type { Control, FieldValues, Path } from "react-hook-form";
import { strings } from "@/shared/lib/strings";
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Textarea } from "@/shared/ui/textarea";

interface MetadataEditorProps<T extends FieldValues & { metadata: string }> {
  control: Control<T>;
  label?: string;
}

/** The metadata JSON textarea; validation lives in the form's zod schema (`metadataText`). */
export function MetadataEditor<T extends FieldValues & { metadata: string }>({
  control,
  label = strings.apps.metadataOptional,
}: MetadataEditorProps<T>) {
  return (
    <FormField
      control={control}
      name={"metadata" as Path<T>}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Textarea
              rows={4}
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              placeholder={'{"team": "growth"}'}
              className="font-mono md:text-[0.8125rem]"
              {...field}
            />
          </FormControl>
          <FormDescription>{strings.apps.metadataHint}</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
