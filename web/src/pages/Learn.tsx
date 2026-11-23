import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { A, useParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Button from "../components/Button";
import LessonLive from "../components/LessonLive";
import { ArrowRight, Check } from "../components/icons";
import { can, useMe } from "../lib/auth";
import { SIMULATION_KEY, getSimulation, simulatorConnected, type SimFleet } from "../lib/simulation";
import { usePatchMine, useSettingsMe } from "../lib/settings";
import { LAB_DOCS, LESSONS, doneSet, lessonBySlug, resolveLink, stepFor, withDone, type Lesson } from "../lib/lessons";

// Learn (#2450): guided lessons in monitoring, each walking the live console
// over a simulated component. The lessons are typed definitions
// (lib/lessons.ts) shared with the generated docs page; whether a person has
// finished one is their own learn.done setting, at the user level of the
// settings cascade, so it follows them from browser to browser.

// lessonComponent is the simulated component a lesson is walked on: the first
// one a simulator is serving, by name, so every lesson (and every reload) lands
// on the same one. Undefined with no simulator or nothing served.
export function lessonComponent(f: SimFleet | undefined): { id: string; name: string } | undefined {
  const served = (f?.outposts ?? [])
    .flatMap((o) => o.components ?? [])
    .filter((c) => c.component_id && c.serving)
    .sort((a, b) => a.component.localeCompare(b.component) || a.component_id!.localeCompare(b.component_id!));
  const c = served[0];
  return c ? { id: c.component_id!, name: c.component } : undefined;
}

function useSim() {
  const me = useMe();
  const sim = useQuery(() => ({
    queryKey: SIMULATION_KEY,
    queryFn: getSimulation,
    enabled: can(me.data, "simulation", "read"),
    refetchInterval: 10_000,
    retry: false,
  }));
  const connected = () => simulatorConnected(me.data, sim.data);
  const component = createMemo(() => (connected() ? lessonComponent(sim.data) : undefined));
  return { connected, component, settled: () => !can(me.data, "simulation", "read") || !sim.isLoading };
}

function useDone() {
  const settings = useSettingsMe();
  const patch = usePatchMine();
  const done = createMemo(() => doneSet(settings.data?.values?.learn?.done));
  return { done, patch };
}

export default function Learn(): JSX.Element {
  const { done } = useDone();
  const sim = useSim();
  return (
    <Page title="Learn" subtitle="Six short lessons in monitoring, in the order a fleet lives it. Each one drives the live console over a simulated component.">
      <Show when={sim.settled() && !sim.component()}>
        <NoSimulator />
      </Show>
      <ol class="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-4" aria-label="Lessons">
        <For each={LESSONS}>{(l, i) => <LessonCard lesson={l} n={i() + 1} done={done().has(l.slug)} />}</For>
      </ol>
    </Page>
  );
}

function LessonCard(props: { lesson: Lesson; n: number; done: boolean }): JSX.Element {
  return (
    <li class="contents">
      <A
        href={`/learn/${props.lesson.slug}`}
        data-lesson={props.lesson.slug}
        class="card border border-base-300 bg-base-200 transition-colors hover:border-primary/60"
      >
        <div class="card-body gap-2 p-5">
          <div class="flex items-center gap-2">
            <span class="eyebrow">Lesson {props.n}</span>
            <span class="ml-auto text-xs text-base-content/60 tnum">about {props.lesson.minutes} min</span>
          </div>
          <h2 class="card-title text-base">{props.lesson.title}</h2>
          <p class="text-sm text-base-content/70">{props.lesson.teaches}</p>
          <div class="mt-auto flex items-center pt-1">
            <Show when={props.done} fallback={<span class="text-xs text-base-content/50">Not done yet</span>}>
              <span data-testid="lesson-done" class="badge badge-healthy badge-sm gap-1"><Check size={12} />Done</span>
            </Show>
            <span class="ml-auto text-base-content/40" aria-hidden="true"><ArrowRight size={14} /></span>
          </div>
        </div>
      </A>
    </li>
  );
}

function NoSimulator(): JSX.Element {
  return (
    <div role="status" data-testid="learn-no-simulator" class="alert alert-info alert-soft text-sm">
      <span>
        These lessons walk a simulated component, and no simulator is connected to this install. Bring up a lab, which runs one
        beside its outpost, to follow them live.{" "}
        <a href={LAB_DOCS} target="_blank" rel="noopener" class="link">Lab instructions</a>
      </span>
    </div>
  );
}

export function LessonPage(): JSX.Element {
  const params = useParams();
  const lesson = () => lessonBySlug(params.lesson ?? "");
  return (
    <Show
      when={lesson()}
      fallback={
        <Page title="No such lesson" breadcrumb={<A href="/learn" class="link link-hover text-xs">Learn</A>}>
          <p class="text-sm text-base-content/70">There is no lesson at this address. <A href="/learn" class="link">See every lesson</A>.</p>
        </Page>
      }
    >
      {(l) => <LessonBody lesson={l()} />}
    </Show>
  );
}

function LessonBody(props: { lesson: Lesson }): JSX.Element {
  const { done, patch } = useDone();
  const sim = useSim();
  const n = () => LESSONS.indexOf(props.lesson) + 1;
  const next = () => LESSONS[n()];
  const isDone = () => done().has(props.lesson.slug);
  const [busy, setBusy] = createSignal(false);
  const [failed, setFailed] = createSignal<string | null>(null);
  const live = () => !props.lesson.needsSimulator || !!sim.component();
  async function toggle() {
    setBusy(true);
    setFailed(null);
    const r = await patch("learn", { done: withDone(done(), props.lesson.slug, !isDone()) });
    if (!r.ok) setFailed(r.message);
    setBusy(false);
  }
  return (
    <Page
      title={`${n()}. ${props.lesson.title}`}
      subtitle={props.lesson.teaches}
      breadcrumb={<A href="/learn" class="link link-hover text-xs">Learn</A>}
      actions={
        <Show
          when={isDone()}
          fallback={<Button intent="action" size="sm" icon={Check} loading={busy()} onClick={() => void toggle()}>Mark as done</Button>}
        >
          <span data-testid="lesson-done" class="badge badge-healthy gap-1"><Check size={12} />Done</span>
          <Button intent="quiet" size="sm" loading={busy()} onClick={() => void toggle()}>Mark as not done</Button>
        </Show>
      }
    >
      <Show when={failed()}>{(m) => <div role="alert" class="alert alert-error alert-soft text-sm"><span>{m()}</span></div>}</Show>
      <Show when={sim.settled() && !live()}>
        <NoSimulator />
      </Show>
      <Show when={sim.component()}>
        {(c) => (
          <p class="text-sm text-base-content/60">
            Walked on <A href={`/components/${c().id}`} class="link font-data">{c().name}</A>, a simulated component.
          </p>
        )}
      </Show>
      <ol class="flex flex-col gap-4" aria-label="Steps">
        <For each={props.lesson.steps}>
          {(raw, i) => {
            const step = () => stepFor(raw);
            const to = () => resolveLink(step().link.to, { component: sim.component()?.id });
            return (
              <li data-testid="lesson-step" class="card border border-base-300 bg-base-200">
                <div class="card-body gap-3 p-5">
                  <span class="eyebrow">Step {i() + 1}</span>
                  <h2 class="card-title text-base">{step().title}</h2>
                  <p class="max-w-3xl text-sm text-base-content/80">{step().prose}</p>
                  <Show when={step().live && live() && sim.component()}>
                    {(c) => <LessonLive kind={step().live!} componentId={c().id} componentName={c().name} />}
                  </Show>
                  <div class="flex items-center">
                    <Show
                      when={to()}
                      fallback={
                        <a href={LAB_DOCS} target="_blank" rel="noopener" class="btn btn-quiet btn-sm">Needs a simulator: lab instructions</a>
                      }
                    >
                      {(href) => (
                        <A href={href()} data-testid="lesson-do-it" class="btn btn-quiet btn-sm gap-1.5">
                          Do it in the console: {step().link.label} <ArrowRight size={14} />
                        </A>
                      )}
                    </Show>
                  </div>
                </div>
              </li>
            );
          }}
        </For>
      </ol>
      <Show when={next()}>
        {(nx) => (
          <div class="flex justify-end">
            <A href={`/learn/${nx().slug}`} data-testid="lesson-next" class="btn btn-quiet btn-sm gap-1.5">
              Next: {nx().title} <ArrowRight size={14} />
            </A>
          </div>
        )}
      </Show>
    </Page>
  );
}
