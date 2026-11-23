---
title: "make marketing-shots records the landing page's captures"
date: 2026-11-23
refs: [2299]
---

`make marketing-shots MARKETING_DEST=<dir>` records the captures the hyperscaleav.com Omniglass page
plays, from a corporate lab at 1,000 systems with the `corporate-outage` omnibot recording: seven shots
(two stills, five loops) and a `shots.json` manifest the landing page reads. The shot list is data
(`web/e2e/marketing/shots.yaml`): steps in the walkthrough grammar without captions, and off-camera
setup that stages the faults through the API. Loops are H.264 MP4s of 5 to 9 seconds and at most
1.5 MB with a WebP poster; stills are 2x WebP. A step's `focus` and `zoom` point a camera that pans and
zooms over the loop's frames (screenshots at 2x, since a Playwright video is CSS-pixel sized), rendered offline by a Revideo (MIT) pass, centred on what each beat is about; a `wait` may play as a time-lapse (`fast`). A step's `expect` names the text its beat's frame must show, checked against the text on screen inside the frame, and each loop gets a storyboard of one still per beat. A capture-only stylesheet hides the lab banner and the
sidebar's `soon` items (marked `data-soon`) and nothing else. Preconditions fail before the lab
starts, and a setup that names a component the lab does not have fails before the first shot.
