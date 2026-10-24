## swallowed-build-gate (live; found by the #480 review)

- **Cue:** a claim that some check "fails the build" backed only by a remark/rehype
  `file.fail` (or any error raised inside Astro's content-layer render); no test or
  script proves the build's exit code actually goes nonzero.
- **Failure:** the Starlight docs loader catches per-page render errors, logs them, and
  builds the page empty with exit 0, so the "gate" publishes the broken page instead of
  blocking it. Found live on the `:::design` fence gate during #480 (fixed with the
  pre-build `scripts/check-design-fences.mjs`); the `::screenshot` frontmatter gate has
  the same hole on `.md` pages.
- **Fix:** gate outside the render path: a pre-build script wired into `pnpm build`
  (exemplar: `docs/scripts/check-design-fences.mjs`) or a lint in `internal/docslint`,
  and verify the exit code, not the log line.
