import {
  For,
  Match,
  Show,
  Switch,
  createEffect,
  createSignal,
  onCleanup,
} from "solid-js";
import Button from "./Button";
import Drawer from "./Drawer";
import OmnibotStep, { StepArgs } from "./OmnibotStep";
import CreateSecretForm from "./CreateSecretForm";
import { Bot, Key, Plus, X } from "./icons";
import {
  answer,
  ask,
  closed,
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
import {
  clearTray,
  contextEntries,
  pendingTriage,
  removeSubject,
  requestTriage,
  tray,
  triageCall,
  type Subject,
} from "../lib/handover";

// OmnibotPanel is omnibot in the console (#1167): a drawer over one of the
// person's conversations, run on the server (#2121). A prompt streams back as
// omnibot's text and a card per tool call; a write the person's agent policy
// asks about pauses the run on a confirm card, and the answer resumes it. The
// run acts as the person's omnibot agent: anything they may do, it may do,
// audited as its own, and it never sees a secret's value. The credential
// button creates a secret through the secrets route and puts only its name in
// the prompt, so a credential the person types never reaches the model.
// What the person gathered from the console (#2122) waits in a tray above the
// composer and goes with the next prompt as references, never as what the
// console rendered. An alarm in the tray triages in one click: the run is the
// embedded triage-alarm skill, its text the server's, on that alarm with the
// rest of the tray as context.

// SubjectChip is one handed-over subject: its kind and its name, address or
// page, with a remove button while it waits in the tray, and a triage button
// for an alarm there.
function SubjectChip(props: {
  subject: Subject;
  onRemove?: () => void;
  onTriage?: () => void;
}) {
  return (
    <span class="badge badge-sm gap-1 max-w-full">
      <span class="text-base-content/60">{props.subject.kind}</span>
      <span class="font-data truncate">{props.subject.ref}</span>
      <Show when={props.onTriage}>
        <button
          type="button"
          class="cursor-pointer text-primary font-medium"
          aria-label={`Triage alarm ${props.subject.ref}`}
          title="Run the triage-alarm skill on this alarm"
          onClick={() => props.onTriage?.()}
        >
          Triage
        </button>
      </Show>
      <Show when={props.onRemove}>
        <button
          type="button"
          class="cursor-pointer"
          aria-label={`Remove ${props.subject.kind} ${props.subject.ref}`}
          onClick={() => props.onRemove?.()}
        >
          <X size={12} />
        </button>
      </Show>
    </span>
  );
}

export default function OmnibotPanel(props: {
  open: boolean;
  onClose: () => void;
  prompt?: string;
}) {
  const [state, setState] = createSignal<RunState>(initialRun);
  const [conversation, setConversation] = createSignal<string>();
  const [draft, setDraft] = createSignal("");
  const [credential, setCredential] = createSignal(false);
  let controller: AbortController | undefined;
  let end: HTMLDivElement | undefined;
  let composer: HTMLTextAreaElement | undefined;

  // A prompt handed in (the command palette's query) seeds the composer.
  createEffect(() => {
    if (props.open && props.prompt) setDraft(props.prompt);
  });
  onCleanup(() => controller?.abort());

  // The latest item stays in view as the run streams: the drawer's body is
  // what scrolls, so the transcript's last element brings itself into it.
  createEffect(() => {
    state();
    end?.scrollIntoView?.({ block: "end" });
  });

  const busy = () =>
    state().status === "working" || state().status === "pending";

  async function run(input: RunInput) {
    controller = new AbortController();
    try {
      let name = conversation();
      if (!name) {
        name = await startConversation();
        setConversation(name);
      }
      await runOmnibot(
        name,
        input,
        (ev) => setState((s) => reduce(s, ev)),
        controller.signal,
      );
      setState(closed);
    } catch (err) {
      if (controller.signal.aborted) return;
      const message = err instanceof Error ? err.message : describeError(err);
      setState((s) => fail(s, message, err instanceof RunRefused));
    }
  }

  function send() {
    const text = draft().trim();
    if (!text || busy()) return;
    const context = contextEntries(tray());
    setDraft("");
    clearTray();
    setState((s) => ask(s, text, context));
    void run({ prompt: text, context });
  }

  // triage runs the triage-alarm skill on an alarm, the whole tray going with
  // it as context.
  function triage(s: Subject) {
    const skill = triageCall(s);
    if (!skill || busy()) return;
    const context = contextEntries([s, ...tray()]);
    clearTray();
    setState((st) => ask(st, `Triage alarm ${s.ref}`, context));
    void run({ skill, context });
  }

  // A triage asked for from the palette runs once the panel is open.
  createEffect(() => {
    const s = pendingTriage();
    if (!props.open || !s || busy()) return;
    requestTriage(undefined);
    triage(s);
  });

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
      <Show when={tray().length > 0}>
        <div
          data-testid="omnibot-tray"
          class="flex flex-wrap items-center gap-1.5"
          aria-label="Handed to omnibot with your next prompt"
        >
          <span class="text-xs text-base-content/60">With your prompt</span>
          <For each={tray()}>
            {(s) => (
              <SubjectChip
                subject={s}
                onRemove={() => removeSubject(s)}
                onTriage={
                  triageCall(s) && !busy() ? () => triage(s) : undefined
                }
              />
            )}
          </For>
        </div>
      </Show>
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
        <Button
          intent="quiet"
          size="sm"
          icon={Key}
          title="Create a secret and give omnibot its name, never its value"
          onClick={() => setCredential(true)}
        >
          Add a credential
        </Button>
        <Button
          intent="action"
          size="sm"
          type="submit"
          disabled={busy() || !draft().trim()}
        >
          Send
        </Button>
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
            <Button
              intent="quiet"
              size="sm"
              icon={Plus}
              disabled={busy()}
              onClick={restart}
            >
              New conversation
            </Button>
          </Show>
        }
        footer={composerBar}
      >
        <div class="flex flex-col gap-3">
          <Show when={state().items.length === 0}>
            <div
              data-testid="omnibot-empty"
              class="flex flex-col gap-2 text-sm text-base-content/70"
            >
              <p>
                Omnibot acts as you, through the same API you use: it reads what
                you may read and changes what you may change, and the audit log
                records each call as omnibot's, on your behalf.
              </p>
              <p>
                Your agent policy decides each write: always, ask, or never. A
                write it asks about waits here for you to confirm. Omnibot never
                sees a secret's value; give it a credential with Add a
                credential, and it gets only the secret's name.
              </p>
            </div>
          </Show>
          <For each={state().items}>
            {(item) => (
              <Switch>
                <Match when={item.kind === "user" && item}>
                  {(u) => (
                    <div class="self-end flex max-w-[85%] flex-col items-end gap-1">
                      <Show when={u().context}>
                        {(c) => (
                          <div
                            data-testid="omnibot-handed"
                            class="flex flex-wrap justify-end gap-1"
                          >
                            <For each={c()}>
                              {(e) => (
                                <SubjectChip
                                  subject={{
                                    kind: e.description,
                                    ref: e.value,
                                  }}
                                />
                              )}
                            </For>
                          </div>
                        )}
                      </Show>
                      <div class="rounded-box bg-primary/10 px-3 py-2 text-sm whitespace-pre-wrap">
                        {u().text}
                      </div>
                    </div>
                  )}
                </Match>
                <Match when={item.kind === "text" && item}>
                  {(t) => (
                    <div class="text-sm whitespace-pre-wrap">{t().text}</div>
                  )}
                </Match>
                <Match when={item.kind === "step" && item}>
                  {(s) => (
                    <OmnibotStep
                      step={s()}
                      waiting={state().interrupt?.id === s().id}
                    />
                  )}
                </Match>
              </Switch>
            )}
          </For>
          <Show when={state().interrupt}>
            {(i) => (
              <div
                data-testid="omnibot-ask"
                class="rounded-box border border-warning/50 bg-warning/5 p-3 text-sm"
              >
                <p class="font-medium">
                  Omnibot asks before it runs {i().title}
                </p>
                <div class="mt-1.5">
                  <StepArgs args={i().arguments} testid="omnibot-ask-args" />
                </div>
                <Show when={i().destructive}>
                  <p class="mt-1 text-xs text-error">This cannot be undone.</p>
                </Show>
                <p class="mt-1 text-xs text-base-content/60">
                  Your agent policy asks about this call. It runs as omnibot, on
                  your behalf.
                </p>
                <div class="mt-2 flex gap-2">
                  <Button
                    intent={i().destructive ? "danger" : "action"}
                    size="sm"
                    onClick={() => decide(true)}
                  >
                    Confirm
                  </Button>
                  <Button
                    intent="quiet"
                    size="sm"
                    onClick={() => decide(false)}
                  >
                    Decline
                  </Button>
                </div>
              </div>
            )}
          </Show>
          <Switch>
            <Match when={state().status === "working"}>
              <div
                data-testid="omnibot-working"
                class="flex items-center gap-2 text-xs text-base-content/60"
              >
                <span class="loading loading-dots loading-xs" /> Working
              </div>
            </Match>
            <Match when={state().status === "stopped"}>
              <div
                data-testid="omnibot-status"
                data-status="stopped"
                role="status"
                class="alert alert-soft py-1.5 text-xs"
              >
                <span>
                  Stopped after {state().steps} steps without an answer. Ask
                  again to carry on from here.
                </span>
              </div>
            </Match>
            <Match when={state().status === "refused"}>
              <div
                data-testid="omnibot-status"
                data-status="refused"
                role="alert"
                class="alert alert-warning alert-soft py-1.5 text-xs"
              >
                <span>Omnibot cannot run here: {state().error}</span>
              </div>
            </Match>
            <Match when={state().status === "error"}>
              <div
                data-testid="omnibot-status"
                data-status="error"
                role="alert"
                class="alert alert-error alert-soft py-1.5 text-xs"
              >
                <span>{state().error}</span>
              </div>
            </Match>
          </Switch>
          <div ref={end} data-testid="omnibot-end" />
        </div>
      </Drawer>
      <Drawer
        open={credential()}
        onClose={() => setCredential(false)}
        title="Add a credential"
      >
        <p class="mb-4 text-xs text-base-content/60">
          The secret is sealed on the server. Omnibot gets its name, to use
          where a call takes a secret, and never its value.
        </p>
        <Show when={credential()}>
          <CreateSecretForm onCreated={(s) => useSecret(s.name)} />
        </Show>
      </Drawer>
    </>
  );
}
