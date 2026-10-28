import { InboxIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { EmptyState } from "@/shared/ui/empty-state";
import { PageHeader } from "@/shared/ui/page-header";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/shared/ui/tabs";
import { OutboundDeliveries } from "../components/outbound-deliveries";

export function WebhooksPage() {
  return (
    <>
      <PageHeader title={strings.webhooks.title} description={strings.webhooks.subtitle} />
      <Tabs defaultValue="outbound">
        <TabsList>
          <TabsTrigger value="outbound">{strings.webhooks.outbound}</TabsTrigger>
          <TabsTrigger value="inbound">{strings.webhooks.inbound}</TabsTrigger>
        </TabsList>
        <TabsContent value="outbound">
          <OutboundDeliveries />
        </TabsContent>
        <TabsContent value="inbound">
          <EmptyState icon={InboxIcon} title={strings.webhooks.inboundTitle} description={strings.webhooks.inboundBody} />
        </TabsContent>
      </Tabs>
    </>
  );
}
