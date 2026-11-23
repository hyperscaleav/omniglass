import { For, Show, createEffect, createSignal, on, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import Eyebrow from "./Eyebrow";
import InfoTip from "./InfoTip";
import Button from "./Button";
import { describeError, fmtTime, rel } from "../lib/format";
import { previewBody, type RuleDraft } from "../lib/event_rules";
import { previewEventRule, type RulePreview } from "../lib/expressions";

export const PREVIEW_HINT =
  "Runs this draft against the last day of its key's samples, on the components you can read, without saving or writing anything: the edges it would have written and what it would hold in force now. Each sample is judged alone, so sustains are not applied.";

// RulePreviewPanel runs the draft as a candidate rule against the last day of
// its key's samples (POST /event-rules:preview, #959): the series it would hold
// in force now, linked to their components, and the edges it would have
// written. It writes nothing, and judges each sample alone (sustains are not
// applied), which it says.
export default function RulePreviewPanel(p: { draft: RuleDraft; ready: boolean }): JSX.Element {
  const [result, setResult] = createSignal<RulePreview | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [failed, setFailed] = createSignal<string | null>(null);
  let section: HTMLElement | undefined;
  // An answer is to the draft it was asked about: once that changes, it goes.
  createEffect(on(() => JSON.stringify(previewBody(p.draft)), () => setResult(null), { defer: true }));
  async function run() {
    setBusy(true);
    setFailed(null);
    try {
      setResult(await previewEventRule(previewBody(p.draft)));
      // The answer lands at the foot of a long drawer: bring the panel up so
      // it is read, not scrolled for.
      section?.scrollIntoView?.({ block: "start", behavior: "smooth" });
    } catch (e) {
      setResult(null);
      setFailed(describeError(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section ref={section} aria-label="Preview" class="flex flex-col gap-2 border-t border-base-300 pt-3">
      <Eyebrow
        label="Preview"
        hint={PREVIEW_HINT}
        right={
          <Button intent="quiet" size="xs" loading={busy()} disabled={!p.ready} onClick={() => void run()}>
            Preview
          </Button>
        }
      />
      <Show when={failed()}>{(m) => <div role="alert" class="alert alert-error alert-soft text-sm"><span>{m()}</span></div>}</Show>
      <Show when={result()}>
        {(r) => (
          <div data-testid="preview-result" class="flex flex-col gap-2 text-sm">
            <Show when={r().truncated}>
              <div data-testid="preview-truncated" class="alert alert-warning alert-soft text-[12px]">
                <span>
                  Judged {r().series_judged} of {r().series_total} series: the rest would pass the{" "}
                  {r().row_cap.toLocaleString("en-US")}-sample cap.
                </span>
              </div>
            </Show>
            <Show when={(r().state_unknown ?? []).length > 0}>
              <span data-testid="preview-unknown" class="inline-flex items-center gap-1 text-[11px] text-base-content/60">
                {r().state_unknown!.length} series started unknown
                <InfoTip
                  label="series that started unknown"
                  text={`No sample in the week before the window fired or cleared, so where they started is not known: their first one in the window set their state without an edge. ${(r().state_unknown ?? []).map((s) => `${s.component}${s.instance ? ` ${s.instance}` : ""}`).join(", ")}`}
                />
              </span>
            </Show>
            <Eyebrow label={`In force now (${r().in_force_now?.length ?? 0})`} />
            <Show when={(r().in_force_now ?? []).length > 0} fallback={<span class="text-[12px] text-base-content/50">Nothing in force.</span>}>
              <ul class="flex flex-col gap-0.5">
                <For each={r().in_force_now ?? []}>
                  {(s) => (
                    <li class="flex items-center gap-2 text-[12px]">
                      <A href={`/components/${s.component_id}`} class="link link-hover">{s.component}</A>
                      <Show when={s.instance}><span class="font-data text-base-content/60">{s.instance}</span></Show>
                      <span class="ml-auto text-[11px] text-base-content/50">
                        {s.since ? <span title={fmtTime(s.since)}>since {rel(s.since)}</span> : "since before the window"}
                      </span>
                    </li>
                  )}
                </For>
              </ul>
            </Show>
            <Eyebrow label={`Edges in the last ${r().window === "24h0m0s" ? "24h" : r().window} (${r().edges?.length ?? 0})`} />
            <Show when={(r().edges ?? []).length > 0} fallback={<span class="text-[12px] text-base-content/50">No edge.</span>}>
              <ul class="flex max-h-48 flex-col gap-0.5 overflow-y-auto">
                <For each={r().edges ?? []}>
                  {(e) => (
                    <li data-testid="preview-edge" class="flex items-center gap-2 text-[12px]">
                      <span class={`badge badge-sm ${e.kind === "fire" ? "badge-soft badge-warning" : "badge-soft badge-success"}`}>{e.kind}</span>
                      <span>{e.component}</span>
                      <Show when={e.instance}><span class="font-data text-base-content/60">{e.instance}</span></Show>
                      <span class="ml-auto text-[11px] text-base-content/50" title={fmtTime(e.at)}>{rel(e.at)}</span>
                    </li>
                  )}
                </For>
              </ul>
            </Show>
            <span class="text-[11px] text-base-content/50">Sustains are ignored: each sample is judged alone.</span>
          </div>
        )}
      </Show>
    </section>
  );
}
