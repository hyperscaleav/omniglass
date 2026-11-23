import { For, Show, createEffect, createMemo, createSignal, type JSX } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FieldRow from "./FieldRow";
import { Zap } from "./icons";
import { useFormActions } from "../lib/formactions";
import { COMMAND_TYPES_KEY, listCommandTypes } from "../lib/command_types";
import { PROPERTIES_KEY, listProperties } from "../lib/properties";
import { METRICS_KEY, listMetricTypes } from "../lib/metric_types";
import { commandControls, initialDraft, rangeWords, readDraft, type ControlSpec, type Draft } from "../lib/command_form";
import {
  PHASE_WORDS,
  commandKey,
  commandsPollMs,
  componentCommandsKey,
  getCommand,
  inFlight,
  issueCommand,
  phaseBadge,
  type Command,
  type IssueCommand,
} from "../lib/commands";
import { pushToast } from "../lib/toast";
import { describeError } from "../lib/format";

// IssueCommandForm (#1155): tell a component to do something. The type is one
// a driver attached to the component binds (only those reach it); its value
// and params are controls generated from the schemas of the type it targets and
// of its own params (lib/command_form, #2466): a select for an enum, a number
// with its range and unit, a switch for a boolean, free text only for a free
// string, and none for a fire-and-forget command. Issuing records the command and closes the
// drawer; a toast follows the command live (queued, delivered, awaiting its
// reply or observation, then settled, failed or timed out) and opens its row in
// the component's commands.
export default function IssueCommandForm(props: { component: string; name: string; componentId?: string; types: string[]; close: () => void }): JSX.Element {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const catalog = useQuery(() => ({ queryKey: COMMAND_TYPES_KEY, queryFn: listCommandTypes }));
  // The types a command targets describe its value (an enum, a range, a unit).
  // A caller who cannot read them gets the free-text value of old, not an error.
  const properties = useQuery(() => ({ queryKey: PROPERTIES_KEY, queryFn: listProperties, retry: false }));
  const metrics = useQuery(() => ({ queryKey: METRICS_KEY, queryFn: listMetricTypes, retry: false }));
  const [typeName, setTypeName] = createSignal(props.types.length === 1 ? props.types[0] : "");
  const [draft, setDraft] = createSignal<Draft>({});
  const [touched, setTouched] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  const chosen = createMemo(() => (catalog.data ?? []).find((t) => t.name === typeName()));
  const target = () => chosen()?.target_metric_type ?? chosen()?.target_property_type;
  const controls = createMemo(() => {
    const ct = chosen();
    return ct ? commandControls(ct, { properties: properties.data, metrics: metrics.data }) : [];
  }, [], { equals: (a, b) => JSON.stringify(a) === JSON.stringify(b) });
  const read = createMemo(() => readDraft(controls(), draft()));
  const ready = () => !!chosen() && !read().error;

  // A new command starts its controls afresh: a switch off, the rest empty.
  createEffect(() => setDraft(initialDraft(controls())));
  // A field's refusal shows once it has been typed in or the form was tried.
  const shows = (field: string) => (touched() || (draft()[field] ?? "") !== "") && !!read().errors[field];
  const set = (field: string, v: string) => setDraft((d) => ({ ...d, [field]: v }));

  useFormActions().bind({
    submitLabel: "Issue",
    submitIcon: Zap,
    submit: () => void submit(),
    busy,
    disabled: () => !ready(),
    cancel: props.close,
  });

  async function submit() {
    setTouched(true);
    if (!ready()) return;
    setBusy(true);
    setErr(null);
    try {
      const r = read();
      const body: IssueCommand = { command_type: typeName() };
      if (r.value !== undefined) body.value = r.value;
      if (r.params) body.params = r.params;
      const cmd = await issueCommand(props.component, body);
      qc.setQueryData(commandKey(props.component, cmd.id), cmd);
      await qc.invalidateQueries({ queryKey: componentCommandsKey(props.component), exact: true });
      const where = props.componentId ?? props.component;
      pushToast({
        tone: "info",
        title: `${cmd.command_type} issued to ${props.name}`,
        body: () => <CommandToastBody component={props.component} id={cmd.id} />,
        action: { label: "Open", onClick: () => navigate(`/components/${encodeURIComponent(where)}?tab=activity&command=${cmd.id}`) },
        ttlMs: 20_000,
      });
      props.close();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  const infoFor = (c: ControlSpec) =>
    c.field === "value"
      ? `What it sets: ${target()}.${c.description ? ` ${c.description}` : ""} The command settles when the component's reply or the next observation of ${target()} agrees, within its ${chosen()?.settle_window_seconds ?? 0}s settle window.`
      : c.description;

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm" data-testid="issue-command-error"><span>{err()}</span></div>
      </Show>
      <FieldRow
        label="Command"
        info="What to tell the component. Only the commands a driver attached to one of its endpoints binds are offered: those are the ones that reach it."
      >
        <select class="select select-bordered w-full font-data" aria-required="true" value={typeName()} onChange={(e) => { setTypeName(e.currentTarget.value); setTouched(false); }} disabled={busy()}>
          <option value="" disabled>Choose a command...</option>
          <For each={props.types}>{(t) => <option value={t}>{t}</option>}</For>
        </select>
      </FieldRow>
      <For each={controls()}>
        {(c) => (
          <FieldRow
            label={c.label.charAt(0).toUpperCase() + c.label.slice(1)}
            info={infoFor(c)}
            hint={(shows(c.field) ? read().errors[c.field] : undefined) ?? (c.kind === "number" ? rangeWords(c) : undefined)}
          >
            <ControlInput c={c} value={draft()[c.field] ?? ""} onChange={(v) => set(c.field, v)} onBlur={() => setTouched(true)} disabled={busy()} invalid={shows(c.field)} />
          </FieldRow>
        )}
      </For>
    </form>
  );
}

// ControlInput renders one generated control. No native min/max/required: the
// refusal is readDraft's, shown in the field's hint (validation-guard).
function ControlInput(props: { c: ControlSpec; value: string; onChange: (v: string) => void; onBlur: () => void; disabled: boolean; invalid: boolean }): JSX.Element {
  const c = props.c;
  if (c.kind === "select") {
    return (
      <select class="select select-bordered w-full font-data" aria-required={c.required} aria-invalid={props.invalid} value={props.value} onChange={(e) => props.onChange(e.currentTarget.value)} onBlur={props.onBlur} disabled={props.disabled}>
        <option value="" disabled>Choose...</option>
        <For each={c.options}>{(o) => <option value={o} selected={o === props.value}>{o}</option>}</For>
      </select>
    );
  }
  if (c.kind === "switch") {
    return (
      <input type="checkbox" role="switch" class="toggle toggle-sm" checked={props.value === "on"} onChange={(e) => props.onChange(e.currentTarget.checked ? "on" : "off")} disabled={props.disabled} />
    );
  }
  if (c.kind === "number") {
    // The unit rides in the hint under the field (its range with its unit), so
    // the input stays the one labelable element FieldRow points its label at.
    return (
      <input
        class="input input-bordered w-full font-data"
        aria-required={c.required}
        aria-invalid={props.invalid}
        inputmode={c.integer ? "numeric" : "decimal"}
        placeholder={c.unit}
        value={props.value}
        onInput={(e) => props.onChange(e.currentTarget.value)}
        onBlur={props.onBlur}
        disabled={props.disabled}
      />
    );
  }
  return (
    <input class="input input-bordered w-full font-data" aria-required={c.required} aria-invalid={props.invalid} value={props.value} onInput={(e) => props.onChange(e.currentTarget.value)} onBlur={props.onBlur} disabled={props.disabled} />
  );
}

// CommandToastBody follows one command live inside its toast: its phase, and
// once terminal, why.
function CommandToastBody(props: { component: string; id: number }): JSX.Element {
  const q = useQuery(() => ({
    queryKey: commandKey(props.component, props.id),
    queryFn: () => getCommand(props.component, props.id),
    refetchInterval: (query) => commandsPollMs(query.state.data ? [query.state.data as Command] : [{ status: "issued" }]),
  }));
  return (
    <Show when={q.data}>
      {(c) => (
        <span class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-[12px]" data-testid="command-toast">
          <span class={`badge badge-sm ${phaseBadge(c().phase)}`}>{PHASE_WORDS[c().phase]}</span>
          <Show when={!inFlight(c()) && c().reason}>
            <span class="text-base-content/70">{c().reason}</span>
          </Show>
        </span>
      )}
    </Show>
  );
}
