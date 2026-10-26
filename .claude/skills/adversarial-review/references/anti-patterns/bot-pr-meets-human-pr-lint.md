## bot-pr-meets-human-pr-lint (fixed; found by the #66 review)

- **Cue:** a diff adds a bot PR source (`.github/dependabot.yml`, a Renovate config, a
  workflow that opens PRs) without walking every `pull_request` workflow that lints the
  PR's body, title or author.
- **Failure:** the bot writes its own body and title, so a check that requires something
  only a human adds fails every bot PR, permanently and from its first run. Found on #66:
  `demo-tag.yml` exits 1 on a body with no `demo:` line, which no Dependabot body has.
- **Fix:** exempt the bot by author at the job (`if: github.event.pull_request.user.login
  != 'dependabot[bot]'`), so the skipped job reads as passed and every human PR is still
  held to the rule, with a test pinning the condition (exemplar: `demo-tag.yml`, pinned in
  `web/src/demo-workflow.test.ts`). Check the title gate the same way: the bot's
  commit-message prefix has to produce a title it accepts.
