import { zodResolver } from "@hookform/resolvers/zod";
import { useQueryClient } from "@tanstack/react-query";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { errorMessage, retryAfterMinutes } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { applyFieldErrors } from "@/shared/lib/field-errors";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { ErrorState } from "@/shared/ui/error-state";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { setupStatusKey, useSetup, useSetupStatus } from "../api/auth";
import { AuthLayout } from "../components/auth-layout";
import { PasswordInput } from "../components/password-input";
import { setupSchema, type SetupValues } from "../schemas/auth";

const FIELD_NAMES = ["name", "email", "password", "setup_token"] as const;

export function SetupPage() {
  const status = useSetupStatus();
  if (!status.data) {
    return (
      <AuthLayout title={strings.setup.title} subtitle={strings.setup.subtitle}>
        <ErrorState error={status.error} title={strings.setup.loadFailed} onRetry={() => void status.refetch()} />
      </AuthLayout>
    );
  }
  const tokenRequired = status.data.setup_token_required;
  // Remount when the server starts or stops requiring a token: the schema changes.
  return <SetupForm key={String(tokenRequired)} tokenRequired={tokenRequired} />;
}

function SetupForm({ tokenRequired }: { tokenRequired: boolean }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const setup = useSetup();
  const [formError, setFormError] = useState<string | null>(null);
  const form = useForm<SetupValues>({
    resolver: zodResolver(setupSchema(tokenRequired)),
    defaultValues: { name: "", email: "", password: "", confirm_password: "", setup_token: "" },
  });
  const pending = setup.isPending;

  function onError(error: unknown) {
    if (isApiError(error)) {
      switch (error.code) {
        case "setup_completed":
          toast.info(strings.setup.completed);
          void queryClient.invalidateQueries({ queryKey: setupStatusKey });
          navigate("/login", { replace: true });
          return;
        case "invalid_setup_token":
          if (tokenRequired) {
            form.setError("setup_token", { message: strings.setup.setupTokenInvalid }, { shouldFocus: true });
            return;
          }
          // The server started requiring a token after this page loaded.
          void queryClient.invalidateQueries({ queryKey: setupStatusKey });
          setFormError(strings.setup.setupTokenInvalid);
          return;
        case "rate_limited":
          setFormError(strings.errors.rateLimited(retryAfterMinutes(error)));
          return;
      }
    }
    if (!applyFieldErrors(error, form.setError, FIELD_NAMES)) {
      setFormError(errorMessage(error, strings.setup.failed));
    }
  }

  function onSubmit(values: SetupValues) {
    setFormError(null);
    setup.mutate(
      {
        name: values.name,
        email: values.email,
        password: values.password,
        ...(tokenRequired ? { setup_token: values.setup_token } : {}),
      },
      { onSuccess: () => navigate("/", { replace: true }), onError },
    );
  }

  return (
    <AuthLayout title={strings.setup.title} subtitle={strings.setup.subtitle}>
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
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.auth.name}</FormLabel>
                  <FormControl>
                    <Input autoComplete="name" autoFocus {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="email"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.auth.email}</FormLabel>
                  <FormControl>
                    <Input
                      type="email"
                      inputMode="email"
                      autoComplete="username"
                      autoCapitalize="off"
                      spellCheck={false}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.auth.password}</FormLabel>
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
                  <FormLabel>{strings.setup.confirmPassword}</FormLabel>
                  <FormControl>
                    <PasswordInput autoComplete="new-password" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            {tokenRequired && (
              <FormField
                control={form.control}
                name="setup_token"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{strings.setup.setupToken}</FormLabel>
                    <FormControl>
                      <PasswordInput autoComplete="off" {...field} />
                    </FormControl>
                    <FormDescription>{strings.setup.setupTokenHint}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
          </fieldset>
          <Button type="submit" disabled={pending} className="w-full">
            {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
            {pending ? strings.setup.submitting : strings.setup.submit}
          </Button>
        </form>
      </Form>
    </AuthLayout>
  );
}
