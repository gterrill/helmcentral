# Agent Instructions

## Release Tags

- Use SemVer tags only (for example `v0.3.5`).
- Do not create date-based or ad-hoc tags.
- If tagging is requested, determine the next SemVer from existing tags.

## Release Notes

- `CHANGELOG.md` is the release notes. The release workflow publishes the
  tag's section as the GitHub release body and fails the release if the tag
  has no section.
- Add an entry under `## [Unreleased]` in the same commit as any change an
  operator would notice. Refactors, tests and tooling get no entry.
- Any change that makes an operator act on upgrade (moved setting, removed
  environment variable, changed URL, rewritten stored state, anything they
  must re-enter or place again) goes under **Breaking**, and says what to do.
- Before tagging, rename `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`, add a
  fresh empty `## [Unreleased]` above it, and update the compare links at the
  foot of the file. Check it with `sh packaging/release-notes.sh vX.Y.Z`.
- Entries are written for the operator with the `help-pages` skill: no ADR
  numbers, no implementation detail.

## Standing Authorisation

- In your own worktree, carry finished work to an open PR without asking:
  commit by path, push the branch, open the PR. The `ship` skill holds the
  steps and watches the PR until it merges.
- Ask first before merging, tagging, deploying, editing the main checkout,
  or changing a design the operator agreed.
  
## Checks Before Commit And Release

- Before `/code-review medium`, run `/simplify` on batches larger than a small
  edit, then re-run the tests. It applies its fixes, so run it only in your own
  worktree or a tree holding nothing but your changes. Its edits are refactors:
  no test-first, no CHANGELOG entry.
- Run `/code-review medium` on the working tree before committing a batch of changes.
- Run `/code-review high` on the commits since the last tag before creating a release tag. For large migrations (framework or major dependency upgrades), use `/code-review ultra` instead.
- Run `/security-review` once the changes are committed, before merging a branch or creating a release tag. It diffs `origin/HEAD...` in the session's own directory, whatever a `cd` says, so run it from the checkout holding the branch's commits (step 1 of the `ship` skill covers a worktree). A report on an empty diff checked nothing.
- `/security-review` takes no arguments and never edits code. It returns a markdown report of HIGH/MEDIUM findings only and filters anything below 8/10 confidence.
- Check each finding against the code before acting on it. `high` and above report uncertain findings.
- Findings applied with `--fix` still have to follow the Fallback and Test-First policies below.

## Shared Working Tree

- This repo is often open in more than one session at once. Before your first
  edit, run `git status`. If the tree already holds changes you did not make,
  say so and ask before editing any file they touch.
- Stage by path. Never `git add -A`, `git add .` or `git commit -a` in this
  repo: they commit other sessions' unfinished work along with your own, and
  the resulting commit cannot be split once it is pushed.
- Never commit, stash, revert or check out a file you did not change. If your
  work genuinely needs a change another session is mid-way through, ask rather
  than resolving it yourself.
- For anything more than a small edit, work in your own checkout:
  `make worktree NAME=<short-name>` creates one alongside this directory with
  `frontend/node_modules` symlinked, so tests run immediately. Only one
  checkout can run the dev stack; use the one that is already up.
- To check a UI change made in a worktree, leave the dev stack alone and run
  the worktree's frontend on the next free port against the same backend:
  `cd frontend && npx vite --port 5175 --strictPort`. Vite proxies `/api` to
  the running backend on :8080, so the page shows the same live data as :5173
  and the two can be compared side by side. To see the boat's own data
  instead, prefix `VITE_API_PROXY_TARGET=http://192.168.50.240:9091`; that
  page writes to the boat, so use it to look and save on the dev stack.
  Ports 5173 and 5174 belong to the dev and e2e stacks; go up from 5175 if
  another worktree already holds it.
  Stop the server once the change is checked; `make prune-merged` will not
  remove a worktree while it runs.
- Once a branch's PR is merged, `make prune-merged` lists the worktrees and
  local and remote branches it would clear; `CONFIRM=1` deletes them. It
  skips any worktree with uncommitted changes or with a process still
  running inside it, such as a forgotten preview server, and names the pids.

