---
title: "Omnibot reads the page you are on"
date: 2026-11-18
refs: [2162]
---

Every omnibot prompt now carries the page the person is on, shown as a Looking at chip above the
composer and read at send time, so it follows navigation; dismissing the chip leaves the page out of
one prompt. A detail page is named by its entity, never its uuid. The alt-click gathering, its tray
and the hand-over key are gone, along with the subject stamps on the shared primitives
([ADR-2162](/architecture/decisions/#adr-2162-omnibot-is-handed-the-page-not-what-the-person-gathers)).
