import { RequirePermission } from "@/features/auth/components/can";
import { AuditLogSection } from "../components/audit-log-section";

export function AuditLogTab() {
  return (
    <RequirePermission permission="audit:read">
      <AuditLogSection />
    </RequirePermission>
  );
}
