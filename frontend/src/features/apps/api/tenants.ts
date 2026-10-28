import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type Tenant = components["schemas"]["Tenant"];
export type CreateTenantRequest = components["schemas"]["CreateTenantRequest"];
export type UpdateTenantRequest = components["schemas"]["UpdateTenantRequest"];

export const tenantKeys = { all: ["tenants"] as const };

export function useTenants() {
  return useQuery({
    queryKey: tenantKeys.all,
    queryFn: async () => (await unwrap(api.GET("/admin/tenants"))).tenants,
  });
}

/** id → name, for showing app names in other features' tables. */
export function useTenantNames(): Map<number, string> {
  const { data } = useTenants();
  return useMemo(() => new Map((data ?? []).map((tenant) => [tenant.id, tenant.name])), [data]);
}

export function useCreateTenant() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateTenantRequest) => unwrap(api.POST("/admin/tenants", { body })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: tenantKeys.all }),
  });
}

export function useUpdateTenant() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: number; body: UpdateTenantRequest }) =>
      unwrap(api.PUT("/admin/tenants/{id}", { params: { path: { id } }, body })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: tenantKeys.all }),
  });
}

export function useRotateTenant() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => unwrap(api.POST("/admin/tenants/{id}/rotate", { params: { path: { id } } })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: tenantKeys.all }),
  });
}

/** The full update body for a tenant with some fields changed. */
export function tenantUpdateBody(tenant: Tenant, changes: Partial<UpdateTenantRequest> = {}): UpdateTenantRequest {
  return {
    name: tenant.name,
    webhook_url: tenant.webhook_url,
    redirect_success_url: tenant.redirect_success_url,
    redirect_failure_url: tenant.redirect_failure_url,
    active: tenant.active,
    ...changes,
  };
}
