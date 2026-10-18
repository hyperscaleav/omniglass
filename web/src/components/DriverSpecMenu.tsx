import { For, Show, type JSX } from "solid-js";
import type { DriverSpec, DriverEmit } from "../lib/drivers";

// DriverSpecMenu renders a driver's declarative spec as the menu it is (#813):
// the transport it rides, the inputs an attach must supply, and the three
// function families. Render-only and pure (props in, DOM out), so the driver
// blade teaches what attaching this driver will do before anyone does it: each
// poll says what it asks and what lands, each listener what it waits for, each
// command binding what it actuates.

function emitChips(emits: DriverEmit[]): JSX.Element {
  return (
    <div class="flex flex-wrap gap-1">
      <For each={emits}>{(em) => <span class="badge badge-outline badge-sm font-data">{em.name}</span>}</For>
    </div>
  );
}

export default function DriverSpecMenu(props: { spec: DriverSpec }): JSX.Element {
  return (
    <div class="flex flex-col gap-3 text-sm">
      <div class="flex items-center gap-2">
        <span class="eyebrow">Rides</span>
        <span class="badge badge-ghost badge-sm font-data">{props.spec.transport}</span>
      </div>

      <Show when={(props.spec.inputs ?? []).length > 0}>
        <div>
          <span class="eyebrow mb-1.5 block">Inputs</span>
          <ul class="flex flex-col gap-1">
            <For each={props.spec.inputs}>
              {(input) => (
                <li class="flex items-center gap-2">
                  <span class="font-data text-xs">{input.name}</span>
                  <span class="text-[11px] text-base-content/50">
                    {input.kind === "secret" ? `${input.secret_type} secret` : input.kind}
                  </span>
                  <Show when={input.required}>
                    <span class="badge badge-ghost badge-xs">required</span>
                  </Show>
                  <Show when={input.default}>
                    <span class="text-[11px] text-base-content/40">
                      default <span class="font-data">{input.default}</span>
                    </span>
                  </Show>
                </li>
              )}
            </For>
          </ul>
        </div>
      </Show>

      <Show when={(props.spec.polls ?? []).length > 0}>
        <div>
          <span class="eyebrow mb-1.5 block">Polls</span>
          <ul class="flex flex-col gap-2">
            <For each={props.spec.polls}>
              {(poll) => (
                <li>
                  <div class="flex items-center gap-2">
                    <span class="font-data text-xs">{poll.name}</span>
                    <span class="text-[11px] text-base-content/50">every {poll.schedule.every}</span>
                  </div>
                  {emitChips(poll.emits)}
                </li>
              )}
            </For>
          </ul>
        </div>
      </Show>

      <Show when={(props.spec.listeners ?? []).length > 0}>
        <div>
          <span class="eyebrow mb-1.5 block">Listeners</span>
          <ul class="flex flex-col gap-2">
            <For each={props.spec.listeners}>
              {(listener) => (
                <li>
                  <div class="flex items-center gap-2">
                    <span class="font-data text-xs">{listener.name}</span>
                    <span class="text-[11px] text-base-content/50">
                      {listener.match.prefix !== undefined ? `claims lines starting "${listener.match.prefix}"` : `claims lines matching ${listener.match.regex}`}
                    </span>
                  </div>
                  {emitChips(listener.emits)}
                </li>
              )}
            </For>
          </ul>
        </div>
      </Show>

      <Show when={(props.spec.commands ?? []).length > 0}>
        <div>
          <span class="eyebrow mb-1.5 block">Commands</span>
          <div class="flex flex-wrap gap-1">
            <For each={props.spec.commands}>
              {(binding) => <span class="badge badge-outline badge-sm font-data">{binding.command_type}</span>}
            </For>
          </div>
        </div>
      </Show>
    </div>
  );
}
