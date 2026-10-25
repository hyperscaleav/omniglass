import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FieldRow from "./FieldRow";
import { Zap } from "./icons";
import { useFormActions } from "../lib/formactions";
import { COMMAND_TYPES_KEY, listCommandTypes } from "../lib/command_types";
import {
  PHASE_WORDS,
  commandKey,
  commandsPollMs,
  componentCommandsKey,
  getCommand,
  inFlight,
  issueCommand,
  phaseBadge,
  readCommandValue,
  type Command,
} from "../lib/commands";
import { pushToast } from "../lib/toast";
import { describeError } from "../lib/format";

// IssueCommandForm (#1155): tell a component to do something. The type is one
// a driver attached to the component binds (only those reach it); the value is
// what it sets, a number for a metric and any value for a property, and none
// for a fire-and-forget command. Issuing records the command and closes the
// drawer; a toast follows the command live (queued, delivered, awaiting its
// reply or observation, then settled, failed or timed out) and opens its row in
// the component's commands.
export default function IssueCommandForm(props: { component: string; name: string; componentId?: string; types: string[]; close: () => void }): JSX.Element {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const catalog = useQuery(() => ({ queryKey: COMMAND_TYPES_KEY, queryFn: listCommandTypes }));
  const [typeName, setTypeName] = createSignal(props.types.length === 1 ? props.types[0] : "");
  const [typed, setTyped] = createSignal("");
  const [touched, setTouched] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  const chosen = createMemo(() => (catalog.data ?? []).find((t) => t.name === typeName()));
  const targetKind = (): "property" | "metric" | undefined =>
    chosen()?.target_metric_type ? "metric" : chosen()?.target_property_type ? "property" : undefined;
  const target = () => chosen()?.target_metric_type ?? chosen()?.target_property_type;
  const value = createMemo(() => readCommandValue(typed(), targetKind()));
  const ready = () => !!chosen() && !value().error;

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
      const body = targetKind() ? { command_type: typeName(), value: value().value } : { command_type: typeName() };
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

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm" data-testid="issue-command-error"><span>{err()}</span></div>
      </Show>
      <FieldRow
        label="Command"
        info="What to tell the component. Only the commands a driver attached to one of its endpoints binds are offered: those are the ones that reach it."
      >
        <select class="select select-bordered w-full font-data" aria-required="true" value={typeName()} onChange={(e) => { setTypeName(e.currentTarget.value); setTyped(""); setTouched(false); }} disabled={busy()}>
          <option value="" disabled>Choose a command...</option>
          <For each={props.types}>{(t) => <option value={t}>{t}</option>}</For>
        </select>
      </FieldRow>
      <Show when={targetKind()}>
        <FieldRow
          label="Value"
          info={`The ${targetKind()} it sets, ${target()}. The command settles when the component's reply or the next observation of ${target()} agrees, within its ${chosen()?.settle_window_seconds ?? 0}s settle window.`}
          hint={touched() ? value().error : undefined}
        >
          <input
            class="input input-bordered w-full font-data"
            aria-required="true"
            inputmode={targetKind() === "metric" ? "decimal" : "text"}
            value={typed()}
            onInput={(e) => setTyped(e.currentTarget.value)}
            onBlur={() => setTouched(true)}
            disabled={busy()}
          />
        </FieldRow>
      </Show>
    </form>
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
