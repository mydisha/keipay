import { CircleAlertIcon, CircleCheckIcon, InfoIcon, Loader2Icon, TriangleAlertIcon } from "lucide-react";
import type { CSSProperties } from "react";
import { Toaster as Sonner, type ToasterProps } from "sonner";
import { useTheme } from "@/shared/lib/theme";

// Sonner styles its toasts with its own stylesheet, injected after ours, so the
// overrides below are marked important. The type shows as a tinted icon and in
// the words of the toast; the surface stays the popover paper.
const iconClass = "size-4";

export function Toaster(props: ToasterProps) {
  const { resolvedTheme } = useTheme();
  return (
    <Sonner
      theme={resolvedTheme}
      position="bottom-right"
      closeButton
      icons={{
        success: <CircleCheckIcon aria-hidden="true" className={`${iconClass} text-success-foreground`} />,
        error: <CircleAlertIcon aria-hidden="true" className={`${iconClass} text-danger-foreground`} />,
        warning: <TriangleAlertIcon aria-hidden="true" className={`${iconClass} text-warning-foreground`} />,
        info: <InfoIcon aria-hidden="true" className={`${iconClass} text-muted-foreground`} />,
        loading: <Loader2Icon aria-hidden="true" className={`${iconClass} animate-spin text-muted-foreground`} />,
      }}
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
          "--border-radius": "var(--radius-lg)",
        } as CSSProperties
      }
      toastOptions={{
        classNames: {
          toast: "items-start! gap-3! py-3.5! pr-11! pl-4! font-sans! text-sm! leading-5! shadow-lg!",
          title: "font-medium! leading-5!",
          description: "mt-0.5! text-muted-foreground!",
          icon: "m-0! mt-0.5! [&_svg]:m-0!",
          closeButton:
            "top-2.5! right-2.5! left-auto! size-7! transform-none! rounded-md! border-0! bg-transparent! text-muted-foreground! hover:bg-muted! hover:text-foreground! [&_svg]:size-3.5!",
          actionButton: "rounded-md! bg-primary! text-primary-foreground! font-medium!",
          cancelButton: "rounded-md! bg-muted! text-foreground!",
        },
      }}
      {...props}
    />
  );
}
