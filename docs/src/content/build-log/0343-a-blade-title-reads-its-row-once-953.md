---
title: "A blade title reads its row once (#953)"
date: 2026-10-20
refs: [953]
---

Opening a rule's blade requested `/event-rules` about 1,300 times in 6 seconds. The title was written
`<BladeTitle row={useRuleRow(id)} />`, and Solid compiles a JSX prop to a getter, so every read of
`row` called the hook again and minted a fresh query observer; `BladeTitle` read it inside a memo that
the list's own data re-runs, and each new observer, stale on mount, refetched the list, which re-ran
the memo. Seventeen registry blades carried the same inline form. The fix is at the primitive and
behind it: `BladeTitle` reads its accessor once in its body and tracks only the value, so even the
inline form calls the hook once; every title now calls its row hook in its own component body and
passes the variable; and a guard test (`hook-in-prop-guard`) fails on any JSX prop that calls a hook.
A regression test opens each of the seventeen blades against a stale cache and bounds the list
fetches; before the fix every one of them stormed.
