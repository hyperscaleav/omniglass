import { Show, type JSX } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import { X } from "./icons";
import { FROM_PARAM, TEMPLATES_KEY, listTemplates } from "../lib/templates";
import { entityLabel } from "../lib/entities";

// TemplateNotice (#1329): the one line a created row's page says after a fork
// or an instantiate lands the operator on it. The page was reached with
// `?from=<template>`; the notice names the template and says plainly what the
// model is (ADR-0071): the row is the operator's own, and nothing points back,
// so a release that changes the template never touches it. Dismissing it drops
// the param, so a reload or a shared link does not repeat it.
export default function TemplateNotice(): JSX.Element {
  const [params, setParams] = useSearchParams();
  const from = () => {
    const v = params[FROM_PARAM];
    return (Array.isArray(v) ? v[0] : v) || "";
  };
  const templates = useQuery(() => ({ queryKey: TEMPLATES_KEY, queryFn: listTemplates, enabled: !!from() }));
  const one = (k: string) => {
    const v = params[k];
    return (Array.isArray(v) ? v[0] : v) || "";
  };
  const forked = () => one("forked").split(",").filter(Boolean);
  const unattached = () => Number(one("unattached")) || 0;
  const label = () => {
    const t = (templates.data ?? []).find((x) => x.name === from());
    return t ? entityLabel(t) : from();
  };
  return (
    <Show when={from()}>
      <div role="status" data-testid="template-notice" class="alert alert-info alert-soft flex items-start gap-3 text-sm">
        <span class="flex-1">
          Started from the template <strong>{label()}</strong>. It is yours now: nothing points back at the
          template, so a later release can change it without touching this.
          <Show when={forked().length > 0}>
            {" "}It forked the standard{forked().length > 1 ? "s" : ""} <strong>{forked().join(", ")}</strong> to build to, yours now too.
          </Show>
          <Show when={unattached() > 0}>
            {" "}{unattached()} {unattached() === 1 ? "component is" : "components are"} still to attach.
          </Show>
        </span>
        <Button size="xs" square icon={X} label="Dismiss" onClick={() => setParams({ [FROM_PARAM]: undefined, forked: undefined, unattached: undefined }, { replace: true })} />
      </div>
    </Show>
  );
}
