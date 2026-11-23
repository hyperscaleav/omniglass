import { type JSX } from "solid-js";
import Collapsible from "./ui/Collapsible";
import type { Summary, SummaryTone } from "../lib/platform_sections";

// PlatformSection: one Platform health section folded to a single line (#2446):
// its name, an optional lead (a server's verdict), and its summary, a verdict
// and the one number that matters. The section opens on demand; the caller
// holds the open state (the page keeps it in the URL), and a closed body is not
// mounted, so a closed section reads nothing it does not need.
const DOT: Record<SummaryTone, string> = {
  ok: "bg-success",
  warn: "bg-warning",
  bad: "bg-error",
  none: "bg-base-content/20",
};
const TEXT: Record<SummaryTone, string> = {
  ok: "text-base-content/70",
  warn: "text-warning",
  bad: "text-error",
  none: "text-base-content/50",
};

export default function PlatformSection(props: {
  testid: string;
  label: string;
  lead?: JSX.Element;
  summary: Summary;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: JSX.Element;
}) {
  return (
    <div data-testid={props.testid} class="rounded-box border border-base-300 bg-base-100">
      <Collapsible
        open={props.open}
        onOpenChange={props.onOpenChange}
        triggerClass="group flex w-full cursor-pointer items-center gap-2 rounded-box px-4 py-3 text-left focus:outline-none focus-visible:ring-1 focus-visible:ring-primary/60"
        contentClass="border-t border-base-300 p-4"
        title={
          <span class="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-1">
            <span class="eyebrow">{props.label}</span>{" "}
            {props.lead}{" "}
            <span class={`flex min-w-0 items-center gap-1.5 text-sm ${TEXT[props.summary.tone]}`} data-testid="section-summary" data-tone={props.summary.tone}>
              <span class={`size-2 flex-none rounded-full ${DOT[props.summary.tone]}`} aria-hidden="true" />
              {props.summary.text}
            </span>
          </span>
        }
      >
        {props.children}
      </Collapsible>
    </div>
  );
}
