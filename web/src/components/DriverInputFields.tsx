import { For, Show, type JSX } from "solid-js";
import type { DriverSpecInput } from "../lib/drivers";

// DriverInputFields: the inputs a driver's spec declares (#813), one field
// each, labelled by the input's name with its default as the placeholder. The
// one rendering of a driver's inputs: the new-endpoint form draws all of them,
// the add-a-component flow (#2460) draws the parameters on its endpoint step
// and the credentials on their own step, through secretControl, which swaps a
// secret input's free-text field for a picker of the secrets that fit.
export default function DriverInputFields(props: {
  inputs: DriverSpecInput[];
  values: Record<string, string>;
  onChange: (name: string, value: string) => void;
  disabled?: boolean;
  idPrefix: string;
  secretControl?: (input: DriverSpecInput, id: string) => JSX.Element;
}): JSX.Element {
  return (
    <For each={props.inputs}>
      {(input) => {
        const id = `${props.idPrefix}${input.name}`;
        return (
          <div>
            <label class="eyebrow mb-1.5 block" for={id}>
              {input.name}
              <Show when={!input.required}><span class="ml-1 normal-case text-base-content/40">optional</span></Show>
            </label>
            <Show
              when={input.kind === "secret" && props.secretControl}
              fallback={
                <>
                  <input
                    id={id}
                    autocomplete="off"
                    class="input input-bordered w-full font-data"
                    value={props.values[input.name] ?? ""}
                    placeholder={input.default || (input.kind === "secret" ? `a ${input.secret_type} secret's name` : input.name)}
                    onInput={(e) => props.onChange(input.name, e.currentTarget.value)}
                    disabled={props.disabled}
                  />
                  <Show when={input.kind === "secret"}>
                    <p class="mt-1 text-[11px] text-base-content/40">A secret reference: the name of a {input.secret_type} secret, never the value itself.</p>
                  </Show>
                </>
              }
            >
              {props.secretControl!(input, id)}
            </Show>
          </div>
        );
      }}
    </For>
  );
}
