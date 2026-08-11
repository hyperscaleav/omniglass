import { useQuery } from "@tanstack/solid-query";
import { api } from "../api/client";


// The draft label render (#699): what the console asks so a create form can
// show the label the platform is about to write, in a locked field, before the
// row exists.
//
// The label cannot be rendered in the browser and that is a decision rather
// than a gap (ADR-0098, ADR-0104): a rule is a Go text/template over a closed
// data map behind an AST allowlist, and a second implementation of it in
// TypeScript is exactly the defect slice 3 of this epic swept 42 hand-rolled
// copies of. So the one engine answers, over the wire, and this module is the
// call rather than the calculation.
//
// What makes that affordable is the same thing that makes it honest: the route
// allocates nothing. It takes no advisory lock and opens no write transaction,
// so asking on every picker change costs a read, not a place in the queue every
// create in the estate is waiting in.

// DraftLabel is the answer: the label, and the rule that produced it.
//
// An empty label is a real, common state rather than an error. No location rule
// ships at any tier, so every location create form is in it, and it means the
// platform stores nothing and the surface reads the name instead. The rule is
// what tells that apart from a rule that ran and had nothing to say.
export interface DraftLabel {
  label: string;
  rule: string;
}

// The three draft bodies. Each mirrors its own create body field for field, and
// the NAME field carries the same meaning in both: omitted is the operator
// handing the platform the pen. That is what keeps a locked field and what gets
// posted from disagreeing, since the form asks both routes the same question in
// the same shape and posts the same object to each.
export interface ComponentDraft {
  product: string;
  name?: string;
  location?: string;
  system?: string;
}

export interface SystemDraft {
  system_type_id?: string;
  standard_id?: string;
  name?: string;
  location?: string;
}

export interface LocationDraft {
  location_type: string;
  name?: string;
}

export type EstateDraft =
  | { kind: "component"; body: ComponentDraft }
  | { kind: "system"; body: SystemDraft }
  | { kind: "location"; body: LocationDraft };

export async function renderComponentLabel(body: ComponentDraft): Promise<DraftLabel> {
  const { data, error } = await api.POST("/components:renderLabel", { body });
  if (error) throw error;
  return data as DraftLabel;
}

export async function renderSystemLabel(body: SystemDraft): Promise<DraftLabel> {
  const { data, error } = await api.POST("/systems:renderLabel", { body });
  if (error) throw error;
  return data as DraftLabel;
}

export async function renderLocationLabel(body: LocationDraft): Promise<DraftLabel> {
  const { data, error } = await api.POST("/locations:renderLabel", { body });
  if (error) throw error;
  return data as DraftLabel;
}

export function renderDraftLabel(d: EstateDraft): Promise<DraftLabel> {
  switch (d.kind) {
    case "component":
      return renderComponentLabel(d.body);
    case "system":
      return renderSystemLabel(d.body);
    default:
      return renderLocationLabel(d.body);
  }
}

// LABEL_DRAFT_KEY namespaces the draft in the query cache. The body itself is
// the rest of the key, so changing any picker asks a new question and an
// unchanged form asks none: the render is a pure function of the body, which is
// what makes it cacheable at all and is the same property that lets it skip the
// lock.
export const LABEL_DRAFT_KEY = "label-draft";

// useLabelDraft renders the label for whatever the form currently holds, or
// nothing at all when the form is not ready to be asked.
//
// draft returning null is "do not ask": an unchosen product, an unchosen
// location type, or a name field the operator has unlocked and left empty. The
// query is disabled rather than fired with a half-filled body, because a 422
// from a form mid-edit is noise an operator would read as a failure.
//
// retry is off. The refusals this route gives are 422s about the body (no stem,
// no name rule, a placement out of scope), and none of them gets better by
// being asked again; a retry would only delay the message that names the
// missing fact.
export function useLabelDraft(draft: () => EstateDraft | null) {
  return useQuery(() => {
    const d = draft();
    return {
      queryKey: [LABEL_DRAFT_KEY, d?.kind, d?.body] as const,
      queryFn: () => renderDraftLabel(d!),
      enabled: d !== null,
      retry: false,
      staleTime: 30_000,
    };
  });
}
