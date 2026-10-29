import { RequirePermission } from "@/features/auth/components/can";
import { useMode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { PageHeader } from "@/shared/ui/page-header";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/shared/ui/tabs";
import { RunsTab } from "../components/runs-tab";
import { SummaryTab } from "../components/summary-tab";
import { useReconciliationSearch } from "../lib/reconciliation-search";

/** `/reconciliation?tab=summary|runs`: paid totals and reconciliation runs of the current mode. */
export function ReconciliationPage() {
  return (
    <>
      <PageHeader title={strings.reconciliation.title} description={strings.reconciliation.subtitle} />
      <RequirePermission permission="payments:read">
        <ReconciliationViews />
      </RequirePermission>
    </>
  );
}

function ReconciliationViews() {
  const search = useReconciliationSearch();
  const { mode } = useMode();
  return (
    <Tabs value={search.tab} onValueChange={(value) => search.setTab(value === "runs" ? "runs" : "summary")}>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <TabsList aria-label={strings.reconciliation.tabsLabel}>
          <TabsTrigger value="summary">{strings.reconciliation.summaryTab}</TabsTrigger>
          <TabsTrigger value="runs">{strings.reconciliation.runsTab}</TabsTrigger>
        </TabsList>
        <p className="text-xs text-muted-foreground">
          {strings.reconciliation.modeNote(strings.environments[mode]?.toLowerCase() ?? mode)}
        </p>
      </div>
      <TabsContent value="summary">
        <SummaryTab search={search} />
      </TabsContent>
      <TabsContent value="runs">
        <RunsTab search={search} />
      </TabsContent>
    </Tabs>
  );
}
