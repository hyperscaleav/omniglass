import { Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import Picker from "./ui/Picker";
import Button from "./Button";
import Drawer from "./Drawer";
import CreateSecretForm from "./CreateSecretForm";
import { Plus } from "./icons";
import { SECRETS_KEY, listSecrets } from "../lib/secrets";
import { secretOptions } from "../lib/add_component_flow";
import { can, useMe } from "../lib/auth";

// SecretPicker: choose the secret a driver input names (#2460, an instance of
// the reference pickers of #2289), filtered to the secret type the input takes,
// instead of typing a secret's name from memory. The value is the secret's
// NAME, the reference the cascade resolves; the value itself never reaches the
// console. A missing secret is created in place: the picker's create row, or
// the button under it, opens the shared secret form with the type preset, and
// the new secret is chosen when it exists.
export default function SecretPicker(props: {
  id: string;
  secretType?: string;
  value: string;
  onChange: (name: string) => void;
  disabled?: boolean;
}): JSX.Element {
  const me = useMe();
  const secrets = useQuery(() => ({ queryKey: SECRETS_KEY, queryFn: listSecrets }));
  const options = createMemo(() => secretOptions(secrets.data ?? [], props.secretType));
  const [creating, setCreating] = createSignal<string | null>(null);
  const mayCreate = () => can(me.data, "secret", "create");
  return (
    <div class="flex flex-col gap-1.5">
      <Picker
        id={props.id}
        options={options()}
        value={props.value || null}
        onChange={(v) => props.onChange(v ?? "")}
        placeholder={`Choose a ${props.secretType ?? ""} secret`.replace("  ", " ")}
        emptyText={`No ${props.secretType ?? ""} secret you can read`.replace("  ", " ")}
        disabled={props.disabled}
        class="input input-bordered flex h-auto min-h-10 w-full flex-wrap items-center gap-1 py-1 font-data"
        onCreate={mayCreate() ? (text) => setCreating(text) : undefined}
        createLabel={(text) => `Create a secret “${text}”`}
      />
      <Show when={mayCreate()}>
        <div>
          <Button intent="quiet" size="sm" icon={Plus} onClick={() => setCreating("")} disabled={props.disabled}>
            {`New ${props.secretType ?? ""} secret`.replace("  ", " ")}
          </Button>
        </div>
      </Show>
      <Drawer open={creating() !== null} onClose={() => setCreating(null)} title="New secret" above>
        <Show when={creating() !== null}>
          <CreateSecretForm
            type={props.secretType}
            label={creating() ?? undefined}
            onCreated={(s) => {
              props.onChange(s.name);
              setCreating(null);
            }}
          />
        </Show>
      </Drawer>
    </div>
  );
}
