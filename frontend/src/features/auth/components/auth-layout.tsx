import type { ReactNode } from "react";
import { Brand } from "@/shared/ui/brand";
import { Card, CardContent } from "@/shared/ui/card";

export function AuthLayout({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <main className="flex min-h-dvh items-center justify-center bg-background px-4 py-10">
      <div className="grid w-full max-w-sm gap-6">
        <div className="grid justify-items-center gap-3 text-center">
          <Brand />
          <div className="space-y-1">
            <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
            <p className="text-sm text-muted-foreground">{subtitle}</p>
          </div>
        </div>
        <Card>
          <CardContent className="p-5">{children}</CardContent>
        </Card>
      </div>
    </main>
  );
}
