import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useForm } from "react-hook-form";
import { useNavigate, useSearchParams } from "react-router";
import { isApiError } from "@/shared/api/errors";
import { errorMessage } from "@/shared/api/error-message";
import { setToken } from "@/shared/api/session";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { useLogin } from "../api/auth";
import { AuthLayout } from "../components/auth-layout";
import { PasswordInput } from "../components/password-input";
import { loginSchema, type LoginValues } from "../schemas/auth";

function loginErrorMessage(error: unknown): string {
  if (isApiError(error) && error.code === "invalid_credentials") return strings.auth.invalidCredentials;
  return errorMessage(error, strings.auth.signInFailed);
}

export function LoginPage() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const login = useLogin();
  const form = useForm<LoginValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: "", password: "" },
  });

  const sessionExpired = params.get("reason") === "session_expired";
  const pending = login.isPending;

  function onSubmit(values: LoginValues) {
    login.mutate(values, {
      onSuccess: (result) => {
        setToken(result.token);
        navigate(result.must_change_password ? "/change-password" : "/", { replace: true });
      },
    });
  }

  return (
    <AuthLayout title={strings.auth.signInTitle} subtitle={strings.auth.signInSubtitle}>
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate className="grid gap-4">
          {login.isError ? (
            <Alert variant="danger">
              <CircleAlertIcon aria-hidden="true" />
              <AlertDescription>{loginErrorMessage(login.error)}</AlertDescription>
            </Alert>
          ) : (
            sessionExpired && (
              <Alert variant="warning">
                <CircleAlertIcon aria-hidden="true" />
                <AlertDescription>{strings.auth.sessionExpired}</AlertDescription>
              </Alert>
            )
          )}
          <fieldset disabled={pending} className="grid gap-4">
            <FormField
              control={form.control}
              name="username"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.auth.username}</FormLabel>
                  <FormControl>
                    <Input autoComplete="username" autoCapitalize="off" autoFocus {...field} />
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
                    <PasswordInput autoComplete="current-password" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </fieldset>
          <Button type="submit" disabled={pending} className="w-full">
            {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
            {pending ? strings.auth.signingIn : strings.auth.signIn}
          </Button>
        </form>
      </Form>
    </AuthLayout>
  );
}
