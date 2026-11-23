---
title: "Platform health has one name and folds to a screen"
date: 2026-11-23
status: "Accepted"
summary: "The sidebar door, the top bar and the page heading all say Platform health, the second exception to the one-word-tab rule (after User groups); the page folds every section to a one-line reading and holds what is open in the URL"
refs: [2446, 1767, 1644, 1724]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/), [Platform health](/operator/platform-health/)
- **Decision:** (#2446.) The Platform health page has one name, **Platform health**, in the sidebar door, the top bar's section label and the page's own heading, read from one constant in `nav.ts` so they cannot drift. The one-word-tab rule (#1767) gains a second exception beside User groups. The page itself folds each section (where work is lost, each server's panels, the outposts, the simulator) to one line, its health and the one number that matters, and opens it on demand; the open sections are the `open` query parameter, so a reload, a back step and a shared link keep them. The page carries no second Lab instance badge (the shell's banner says it) and no figure that reads "not wired yet": an unwired figure is removed, not shown as an empty promise.
- **Consequences:** `nav.test.ts` holds the name (the door, the top bar and the heading are one string, and "Status" names no door) and its one-word test lists the two exceptions. Walkthroughs and docs shots that read a section open it through the address (`?open=`). A closed section mounts nothing, so a folded page reads no series and binds no stream. The simulator's device table is a filtered page of rows with its system and location beside each name, which closes #1724.
- **Context:** The page was called Status in the door (#1767 made every tab one word), STATUS in the top bar, which resolves its label from the same door, and Platform health on the page. It was 13,215 px tall with every panel open and an unbounded device table at the foot, so the first screen could not answer whether the platform was healthy.
- **Not chosen:**
  - **Status everywhere.** One word keeps the old rule whole, but "Status" names nothing on its own beside Outposts and Settings, and the docs, Home's tile and every link already say Platform health.
  - **Platform.** One word too, but it collides with the Platform tile's own meaning on Home (the platform system's reading) and with the platform components the page reads.
