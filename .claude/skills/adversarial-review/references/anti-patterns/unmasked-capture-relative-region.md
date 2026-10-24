## unmasked-capture-relative-region (live; found by the #785 review)

A gated screenshot renders a region whose pixels derive from the capture's own clock (a
relative age, a "since" line, a strip whose span weights divide by now) or from
identifiers minted fresh by the capture's seed. Two honest captures of an unchanged
console then differ, and the zero-tolerance shot gate (#623) reads the flap as UI drift.
Detection cue: a diff adds `Date.now()`, `durationText`, `rel(`, or a spans/weight
computation to a component any `screenshots:` frontmatter path renders, with no matching
`mask:` selector in that page's entry. Fix: a stable hook (`data-testid`) on the moving
region and a mask line in the frontmatter that paints a box the text cannot resize (see
`text-sized-mask` below), per the entity blade's declared-box since-line mask
(docs/src/content/docs/guides/operator/entities.md).
