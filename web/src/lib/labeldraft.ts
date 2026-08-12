import { useQuery } from "@tanstack/solid-query";
import { api } from "../api/client";
import { describeError } from "./format";


// The draft identity render (#699, #702): what the console asks so a create
// form can show the name AND the label the platform is about to write, in
// locked fields, before the row exists.
//
// The label cannot be rendered in the browser and that is a decision rather
// than a gap (ADR-0098, ADR-0104): a rule is a Go text/template over a closed
// data map behind an AST allowlist, and a second implementation of it in
// TypeScript is exactly the defect slice 3 of this epic swept 42 hand-rolled
// copies of. The NAME used to be the exception, resolved in the browser by
// walking the type chain for a stem and writing a token where the ordinal went;
// #702 removed that, because the server had already resolved all three to
// render the label and returning them costs nothing. So the one engine answers
// both halves, over the wire, and this module is the call rather than the
// calculation.
//
// What makes that affordable is the same thing that makes it honest: the route
// allocates nothing. It reads the lowest free ordinal rather than minting one,
// takes no advisory lock and opens no write transaction, so asking on every
// picker change costs a read, not a place in the queue every create in the
// estate is waiting in.

// DraftLabel is the answer: the name, the ordinal that name was minted from,
// the label, and the rule that produced it.
//
// The ordinal is what a form posts back as expected_ordinal, and it is absent
// exactly when the caller supplied the name, because an operator-named row
// carries no ordinal at all. So "the field is locked" and "there is a
// precondition to post" are one fact rather than two.
//
// An empty label is a real state rather than an error: it means the platform
// stores nothing and the surface reads the name instead. It is reached by
// clearing the rule at every tier, rather than being where a shipped estate
// starts (each of the three kinds ships a rule as of #657). The rule field is
// what tells it apart from a rule that ran and had nothing to say.
export interface DraftLabel {
  name: string;
  ordinal?: number;
  label: string;
  rule: string;
}

// The three draft bodies. Each mirrors its own create body field for field, and
// the NAME field carries the same meaning in both: omitted is the operator
// handing the platform the pen. That is what keeps a locked field and what gets
// posted from disagreeing, since the form asks both routes the same question in
// the same shape and posts the same object to each.
//
// parent is on all three (#702) because it is half of the placement bucket the
// previewed ordinal is read from: a draft that left it out would preview the
// wrong bucket and the create beside it would be refused by the very
// precondition meant to protect it.
export interface ComponentDraft {
  product: string;
  name?: string;
  parent?: string;
  location?: string;
  system?: string;
}

export interface SystemDraft {
  system_type_id?: string;
  standard_id?: string;
  name?: string;
  parent?: string;
  location?: string;
}

export interface LocationDraft {
  location_type: string;
  name?: string;
  parent?: string;
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

// The two refusals a create form has to ACT on, read out of the RFC 9457
// errors array rather than out of the message (#702).
//
// Every other refusal these routes give means "show it and stop". These two
// have a specific recovery, and matching on the sentence to find them would tie
// the console to server copy that is free to change. The server stamps each
// with the request field the recovery acts on, which is a machine-readable
// channel the ErrorModel already publishes and the generated client already
// types.
interface ProblemDetail {
  location?: string;
  value?: unknown;
}

function details(e: unknown): ProblemDetail[] {
  const errs = (e as { errors?: ProblemDetail[] | null } | null | undefined)?.errors;
  return Array.isArray(errs) ? errs : [];
}

// nameRefused is "the platform will not name this row": a component_type chain
// with no stem, an unclassified system, a system_type chain with no stem, or a
// location_type with no name rule. All four are permanent answers rather than
// loading states, and all four mean the same thing to the form, which is that
// the operator has to type a name.
export function nameRefused(e: unknown): boolean {
  return details(e).some((d) => d.location === "body.name");
}

// ordinalTaken is "another create took the number this form was holding",
// returning the ordinal that moved so a surface can say what happened without
// waiting for the re-read. Null when the refusal is anything else, including
// the OTHER 409 a create can give (a name collision), whose recovery is the
// operator's rather than the form's.
export function ordinalTaken(e: unknown): number | null {
  const d = details(e).find((x) => x.location === "body.expected_ordinal");
  return typeof d?.value === "number" ? d.value : null;
}

// recoverFromTakenOrdinal is what a create form does with a refused submit, and
// it is one function rather than three because the recovery is the same on every
// tier: re-read the draft so the locked field shows the name that is free now,
// and hand back the sentence to put in front of the operator.
//
// Everything else falls through unchanged, which is the point of asking
// ordinalTaken rather than the status: a name collision is a 409 too, and
// re-reading the draft would tell that operator nothing.
//
// The extra sentence is the console's, not the server's: the server says what
// happened to the request, and only the form knows that it has just refreshed a
// field further down the page.
export async function recoverFromTakenOrdinal(
  e: unknown,
  refetch: () => Promise<unknown>,
): Promise<string> {
  if (ordinalTaken(e) === null) return describeError(e);
  await refetch();
  return `${describeError(e)} The name below now shows what is free; create again to take it.`;
}
