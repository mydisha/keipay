import { TriangleAlertIcon } from "lucide-react";
import { useId, useState } from "react";
import { strings } from "@/shared/lib/strings";
import { Button } from "./button";
import { Checkbox } from "./checkbox";
import { CopyButton } from "./copy-button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "./dialog";
import { Label } from "./label";

export interface RevealedSecret {
  label: string;
  value: string;
}

interface SecretRevealDialogProps {
  open: boolean;
  title: string;
  description: string;
  secrets: RevealedSecret[];
  /** Called once the user confirms they saved the secrets. */
  onClose: () => void;
}

export function SecretRevealDialog(props: SecretRevealDialogProps) {
  return <Dialog open={props.open}>{props.open && <SecretRevealBody {...props} />}</Dialog>;
}

function SecretRevealBody({ title, description, secrets, onClose }: SecretRevealDialogProps) {
  const [saved, setSaved] = useState(false);
  const checkboxId = useId();

  return (
    <DialogContent
      showCloseButton={false}
      // Escape and outside clicks can't dismiss it: the secret is gone once closed.
      onEscapeKeyDown={(event) => {
        if (!saved) event.preventDefault();
        else onClose();
      }}
      onPointerDownOutside={(event) => event.preventDefault()}
      onInteractOutside={(event) => event.preventDefault()}
    >
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      <div
        role="note"
        className="flex items-start gap-2 rounded-md border border-warning-border bg-warning px-3 py-2 text-sm text-warning-foreground"
      >
        <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
        <span>{strings.secret.warning}</span>
      </div>
      <dl className="grid gap-3">
        {secrets.map((secret) => (
          <div key={secret.label} className="grid gap-1">
            <dt className="text-xs font-medium text-muted-foreground">{secret.label}</dt>
            <dd className="flex items-center gap-1 rounded-md border bg-muted px-2 py-1">
              <code className="min-w-0 flex-1 font-mono text-[0.8125rem] break-all">{secret.value}</code>
              <CopyButton value={secret.value} label={strings.common.copyValue(secret.label)} />
            </dd>
          </div>
        ))}
      </dl>
      <div className="flex items-center gap-2 max-md:min-h-11">
        <Checkbox id={checkboxId} checked={saved} onCheckedChange={(value) => setSaved(value === true)} />
        <Label htmlFor={checkboxId}>{strings.secret.saved}</Label>
      </div>
      <DialogFooter>
        <Button onClick={onClose} disabled={!saved}>
          {strings.secret.done}
        </Button>
      </DialogFooter>
    </DialogContent>
  );
}
