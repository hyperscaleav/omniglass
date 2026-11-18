---
title: "The MCP server serves embedded skills as prompts"
date: 2026-11-16
status: "Accepted"
summary: "A skill is one internal/mcp/skills/<name>/SKILL.md in the Agent Skills format, embedded in the binary, validated strictly at load and served as an MCP prompt; omniglass mcp skill prints it for agents that read skill files, and cmd/mcpgen renders its docs page. A skill names its tools in its body as tool references; its tools and permissions are read from the catalog, never declared, and a test fails a reference the catalog lacks. A prompt get returns the workflow with the given arguments appended, not substituted into it."
refs: [2026, 1162, 1161, 1165, 2025]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [api](/architecture/api/),
  [AI](/architecture/ai/), [connect an agent](/guides/agents/), [MCP skills](/reference/mcp/skills/)
- **Decision:** (#2026, under epic #1161.)
  1. **One primitive, one file per skill.** A skill is a directory under `internal/mcp/skills/` holding
     one `SKILL.md`: front matter (`name`, equal to the directory; `description`; `category`, one of
     operate, build, administer, report; optional prompt `arguments`), then the Markdown workflow. It
     is embedded with `//go:embed` and loaded strictly: an unknown front matter key, a name that is not
     its directory, an unknown category, a missing description, an unnamed or repeated argument, or a
     body that names no tool fails the load, and a test loads the embedded set in `make test`. The
     server registers every skill as an MCP prompt and names them in its instructions; the stdio relay
     mirrors the server's prompts as it mirrors its tools; `omniglass mcp skill` lists them and
     `omniglass mcp skill <name>` prints the file's bytes exactly, for an agent that loads skill
     files. The issue asked that the CLI and a prompt get print the same bytes; they share the
     workflow instead: the file is the front matter and then the workflow, a prompt get is that
     workflow with the arguments given appended (point 3), and a test holds the two to it. A skill
     file without its front matter is not a skill a loader can read, and a prompt with it would hand
     the agent YAML meant for the loader. #1165's driver builder ships through this, not its own mechanism.
  2. **Tools and permissions are derived, never declared.** A skill names a tool in its body as
     `` `tool:<name>` ``, and nothing else lists them: there is no `tools` or `permissions` key to
     drift. The set is parsed from the body, a test fails any reference the catalog does not hold, and
     the generated [skills reference](/reference/mcp/skills/) reads each tool's gate from the catalog,
     so a skill can never claim less than its tools need. A skill adds no permission and no path:
     every call it leads to is a tool call, gated, scoped and audited as one.
  3. **Arguments are appended, not substituted.** The issue asked for the skill's bytes with arguments
     substituted. A prompt get instead returns the workflow followed by a short list of the arguments
     given (`- component: panel-1`), in declared order; a missing required argument or an undeclared
     one is refused, since the SDK does not check them. The body then reads the same in a prompt, in a
     skill file and on the docs page, with no placeholder syntax for a skill-file loader to show raw.
  4. **Skills never ask for a credential.** A step that needs one (a fresh poll, a command) calls a
     tool the server runs with the credentials bound to the endpoint, so a skill never asks for, reads
     or repeats one, and tells the agent not to repeat anything in a tool's answer that looks like
     one. This is the skill's discipline, not a filter: a tool answers what the caller's token may
     read, and masking credentials out of an answer is the route's job.
- **Context:** The server registered tools only, and #1165 planned its first prompt as a one-off. A
  bag of tools leaves an agent to invent the order of a job each time; a skill carries the house way
  (read before you change, find the root before the symptom, ask before each write), and putting it in
  the binary ships it with the release whose tools it names.
- **Consequences:** Two skills ship: `triage-alarm` and `troubleshoot-component`, both on today's
  catalog. The alarm writes (acknowledge, snooze) and the alarm timeline are not tools until #2025
  stamps them, so `triage-alarm` proposes them for the operator to do in the console, and the issue's
  end-to-end that acknowledges an alarm over `omniglass mcp call` waits for them. A skill that needs
  component edits, tags or system membership waits for the same stamps.
- **Not chosen:** a `tools` list in the front matter (a second statement of what the body says, free to
  drift); `{{placeholder}}` substitution (point 3); serving skills as MCP resources (a prompt is the
  user-invoked shape a workflow wants, and clients list prompts beside tools); and keeping skills
  outside the binary (they would not ship with the tools they name).
