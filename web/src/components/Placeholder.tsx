import Page from "./Page";
import { Layers } from "./icons";

// The "coming soon" body for an IA section whose page lands in a later slice.
export default function Placeholder(props: { title: string; hint: string }) {
  return (
    <Page title={props.title} subtitle={props.hint}>
      <div class="card border border-base-300 bg-base-200">
        <div class="card-body items-center gap-3 py-14 text-center">
          <span class="text-base-content/30"><Layers size={28} /></span>
          <p class="text-base font-semibold">{props.title}</p>
          <span class="eyebrow">Coming soon</span>
        </div>
      </div>
    </Page>
  );
}
