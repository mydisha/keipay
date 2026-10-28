import { EyeIcon, EyeOffIcon } from "lucide-react";
import { useState, type ComponentProps } from "react";
import { strings } from "@/shared/lib/strings";
import { Button } from "./button";
import { Input } from "./input";

interface PasswordInputProps extends Omit<ComponentProps<typeof Input>, "type"> {
  /** Accessible names of the toggle; default to "Show password" / "Hide password". */
  showLabel?: string;
  hideLabel?: string;
}

/** A masked input with a show/hide toggle, for passwords and other secrets. */
export function PasswordInput({
  showLabel = strings.common.showPassword,
  hideLabel = strings.common.hidePassword,
  ...props
}: PasswordInputProps) {
  const [visible, setVisible] = useState(false);
  return (
    <div className="relative">
      <Input type={visible ? "text" : "password"} className="pr-11" {...props} />
      <Button
        variant="ghost"
        size="icon-sm"
        className="absolute top-1/2 right-1 size-7 -translate-y-1/2 max-md:size-9 max-md:min-h-9 max-md:min-w-9"
        onClick={() => setVisible((v) => !v)}
        aria-label={visible ? hideLabel : showLabel}
        aria-pressed={visible}
        disabled={props.disabled}
      >
        {visible ? <EyeOffIcon aria-hidden="true" /> : <EyeIcon aria-hidden="true" />}
      </Button>
    </div>
  );
}
