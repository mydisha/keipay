import { Loader2Icon } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
import { strings } from "@/shared/lib/strings";
import { Button } from "./button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "./dialog";
import { Input } from "./input";
import { Label } from "./label";

interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  onConfirm: () => void;
  /** When set, the user must type this exact text (case-sensitive) to enable confirm. */
  confirmText?: string;
  destructive?: boolean;
  pending?: boolean;
  pendingLabel?: string;
}

export function ConfirmDialog(props: ConfirmDialogProps) {
  // Remount the body on each open so the typed text starts empty.
  return (
    <Dialog open={props.open} onOpenChange={(next) => !props.pending && props.onOpenChange(next)}>
      {props.open && <ConfirmDialogBody {...props} />}
    </Dialog>
  );
}

function ConfirmDialogBody({
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
  confirmText,
  destructive = false,
  pending = false,
  pendingLabel,
}: ConfirmDialogProps) {
  const [typed, setTyped] = useState("");
  const inputId = useId();
  const matches = confirmText === undefined || typed === confirmText;

  return (
    <DialogContent showCloseButton={!pending}>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      <form
        className="grid gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          if (matches && !pending) onConfirm();
        }}
      >
        {confirmText !== undefined && (
          <div className="grid gap-2">
            <Label htmlFor={inputId}>
              <span>
                {strings.confirm.typePrefix} <span className="font-mono font-semibold">{confirmText}</span>{" "}
                {strings.confirm.typeSuffix}
              </span>
            </Label>
            <Input
              id={inputId}
              value={typed}
              onChange={(event) => setTyped(event.target.value)}
              autoComplete="off"
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
              aria-label={strings.confirm.typeToConfirm(confirmText)}
              disabled={pending}
            />
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            {strings.common.cancel}
          </Button>
          <Button type="submit" variant={destructive ? "destructive" : "default"} disabled={!matches || pending}>
            {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
            {pending && pendingLabel ? pendingLabel : confirmLabel}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  );
}
