import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { meKey } from "@/features/auth/api/auth";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type User = components["schemas"]["User"];
export type UserRole = components["schemas"]["UserRole"];
export type CreateUserRequest = components["schemas"]["CreateUserRequest"];
export type UpdateUserRequest = components["schemas"]["UpdateUserRequest"];

export const USER_ROLES: readonly UserRole[] = ["owner", "admin", "viewer"];
export const USERS_PAGE_SIZE = 50;

const userKeys = { all: ["users"] as const };

export function useUsers(cursor: string | null) {
  return useQuery({
    queryKey: [...userKeys.all, { cursor }],
    queryFn: () =>
      unwrap(api.GET("/admin/users", { params: { query: { limit: USERS_PAGE_SIZE, cursor: cursor ?? undefined } } })),
    placeholderData: keepPreviousData,
  });
}

/** Every user mutation refreshes the list, and `me` in case it changed the signed-in user. */
function useUserMutation<TVariables, TData>(mutationFn: (variables: TVariables) => Promise<TData>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: userKeys.all }),
        queryClient.invalidateQueries({ queryKey: meKey }),
      ]),
  });
}

const byId = (id: string) => ({ params: { path: { id } } });

export function useCreateUser() {
  return useUserMutation((body: CreateUserRequest) => unwrap(api.POST("/admin/users", { body })));
}

export function useUpdateUser() {
  return useUserMutation(({ id, body }: { id: string; body: UpdateUserRequest }) =>
    unwrap(api.PATCH("/admin/users/{id}", { ...byId(id), body })),
  );
}

export function useDisableUser() {
  return useUserMutation((id: string) => unwrap(api.POST("/admin/users/{id}/disable", byId(id))));
}

export function useEnableUser() {
  return useUserMutation((id: string) => unwrap(api.POST("/admin/users/{id}/enable", byId(id))));
}

export function useResetUserPassword() {
  return useUserMutation((id: string) => unwrap(api.POST("/admin/users/{id}/reset-password", byId(id))));
}