## Fallback Policy

- Prefer fail-fast behavior for correctness and diagnostics paths.
- Do not add graceful fallback behavior that masks upstream data/source problems.
- If a required upstream source is missing (for example a required Furuno source), surface the issue explicitly and stop.

## Exception Rule

- Add fallback behavior only when explicitly requested.
- When fallback is approved, gate it behind a clear feature flag and emit explicit logs/telemetry that indicate fallback was used.

## Test-First Policy

- Prefer test-first development (TDD) for behavior changes and bug fixes.
- Write or update a failing test that reproduces the expected behavior before implementing the fix.
- Implement the minimal change required to make the test pass.
- Run relevant tests after the change and include the test result summary in your response.
- **Apply TDD only to functional code and business logic** (new features, bug fixes, algorithmic changes).
- **Do NOT use TDD for:**
  - Build/tooling changes (`Makefile`, `Dockerfile`, CI/CD pipelines, config files).
  - Dependency upgrades and package manager lockfile updates.
  - Pure refactoring that doesn't change public interfaces.

## Documentation Location Policy

Two audiences, two trees. Operator-facing documentation exists to explain what
Helmcentral does and how to use it. ADRs exist to record why it was built that
way. Mixing them produces feature pages that read like engineering history and
decision records that read like manuals, and both get worse.

### The user-facing tree

`docs/` follows the [Diátaxis](https://diataxis.fr/) split, indexed by
`docs/index.md`:

| Content | Location |
| --- | --- |
| What a feature does, what it gives the operator, where it stops | `docs/features/` |
| Steps to accomplish a specific task | `docs/how-to/` |
| Fields, formats, environment variables, state paths | `docs/reference/` |
| Guided first run (none yet; create only when something needs it) | `docs/tutorials/` |

A feature normally gets one `features/` page as its entry point, and grows
`how-to/` and `reference/` pages as it needs them. Do not create empty
directories or placeholder pages in advance.

#### Writing help pages

Before writing or editing these pages or a `CHANGELOG.md` entry, read
`.claude/skills/help-pages/SKILL.md` (Claude loads it as the `help-pages`
skill). It holds the reader, the voice, worked examples and the shape of each
kind of page. Run `make docs-lint` before
committing a help page. Errors fail CI, and the warnings mark sentences to
split.

### The engineering tree

`docs/adr/` holds architecture decision records and is **not part of the
Diátaxis tree.** It is the internal record: context, the decision, what was
rejected, and what a later ADR reversed. Wrong turns belong here and only here.

### The rule that keeps them apart

- **User-facing pages do not cite ADR numbers.** If a `features/`, `how-to/` or
  `reference/` page only makes sense once the reader has followed a decision
  record, the page is not finished. State the behaviour and the reason for it in
  the page's own words.
- **ADRs do not carry operator instructions.** An ADR may describe what an
  operator will see; the steps for doing it live in `docs/how-to/`.
- Component READMEs (`backend/README.md`, `frontend/README.md`) hold neither.
  No durable feature specs or architecture notes there.

### When behaviour changes

A change affecting architecture or a feature contract needs both: create or
update the ADR in `docs/adr/`, **and** update the affected page under `docs/`.
An ADR alone leaves the operator-facing docs silently wrong.

## Coding Standards

`CODING_STANDARDS.md` holds the rules for UI code and operator-facing text,
and the review step checks every diff against them. Read the matching
section before writing or reviewing:

- Any text the operator reads (UI strings, `aria-label`s, help pages,
  Mate's answers): **Tiles And Widgets**.
- Alarm titles, bodies and notifications: **Alarm Wording**.
- Equipment, Locations, Profiles, Maintenance, Wall displays, Documents and
  settings sections: **CRUD Pattern Library**.
- Dashboard tiles, Tailwind classes, colours, type sizes and chart labels:
  **High-Density Tailwind UI/UX Specification**.

## Modern Web Guidance

This project's Baseline target is Baseline 2024.

## Papercuts

- Maintain ~/papercuts.md, a global log shared by all sessions of anything that slowed down development. When you lose time to one mid-session, append date · symptom · fix · project. Check this file first when tooling fails mysteriously.