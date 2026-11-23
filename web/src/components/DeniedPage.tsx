import { For, Show } from "solid-js";
import { A } from "@solidjs/router";
import { Ban } from "./icons";

// DeniedPage (#2442): the face of a whole page the person may not read, the way
// DetailGate is the face of one entity that is missing or failed. It says which
// section, which permission opens it (this console also teaches the model, so it
// names the <resource>:<action> string), and the way back: Home, and Profile where
// the person's own access is listed. It discloses nothing about what exists behind
// the page: no count, no name, no hint of content. The console only explains here;
// the server's permission check is the control and has already refused.
export default function DeniedPage(props: { section: string; needs?: string[] }) {
  return (
    <section data-testid="denied-page" role="alert" class="mx-auto flex max-w-xl flex-col items-start gap-3 py-10">
      <div class="flex items-center gap-2 text-base-content/60">
        <Ban size={18} />
        <h1 class="text-lg font-semibold text-base-content">You do not have access to {props.section}</h1>
      </div>
      <Show when={props.needs && props.needs.length > 0}>
        <p data-testid="denied-needs" class="text-sm text-base-content/70">
          {props.needs!.length > 1 ? "Opening it takes any of these permissions: " : "Opening it takes the permission "}
          <For each={props.needs}>
            {(n, i) => (
              <>
                <code class="font-data">{n}</code>
                {i() < props.needs!.length - 1 ? ", " : ""}
              </>
            )}
          </For>
          . Ask an administrator for a role that carries it.
        </p>
      </Show>
      <div class="flex gap-3 text-sm">
        <A href="/" class="link link-primary">Home</A>
        <A href="/profile" class="link link-primary">Profile</A>
      </div>
      <p class="text-xs text-base-content/50">Your Profile lists the roles and permissions you hold.</p>
    </section>
  );
}
