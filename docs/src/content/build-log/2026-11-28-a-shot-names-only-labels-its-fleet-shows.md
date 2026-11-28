---
title: "A shot or walkthrough names only component labels its fleet shows (#2847)"
date: 2026-11-28
refs: [2847, 2808]
---

#2808 stopped numbering a component that is alone in its slot ("Video Bar", not "Video Bar 1"), and a dozen
selectors and expectations in the docs shots, walkthroughs and marketing shots still waited for the old labels,
so a capture failed minutes into a lab. They now read the labels the console shows. A unit test in
`internal/devseed` fails the same mistake up front: it reads the labels each fleet shows (the dev fleet from a
seeded database, the corporate scenario from its resolved plan) and checks every component label a script
names against the rooms and systems that script runs in. Captions, comments and page prose are not checked.
