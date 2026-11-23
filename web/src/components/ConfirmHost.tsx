import { For, Show, createEffect, createSignal, on, onCleanup } from "solid-js";
import { render } from "solid-js/web";
import { useQueryClient, type QueryClient } from "@tanstack/solid-query";
import type { DeleteBlocker } from "../api/api.gen";
import ConfirmDialog from "./ui/ConfirmDialog";
import Button from "./Button";
import { Trash } from "./icons";
import { blockerHref, claimConfirmHost, pendingConfirm, refusalOf } from "../lib/confirm_store";
import { describeError } from "../lib/format";
import { deleteComponent } from "../lib/components";
import { deleteSystem } from "../lib/systems";
import { deleteLocation } from "../lib/locations";
import { deleteEndpoint } from "../lib/endpoints";

// ConfirmHost draws the console's one destructive confirm (#2463): whatever
// lib/confirm has open, in the one ConfirmDialog. AppRoot mounts it under the
// router, so a blocker's link navigates in place; lib/confirm self-mounts a
// bare one (no router, no query cache) where none is mounted.
//
// A request with run is acted on here. While it runs the dialog holds; a
// failure keeps it open with the reason; a refused delete that names its
// blockers lists them, each a link to its own page and a two-step Delete of
// its own, so clearing the way and retrying never leaves the dialog. Nothing
// here cascades: each blocker goes by its own kind's delete, under its own
// permission, and may itself be refused (its reason shows on its row).

const KIND_LABEL: Record<DeleteBlocker["kind"], string> = {
  component: "Component",
  endpoint: "Endpoint",
  system: "System",
  location: "Location",
};

// removeBlocker deletes one blocker by its own kind's route, addressed by uuid.
export function removeBlocker(b: Pick<DeleteBlocker, "kind" | "id">): Promise<void> {
  switch (b.kind) {
    case "component":
      return deleteComponent(b.id);
    case "system":
      return deleteSystem(b.id);
    case "location":
      return deleteLocation(b.id);
    case "endpoint":
      return deleteEndpoint(b.id);
  }
}

export default function ConfirmHost(props: { navigate?: (href: string) => void }) {
  if (props.navigate) onCleanup(claimConfirmHost());
  let qc: QueryClient | undefined;
  try {
    qc = useQueryClient();
  } catch {
    qc = undefined;
  }
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);
  const [blockers, setBlockers] = createSignal<DeleteBlocker[]>([]);
  const [hidden, setHidden] = createSignal(0);
  const [refused, setRefused] = createSignal(false);

  // A new request starts clean.
  createEffect(
    on(pendingConfirm, () => {
      setBusy(false);
      setError(null);
      setBlockers([]);
      setHidden(0);
      setRefused(false);
    }),
  );

  const close = () => pendingConfirm()?.resolve(false);

  const act = async () => {
    const req = pendingConfirm();
    if (!req) return;
    if (!req.run) return req.resolve(true);
    setBusy(true);
    setError(null);
    try {
      await req.run();
      req.resolve(true);
    } catch (e) {
      const r = refusalOf(e);
      setRefused(!!r);
      setBlockers(r?.blockers ?? []);
      setHidden(r?.hidden ?? 0);
      setError(describeError(e));
    } finally {
      setBusy(false);
    }
  };

  const follow = (e: MouseEvent, href: string) => {
    if (!props.navigate) return;
    e.preventDefault();
    close();
    props.navigate(href);
  };

  const removed = (b: DeleteBlocker) => {
    setBlockers((xs) => xs.filter((x) => x.id !== b.id));
    void qc?.invalidateQueries();
  };

  return (
    <ConfirmDialog
      open={!!pendingConfirm()}
      onClose={close}
      title={pendingConfirm()?.title ?? ""}
      confirmLabel={pendingConfirm()?.confirmLabel ?? "Confirm"}
      confirmIcon={pendingConfirm()?.tone === "danger" ? Trash : undefined}
      tone={pendingConfirm()?.tone ?? "action"}
      onConfirm={() => void act()}
      busy={busy()}
      // A refusal that names its blockers reads as the list below, the
      // server's sentence its lede; any other failure is the rail's alert.
      error={refused() ? null : error()}
    >
      <Show when={pendingConfirm()?.body}>
        <p class="text-sm">{pendingConfirm()?.body}</p>
      </Show>
      <Show when={refused()}>
        <section class="mt-4" aria-label="In the way" data-testid="delete-blockers">
          <h3 class="mb-1 text-xs font-semibold uppercase tracking-wide text-base-content/60">In the way</h3>
          <p role="status" class="mb-2 text-sm text-base-content/70">{error()}</p>
          <Show
            when={blockers().length > 0 || hidden() > 0}
            fallback={<p class="text-sm text-base-content/70">Nothing left in the way. Try again.</p>}
          >
            <ul class="divide-y divide-base-300 rounded-box border border-base-300">
              <For each={blockers()}>
                {(b) => <BlockerRow blocker={b} follow={follow} onRemoved={() => removed(b)} />}
              </For>
              <Show when={hidden() > 0}>
                <li class="px-3 py-2 text-sm text-base-content/70">
                  {hidden() === 1 ? "1 more" : `${hidden()} more`} you cannot see from here. Someone who can will have to clear{" "}
                  {hidden() === 1 ? "it" : "them"}.
                </li>
              </Show>
            </ul>
          </Show>
        </section>
      </Show>
    </ConfirmDialog>
  );
}

function BlockerRow(props: { blocker: DeleteBlocker; follow: (e: MouseEvent, href: string) => void; onRemoved: () => void }) {
  const [armed, setArmed] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const href = () => blockerHref(props.blocker);
  const remove = async () => {
    setBusy(true);
    setErr(null);
    try {
      await removeBlocker(props.blocker);
      props.onRemoved();
    } catch (e) {
      setErr(describeError(e));
      setArmed(false);
    } finally {
      setBusy(false);
    }
  };
  return (
    <li class="flex flex-col gap-1 px-3 py-2" data-blocker={`${props.blocker.kind}:${props.blocker.name}`}>
      <div class="flex items-center gap-2">
        <span class="badge badge-ghost badge-sm flex-none">{KIND_LABEL[props.blocker.kind]}</span>
        <span class="min-w-0 flex-1 truncate text-sm">
          <Show when={href()} fallback={<span>{props.blocker.label}</span>}>
            <a class="link" href={href()} onClick={(e) => props.follow(e, href()!)}>
              {props.blocker.label}
            </a>
          </Show>
        </span>
        <Show
          when={armed()}
          fallback={
            <Button size="xs" intent="quiet" icon={Trash} onClick={() => setArmed(true)} label={`Delete ${props.blocker.kind} ${props.blocker.label}`}>
              Delete
            </Button>
          }
        >
          <Button size="xs" intent="quiet" onClick={() => setArmed(false)} disabled={busy()}>
            Keep
          </Button>
          <Button size="xs" intent="danger" loading={busy()} onClick={() => void remove()} label={`Confirm delete ${props.blocker.kind} ${props.blocker.label}`}>
            Delete {props.blocker.kind}
          </Button>
        </Show>
      </div>
      <Show when={err()}>
        <p class="text-xs text-error">{err()}</p>
      </Show>
    </li>
  );
}

// mountConfirmHost renders a bare host on document.body (no router: links
// are plain hrefs) and returns its disposer.
export function mountConfirmHost(): () => void {
  const el = document.createElement("div");
  el.dataset.confirmHost = "";
  document.body.appendChild(el);
  const dispose = render(() => <ConfirmHost />, el);
  return () => {
    dispose();
    el.remove();
  };
}
