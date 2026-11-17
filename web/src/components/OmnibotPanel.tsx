import { For, Match, Show, Switch, createEffect, createSignal, onCleanup } from "solid-js";
import Drawer from "./Drawer";
import OmnibotStep from "./OmnibotStep";
import CreateSecretForm from "./CreateSecretForm";
import { Bot, Key, Plus } from "./icons";
import {
  answer,
  ask,
  fail,
  initialRun,
  reduce,
  runOmnibot,
  RunRefused,
  startConversation,
  type RunInput,
  type RunState,
} from "../lib/assistant";
import { describeError } from "../lib/format";

// OmnibotPanel is omnibot in the console (#1167): a drawer over one of the
// person's conversations, run on the server (#2121). A prompt streams back as
// omnibot's text and a card per tool call; a write the person's agent policy
// asks about pauses the run on a confirm card, and the answer resumes it. The
// run acts as the person's omnibot agent: anything they may do, it may do,
// audited as its own, and it never sees a secret's value. The credential
// button creates a secret through the secrets route and puts only its name in
// the prompt, so a credential the person types never reaches the model.

export default function OmnibotPanel(props: { open: boolean; onClose: () => void; prompt?: string }) {
  const [state, setState] = createSignal<RunState>(initialRun);
  const [conversation, setConversation] = createSignal<string>();
  const [draft, setDraft] = createSignal("");
  const [credential, setCredential] = createSignal(false);
  let controller: AbortController | undefined;
  let scroller: HTMLDivElement | undefined;
  let composer: HTMLTextAreaElement | undefined;

  // A prompt handed in (the command palette's query) seeds the composer.
  createEffect(() => {
    if (props.open && props.prompt) setDraft(props.prompt);
  });
  onCleanup(() => controller?.abort());

  // The latest item stays in view as the run streams.
  createEffect(() => {
    state();
    scroller?.scrollTo?.({ top: scroller.scrollHeight });
  });

  const busy = () => state().status === "working" || state().status === "pending";

  async function run(input: RunInput) {
    controller = new AbortController();
    try {
      let name = conversation();
      if (!name) {
        name = await startConversation();
        setConversation(name);
      }
      await runOmnibot(name, input, (ev) => setState((s) => reduce(s, ev)), controller.signal);
    } catch (err) {
      if (controller.signal.aborted) return;
      const message = err instanceof Error ? err.message : describeError(err);
      setState((s) => fail(s, message, err instanceof RunRefused));
    }
  }

  function send() {
    const text = draft().trim();
    if (!text || busy()) return;
    setDraft("");
    setState((s) => ask(s, text));
    void run({ prompt: text });
  }

  function decide(approved: boolean) {
    const id = state().interrupt?.id;
    if (!id) return;
    setState((s) => answer(s, approved));
    void run({ resume: { id, approved } });
  }

  function restart() {
    controller?.abort();
    setConversation(undefined);
    setState(initialRun);
  }

  function useSecret(name: string) {
    setCredential(false);
    setDraft((d) => `${d}${d && !d.endsWith(" ") ? " " : ""}secret ${name}`);
    composer?.focus();
  }

  const composerBar = (
    <form
      class="flex flex-col gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        send();
      }}
    >
      <textarea
        ref={composer}
        aria-label="Ask omnibot"
        class="textarea textarea-sm w-full"
        rows={2}
        placeholder="Ask omnibot about your fleet"
        value={draft()}
        disabled={busy()}
        onInput={(e) => setDraft(e.currentTarget.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            send();
          }
        }}
      />
      <div class="flex items-center justify-between gap-2">
        <button
          type="button"
          class="btn btn-quiet btn-sm"
          title="Create a secret and give omnibot its name, never its value"
          onClick={() => setCredential(true)}
        >
          <Key size={14} /> Add a credential
        </button>
        <button type="submit" class="btn btn-primary btn-sm" disabled={busy() || !draft().trim()}>
          Send
        </button>
      </div>
    </form>
  );

  return (
    <>
      <Drawer
        open={props.open}
        onClose={props.onClose}
        wide
        title={
          <span class="inline-flex items-center gap-2">
            <Bot size={16} /> Omnibot
          </span>
        }
        headerExtra={
          <Show when={state().items.length > 0}>
            <button type="button" class="btn btn-quiet btn-sm" disabled={busy()} onClick={restart}>
              <Plus size={14} /> New conversation
            </button>
          </Show>
        }
        footer={composerBar}
      >
        <div ref={scroller} class="flex flex-col gap-3">
          <Show when={state().items.length === 0}>
            <div data-testid="omnibot-empty" class="flex flex-col gap-2 text-sm text-base-content/70">
              <p>
                Omnibot acts as you, through the same API you use: it reads what you may read and changes what you may change, and the
                audit log records each call as omnibot's, on your behalf.
              </p>
              <p>
                Your agent policy decides each write: always, ask, or never. A write it asks about waits here for you to confirm. Omnibot
                never sees a secret's value; give it a credential with Add a credential, and it gets only the secret's name.
              </p>
            </div>
          </Show>
          <For each={state().items}>
            {(item) => (
              <Switch>
                <Match when={item.kind === "user" && item}>
                  {(u) => <div class="self-end max-w-[85%] rounded-box bg-primary/10 px-3 py-2 text-sm whitespace-pre-wrap">{u().text}</div>}
                </Match>
                <Match when={item.kind === "text" && item}>
                  {(t) => <div class="text-sm whitespace-pre-wrap">{t().text}</div>}
                </Match>
                <Match when={item.kind === "step" && item}>{(s) => <OmnibotStep step={s()} />}</Match>
              </Switch>
            )}
          </For>
          <Show when={state().interrupt}>
            {(i) => (
              <div data-testid="omnibot-ask" class="rounded-box border border-warning/50 bg-warning/5 p-3 text-sm">
                <p class="font-medium">Omnibot asks before it runs {i().title}</p>
                <Show when={i().destructive}>
                  <p class="mt-1 text-xs text-error">This cannot be undone.</p>
                </Show>
                <p class="mt-1 text-xs text-base-content/60">Your agent policy asks about this call. It runs as omnibot, on your behalf.</p>
                <div class="mt-2 flex gap-2">
                  <button type="button" class={`btn btn-sm ${i().destructive ? "btn-error" : "btn-primary"}`} onClick={() => decide(true)}>
                    Confirm
                  </button>
                  <button type="button" class="btn btn-quiet btn-sm" onClick={() => decide(false)}>
                    Decline
                  </button>
                </div>
              </div>
            )}
          </Show>
          <Switch>
            <Match when={state().status === "working"}>
              <div data-testid="omnibot-working" class="flex items-center gap-2 text-xs text-base-content/60">
                <span class="loading loading-dots loading-xs" /> Working
              </div>
            </Match>
            <Match when={state().status === "stopped"}>
              <div data-testid="omnibot-status" data-status="stopped" role="status" class="alert alert-soft py-1.5 text-xs">
                <span>Stopped after {state().steps} steps without an answer. Ask again to carry on from here.</span>
              </div>
            </Match>
            <Match when={state().status === "refused"}>
              <div data-testid="omnibot-status" data-status="refused" role="alert" class="alert alert-warning alert-soft py-1.5 text-xs">
                <span>Omnibot cannot run here: {state().error}</span>
              </div>
            </Match>
            <Match when={state().status === "error"}>
              <div data-testid="omnibot-status" data-status="error" role="alert" class="alert alert-error alert-soft py-1.5 text-xs">
                <span>{state().error}</span>
              </div>
            </Match>
          </Switch>
        </div>
      </Drawer>
      <Drawer open={credential()} onClose={() => setCredential(false)} title="Add a credential">
        <p class="mb-4 text-xs text-base-content/60">
          The secret is sealed on the server. Omnibot gets its name, to use where a call takes a secret, and never its value.
        </p>
        <Show when={credential()}>
          <CreateSecretForm onCreated={(s) => useSecret(s.name)} />
        </Show>
      </Drawer>
    </>
  );
}
