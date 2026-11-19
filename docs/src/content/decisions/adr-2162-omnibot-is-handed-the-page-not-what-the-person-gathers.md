---
title: "Omnibot is handed the page, not what the person gathers"
date: 2026-11-18
status: "Accepted"
summary: "Every omnibot prompt carries the page the person is on as one context entry, read at send time and shown as a chip the person can leave out of one prompt. The alt-click stamps, the gathered tray and the hand-over key of ADR-2122 are retired; context stays references only"
refs: [2162, 2122]
---

- **Date:** 2026-11-18 | **Status:** Accepted | **Pages:** [UI](/architecture/ui/#handing-the-console-to-omnibot), [connect an agent](/guides/agents/#omnibot-in-the-console)
- **Decision:** (#2162, superseding points 2 and 3 of [ADR-2122](#adr-2122-a-console-subject-is-a-stamp-on-the-shared-primitives-gathered-by-one-modifier-click-handler).)
  1. **The page is the context.** Each prompt carries the page the person is on, its route and query
     with a detail page's uuid replaced by its entity's name or address, as one AG-UI context entry.
     The panel reads it when the prompt is sent, so a prompt after navigating carries the new page.
  2. **A chip shows it and can drop it once.** The panel shows the page above the composer as a
     Looking at chip. Dismissing it leaves the page out of the next prompt only.
  3. **Gathering is retired.** The subject stamps on the shared primitives, the alt-click and
     shift-alt-click handler, the tray and the hand-over key are removed outright.
  Point 1 of ADR-2122 (context is references, never renders or uuids) and point 4 (triage is an
  embedded skill run by name) stand.
- **Divergence:** ADR-2122 had a person gather rows by alt-click into a tray. In use the gesture was
  undiscoverable and the page already named what the person was looking at; omnibot reads the rest
  through its own tools.
- **Not chosen:** keeping the stamps beside the page (two ways to hand context, one of them hidden);
  handing the rendered view (it would put device-written text in the model's context).
