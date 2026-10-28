import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ModeProvider } from "@/shared/lib/mode";
import { ThemeProvider } from "@/shared/lib/theme";
import { Toaster } from "@/shared/ui/sonner";
import { TooltipProvider } from "@/shared/ui/tooltip";

export function Providers({ queryClient, children }: { queryClient: QueryClient; children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <ModeProvider>
          <TooltipProvider>
            {children}
            <Toaster />
          </TooltipProvider>
        </ModeProvider>
      </ThemeProvider>
    </QueryClientProvider>
  );
}
