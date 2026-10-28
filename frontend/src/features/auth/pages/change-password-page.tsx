import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useForm } from "react-hook-form";
import { Link, useNavigate } from "react-router";
import { toast } from "sonner";
import { isApiError } from "@/shared/api/errors";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { useChangePassword } from "../api/auth";
import { AuthLayout } from "../components/auth-layout";
import { PasswordInput } from "../components/password-input";
import { changePasswordSchema, type ChangePasswordValues } from "../schemas/auth";

export function ChangePasswordPage() {
  const navigate = useNavigate();
  const change = useChangePassword();
  const form = useForm<ChangePasswordValues>({
    resolver: zodResolver(changePasswordSchema),
    defaultValues: { current_password: "", new_password: "", confirm_password: "" },
  });
  const pending = change.isPending;
  // Wrong current password is shown on its field; other failures above the form.
  const formError =
    change.isError && !(isApiError(change.error) && change.error.code === "invalid_credentials")
      ? errorMessage(change.error, strings.auth.changeFailed)
      : null;

  function onSubmit(values: ChangePasswordValues) {
    change.mutate(
      { current_password: values.current_password, new_password: values.new_password },
      {
        onSuccess: () => {
          toast.success(strings.auth.changed);
          navigate("/", { replace: true });
        },
        onError: (error) => {
          if (isApiError(error) && error.code === "invalid_credentials") {
            form.setError("current_password", { message: strings.auth.currentIncorrect }, { shouldFocus: true });
          }
        },
      },
    );
  }

  return (
    <AuthLayout title={strings.auth.changePasswordTitle} subtitle={strings.auth.changePasswordSubtitle}>
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
                  <FormDescription>{strings.auth.newTooShort}</FormDescription>
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
          <Button asChild variant="link" className="justify-self-center">
            <Link to="/">{strings.auth.backToConsole}</Link>
          </Button>
        </form>
      </Form>
    </AuthLayout>
  );
}
