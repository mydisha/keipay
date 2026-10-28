import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, unwrap, unwrapEmpty } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";
import { redirectForAuth } from "@/shared/api/session";
import { strings } from "@/shared/lib/strings";

export type Me = components["schemas"]["MeResponse"];
export type User = components["schemas"]["User"];
export type Permission = components["schemas"]["Permission"];
export type SetupStatus = components["schemas"]["SetupStatusResponse"];
export type LoginRequest = components["schemas"]["LoginRequest"];
export type SetupRequest = components["schemas"]["SetupRequest"];
export type ChangePasswordRequest = components["schemas"]["ChangePasswordRequest"];

export const meKey = ["me"] as const;
export const setupStatusKey = ["setup-status"] as const;

/** The signed-in user and their permissions: the one source for both. */
export function useMe() {
  return useQuery({
    queryKey: meKey,
    queryFn: () => unwrap(api.GET("/admin/me")),
  });
}

export function useSetupStatus() {
  return useQuery({
    queryKey: setupStatusKey,
    queryFn: () => unwrap(api.GET("/admin/setup")),
    // The guard shows a skeleton while this loads and then mounts the page,
    // which reads it again; refetching a failed status on that mount would
    // loop. The page's "Try again" refetches explicitly.
    retryOnMount: false,
  });
}

/** Sign-in, setup and password change all answer with the new `me`. */
function useSignedInMutation<TBody>(mutationFn: (body: TBody) => Promise<Me>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: (me) => {
      queryClient.setQueryData(meKey, me);
      queryClient.setQueryData<SetupStatus>(setupStatusKey, (status) =>
        status ? { ...status, setup_required: false } : status,
      );
    },
  });
}

export function useLogin() {
  return useSignedInMutation((body: LoginRequest) => unwrap(api.POST("/admin/login", { body })));
}

export function useSetup() {
  return useSignedInMutation((body: SetupRequest) => unwrap(api.POST("/admin/setup", { body })));
}

export function useChangePassword() {
  return useSignedInMutation((body: ChangePasswordRequest) => unwrap(api.POST("/admin/me/password", { body })));
}

/** Ends the session, then clears every cached query and goes to sign-in. */
export function useSignOut() {
  return useMutation({
    mutationFn: () => unwrapEmpty(api.POST("/admin/logout")),
    onSuccess: () => redirectForAuth("signed_out"),
    // Stay put: the session is still valid, so the user can retry.
    onError: () => toast.error(strings.auth.signOutFailed),
  });
}
