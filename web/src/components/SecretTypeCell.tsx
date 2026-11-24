import { Show } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { SECRET_TYPES_KEY, listSecretTypes } from "../lib/secret_types";

// A secret's type as one thing everywhere (#2447): the type's label ("API Key")
// as the primary line and its name ("api-key", the address the API and CLI take)
// beneath it in the data face. The create form lists types by label; the list
// and the drawer used to print the name alone. Until the catalog loads, or for a
// type it does not carry, the name stands alone in the data face.
export default function SecretTypeCell(p: { name: string }) {
  const types = useQuery(() => ({ queryKey: SECRET_TYPES_KEY, queryFn: listSecretTypes }));
  const label = () => (types.data ?? []).find((t) => t.name === p.name)?.label;
  return (
    <Show when={label() && label() !== p.name} fallback={<span class="font-data text-xs" data-testid="secret-type">{p.name}</span>}>
      <span class="flex flex-col" data-testid="secret-type">
        <span class="text-sm">{label()}</span>
        <span class="font-data text-xs text-base-content/50">{p.name}</span>
      </span>
    </Show>
  );
}
