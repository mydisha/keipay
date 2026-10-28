import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { Link, useNavigate } from "react-router";
import { toast } from "sonner";
import { errorMessage } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { applyFieldErrors } from "@/shared/lib/field-errors";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { useChangePassword, useMe, useSignOut } from "../api/auth";
import { AuthLayout } from "../components/auth-layout";
import { PasswordInput } from "@/shared/ui/password-input";
import { changePasswordSchema, type ChangePasswordValues } from "../schemas/auth";

const FIELD_NAMES = ["current_password", "new_password"] as const;

export function ChangePasswordPage() {
  const navigate = useNavigate();
  const me = useMe();
  const change = useChangePassword();
  const signOut = useSignOut();
  const [formError, setFormError] = useState<string | null>(null);
  const form = useForm<ChangePasswordValues>({
    resolver: zodResolver(changePasswordSchema),
    defaultValues: { current_password: "", new_password: "", confirm_password: "" },
  });
  // A temporary password from an owner: the console stays locked until it is replaced.
  const forced = me.data?.user.must_change_password ?? false;
  const pending = change.isPending;

  function onError(error: unknown) {
    if (isApiError(error) && error.code === "invalid_credentials") {
      form.setError("current_password", { message: strings.auth.currentIncorrect }, { shouldFocus: true });
      return;
    }
    if (isApiError(error) && error.code === "validation_failed") {
      form.setError("new_password", { message: strings.auth.newSameAsCurrent }, { shouldFocus: true });
      return;
    }
    if (!applyFieldErrors(error, form.setError, FIELD_NAMES)) {
      setFormError(errorMessage(error, strings.auth.changeFailed));
    }
  }

  function onSubmit(values: ChangePasswordValues) {
    setFormError(null);
    change.mutate(
      { current_password: values.current_password, new_password: values.new_password },
      {
        onSuccess: () => {
          toast.success(strings.auth.changed);
          navigate("/", { replace: true });
        },
        onError,
      },
    );
  }

  return (
    <AuthLayout
      title={forced ? strings.auth.forcedTitle : strings.auth.changePasswordTitle}
      subtitle={forced ? strings.auth.forcedSubtitle : strings.auth.changePasswordSubtitle}
    >
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate className="grid gap-4">
          {formError && (
            <Alert variant="danger">
              <CircleAlertIcon aria-hidden="true" />
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          )}
          <fieldset disabled={pending} className="grid gap-4">
            <FormField
              control={form.control}
              name="current_password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.auth.currentPassword}</FormLabel>
                  <FormControl>
                    <PasswordInput autoComplete="current-password" autoFocus {...field} />
                  </FormControl>
                  {forced && <FormDescription>{strings.auth.forcedCurrentHint}</FormDescription>}
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="new_password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.auth.newPassword}</FormLabel>
                  <FormControl>
                    <PasswordInput autoComplete="new-password" {...field} />
                  </FormControl>
                  <FormDescription>{strings.auth.passwordHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="confirm_password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.auth.confirmPassword}</FormLabel>
                  <FormControl>
                    <PasswordInput autoComplete="new-password" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </fieldset>
          <Button type="submit" disabled={pending} className="w-full">
            {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
            {pending ? strings.auth.changing : strings.auth.submitChange}
          </Button>
          {forced ? (
            <Button
              variant="link"
              className="justify-self-center"
              onClick={() => signOut.mutate()}
              disabled={signOut.isPending || pending}
            >
              {strings.auth.signOut}
            </Button>
          ) : (
            <Button asChild variant="link" className="justify-self-center">
              <Link to="/">{strings.auth.backToConsole}</Link>
            </Button>
          )}
        </form>
      </Form>
    </AuthLayout>
  );
}
