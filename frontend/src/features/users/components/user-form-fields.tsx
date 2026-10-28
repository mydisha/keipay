import type { Control } from "react-hook-form";
import { strings } from "@/shared/lib/strings";
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { USER_ROLES } from "../api/users";
import type { UserFormValues } from "../schemas/user-form";

/** Email, name and role, shared by the add and edit dialogs. */
export function UserFormFields({ control }: { control: Control<UserFormValues> }) {
  return (
    <>
      <FormField
        control={control}
        name="email"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{strings.users.email}</FormLabel>
            <FormControl>
              <Input type="email" inputMode="email" autoComplete="off" autoCapitalize="off" spellCheck={false} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={control}
        name="name"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{strings.users.name}</FormLabel>
            <FormControl>
              <Input autoComplete="off" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={control}
        name="role"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{strings.users.role}</FormLabel>
            <Select value={field.value} onValueChange={field.onChange} disabled={field.disabled}>
              <FormControl>
                <SelectTrigger onBlur={field.onBlur} ref={field.ref}>
                  <SelectValue />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {USER_ROLES.map((role) => (
                  <SelectItem
                    key={role}
                    value={role}
                    textValue={strings.roles[role]}
                    description={strings.roleDescriptions[role]}
                  >
                    {strings.roles[role]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormDescription>{strings.roleDescriptions[field.value]}</FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
