import { useMutation } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type LoginRequest = components["schemas"]["LoginRequest"];
export type ChangePasswordRequest = components["schemas"]["ChangePasswordRequest"];

export function useLogin() {
  return useMutation({
    mutationFn: (body: LoginRequest) => unwrap(api.POST("/admin/login", { body })),
  });
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (body: ChangePasswordRequest) => unwrap(api.POST("/admin/change-password", { body })),
  });
}
