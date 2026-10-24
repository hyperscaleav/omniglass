---
title: "An endpoint shows its held session, and a rejected one offers a retry (#1059)"
date: 2026-10-23
refs: [1059]
---

The first console surface to read `session-state` (ADR-0150 point 2). The component's reachability
read now carries each endpoint's `id` (the address `:retry` takes), its held `session` (the latest
state, when it changed, and the `session-reason` beside it), and the `rejection` a terminal refusal
holds (the rung and class of `endpoint-rejection`; the fingerprint is dropped at the read). Three
console primitives render it, each on the component's Collection card, its Endpoints panel and the
endpoint's blade: `SessionPill` (each state its own glyph, word and soft hue on HealthBadge's recipe,
rejected amber with a refusal glyph apart from down's red, aged by `last_seen` and grey once the
endpoint has delivered nothing for the staleness window), `ReachLadder` (reachable, responsive and
authenticated bottom up, the ping's chip dashed as informational, the held rung marked and the rungs
above it untried, each chip teaching its rung through `InfoTip`, which now takes a trigger of its
own), and `SessionNotice` (the reason for a down or rejected session; for rejected, **Retry** through
the generated client, greyed with its reason without `endpoint:retry` and holding its spinner until
the session moves, and the way into the endpoint's edit, where **Edit credential** focuses the secret
input). The collection card's state reads `polls stopped` for a rejected session and `polls failing`
for a down one, where freshness alone read collecting. Rows on both lists are now kept by endpoint id
rather than by the objects a poll hands back, so an expanded row and a retry waiting on its session
survive the next poll. The dev fleet's huddle touch panel gains an NVP-over-telnet endpoint logging
in with the secret from before a password rotation (`sim_secrets`, `sim_endpoints` in the fixtures,
wired with the simulated fleet only), so `make dev-sim` shows a rejected session to read and fix, and
the `fix-a-rejected-session` walkthrough points its login at the rotated secret and watches it come
up. Proven against Postgres (`TestReachabilityCarriesTheHeldSession`: the state, reason and rung
read, the fingerprint never served, a lifted gate and a session-less endpoint absent;
`TestSeededFleetHoldsARejectedSession`), in vitest (the ladder model, the pill, the notice's retry
and its permission greying, the blade's credential edit, rows surviving a poll), and live against
the simulated panel. Host-key trust from the console (#1059's ssh half) is #1078.

The review's fixes, each with a test that failed first: `session-state` and `session-reason` join
the node's own facts the push lane refuses (a push could read a rejected session up, hiding its
Retry, or plant a reason the node never scrubbed; ADR-0158 point 5 amended); the read serves a
session only while the endpoint holds one (a standing listen task, or a driver declaring a
session), since teardown publishes nothing and a torn-down session's last state stood beside a
Retry that could change nothing; the reason is cut to 500 characters at the read; a retry in hand
lives in the query cache under the endpoint, so the Collection card's notice and the blade's hold
it together and cannot stack a second redial; the ladder no longer grows a login rung for a
session with no login; and the rows-survive-a-poll tests now let the poll land and assert DOM
identity, proven to fail against the unkeyed iteration (two review catalog entries record it).
