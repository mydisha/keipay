import { RequirePermission } from "@/features/auth/components/can";
import { strings } from "@/shared/lib/strings";
import { PageHeader } from "@/shared/ui/page-header";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/shared/ui/tabs";
import { DeliveryDetailSheet } from "../components/delivery-detail-sheet";
import { InboundDetailSheet } from "../components/inbound-detail-sheet";
import { InboundWebhooks } from "../components/inbound-webhooks";
import { OutboundDeliveries } from "../components/outbound-deliveries";
import { useWebhooksSearch } from "../lib/webhooks-search";

/** `/webhooks?tab=outbound|inbound`: deliveries to apps and callbacks from gateways. */
export function WebhooksPage() {
  return (
    <>
      <PageHeader title={strings.webhooks.title} description={strings.webhooks.subtitle} />
      <RequirePermission permission="deliveries:read">
        <WebhookLogs />
      </RequirePermission>
    </>
  );
}

function WebhookLogs() {
  const search = useWebhooksSearch();
  const { tab, id } = search;
  return (
    <>
      <Tabs value={tab} onValueChange={(value) => search.setTab(value === "inbound" ? "inbound" : "outbound")}>
        <TabsList aria-label={strings.webhooks.tabsLabel}>
          <TabsTrigger value="outbound">{strings.webhooks.outbound}</TabsTrigger>
          <TabsTrigger value="inbound">{strings.webhooks.inbound}</TabsTrigger>
        </TabsList>
        <TabsContent value="outbound">
          <OutboundDeliveries search={search} />
        </TabsContent>
        <TabsContent value="inbound">
          <InboundWebhooks search={search} />
        </TabsContent>
      </Tabs>
      <DeliveryDetailSheet id={tab === "outbound" ? id : null} onClose={search.closeDetail} />
      <InboundDetailSheet id={tab === "inbound" ? id : null} onClose={search.closeDetail} />
    </>
  );
}
