import Page from "./Page";
import { Layers } from "./icons";

// The "coming soon" body for an IA section whose page lands in a later slice,
// matching the design's SectionStub.
export default function Placeholder(props: { title: string; hint: string }) {
  return (
    <Page title={props.title} subtitle={props.hint}>
      <div class="card" style={{ padding: "56px 24px", "text-align": "center", display: "flex", "flex-direction": "column", "align-items": "center", gap: "12px" }}>
        <span style={{ color: "var(--text-faint)" }}><Layers size={28} /></span>
        <p style={{ "font-size": "15px", "font-weight": 600 }}>{props.title}</p>
        <p class="eyebrow" style={{ "margin-top": "4px" }}>Coming soon</p>
      </div>
    </Page>
  );
}
