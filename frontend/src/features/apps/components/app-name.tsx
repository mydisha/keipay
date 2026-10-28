import { strings } from "@/shared/lib/strings";

/** The app's name when known, else its numeric id. */
export function AppName({ id, names }: { id: number; names: Map<number, string> }) {
  const name = names.get(id);
  return <span className="truncate">{name ?? strings.common.unknownApp(id)}</span>;
}
