---
title: "The collection decision set (#818)"
date: 2026-10-23
refs: [818]
---

The closeout of epic #603. The four decisions the epic names are in the log, cited rather than
duplicated: spec-is-data
([ADR-0135](/architecture/decisions/#adr-0135-a-drivers-spec-is-data-one-engine-interprets-it)), the
actuation contract
([ADR-0136](/architecture/decisions/#adr-0136-actuation-is-a-per-node-pull-rendered-at-dispatch), with
settlement from the reply in
[ADR-0141](/architecture/decisions/#adr-0141-a-command-settles-when-its-evidence-arrives-from-its-reply-first-and-observation-second)),
the endpoint naming with `interface` on the docslint denylist
([ADR-0134](/architecture/decisions/#adr-0134-the-entity-an-api-is-reached-through-is-an-endpoint)), and
instantiation commands as plumbing
([ADR-0151](/architecture/decisions/#adr-0151-a-held-line-session-routes-every-inbound-line-listeners-first-answers-by-reply-shape)
point 1). One new decision records the arc's held-session proof
([ADR-0153](/architecture/decisions/#adr-0153-the-held-session-proof-is-the-line-protocol-over-tcp-other-held-transports-are-their-own-issues)):
the line protocol over `tcp`, with SSH and the other held transports deferred to #1032 through #1041 and
server-hosted ingress to #1024, and no serial transport (serial is reached through an IP-to-serial
gateway over tcp, tls, or telnet), which the collection and nodes pages' transport fences now name. The
sweep moved what the arc built out of design prose: the nodes page's status note (the command wire,
scheduled polls, held line sessions, listeners and the demux), its listen-mode, sessions, and task-queue
sections, the glossary (the endpoint row's rename note, and new listener and reply-shape rows), the
collection page's stale hand-count of seeded catalog rows, and the roadmap. Badges stay at their floors:
collection, nodes, messaging, commands, and the glossary are each Partial, every one still carrying
design.
