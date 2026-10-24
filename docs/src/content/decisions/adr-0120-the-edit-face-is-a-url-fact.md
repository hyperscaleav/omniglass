---
title: "The edit face is a URL fact"
date: 2026-08-14
status: "Accepted"
summary: "`?edit=1` beside a detail address (or a blade's id param, `?u=<id>&edit=1`) is how the console expresses edit mode: deep links, refresh, and the create/row-pencil handoffs all carry the mode in the URL, behind the same `<resource>:update` the footer Edit is behind, and leaving edit strips the param via history replace. The one-shot in-memory handoffs (`pendingedit`, `openPrincipalInEdit`) are retired for one hook (`web/src/lib/editurl.ts`); the param is a consume-once intent (deriving the mode would re-enter edit in the Cancel gap), a blade honors it only when the URL also names it, and the name-to-uuid redirect keeps its query string. The groups blade keeps its one-shot until it gains an id deep link"
---

- **Date:** 2026-08-14 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/)
- **Decision:** `?edit=1` beside a detail address (or beside a blade's id param,
  `?u=<id>&edit=1`) is how the console expresses edit mode: a deep link or a refresh lands the
  edit face directly (behind the same `<resource>:update` the footer Edit is behind; without it
  the link lands read-only), leaving edit strips the param via history replace, and the
  create-as-route and row-pencil handoffs are that URL rather than a signal. The one-shot
  in-memory handoffs (`web/src/lib/pendingedit.ts`, `openPrincipalInEdit`) are retired; one hook
  (`web/src/lib/editurl.ts`) owns both directions. The name-to-uuid redirect keeps its query
  string, so a name-shaped edit link survives resolution.
- **Context:** The mode was reachable only through an in-memory one-shot set just before a
  navigate, so no URL could express "this entity, editing": a refresh mid-edit fell out of edit,
  Back behaved arbitrarily, and the docs screenshot pipeline (which declares shots as URLs)
  could not capture an edit face without click choreography (#758). The param is a consume-once
  intent rather than a state the mode derives from, because deriving would re-enter edit in the
  gap between Cancel's mode flip and the strip; a blade honors the param only when the URL also
  names it (`?u=<id>` matching), so a stale param cannot flip a blade merely clicked open. The
  groups blade keeps its own one-shot (`openGroupInEdit`) for now and adopts the param when it
  gains an id deep link of its own (#758 leaves it out of scope).
