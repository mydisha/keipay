import { EyeIcon, EyeOffIcon } from "lucide-react";
import { useState, type ComponentProps } from "react";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";

export function PasswordInput(props: Omit<ComponentProps<typeof Input>, "type">) {
  const [visible, setVisible] = useState(false);
  return (
    <div className="relative">
      <Input type={visible ? "text" : "password"} className="pr-11" {...props} />
      <Button
        variant="ghost"
        size="icon-sm"
        className="absolute top-1/2 right-1 size-7 -translate-y-1/2 max-md:size-9 max-md:min-h-9 max-md:min-w-9"
        onClick={() => setVisible((v) => !v)}
        aria-label={visible ? strings.common.hidePassword : strings.common.showPassword}
        aria-pressed={visible}
        disabled={props.disabled}
      >
        {visible ? <EyeOffIcon aria-hidden="true" /> : <EyeIcon aria-hidden="true" />}
      </Button>
    </div>
  );
}
