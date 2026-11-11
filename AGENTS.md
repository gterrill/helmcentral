# Agent Instructions

## Release Notes

- `CHANGELOG.md` is the release notes. The release workflow publishes the
  tag's section as the GitHub release body and fails the release if the tag
  has no section.
- Add an entry under `## [Unreleased]` in the same commit as any change an
  operator would notice. Refactors, tests and tooling get no entry.
- Any change that makes an operator act on upgrade (moved setting, removed
  environment variable, changed URL, rewritten stored state, anything they
  must re-enter or place again) goes under **Breaking**, and says what to do.
- Entries are written for the operator with the `help-pages` skill: no ADR
  numbers, no implementation detail.

## Standing Authorisation

- In your own worktree, carry finished work to an open PR without asking:
  commit by path, push the branch, open the PR. The `ship` skill holds the
  steps and watches the PR until it merges.
- Ask first before merging, tagging, deploying, editing the main checkout,
  or changing a design the operator agreed.
  
## Checks Before Commit And Release

- Every batch of changes passes `/code-review medium` before its commit and
  `/security-review` before it merges, with or without the `ship` skill, which
  holds the full steps. Check each finding against the code. From a worktree,
  run the security review with `sh scripts/security-review.sh <worktree>`.
  A docs-only change, as the `ship` skill defines it, skips `/security-review`.
- Tags are SemVer only, such as `v0.3.5`. Every release goes through the
  `release` skill.

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
- An isolated worktree session refuses Bash commands it cannot verify stay
  inside the worktree. Run one plain command per call with absolute paths,
  make edits with `Edit`, and put this in the brief of any subagent you start
  there.
- To check a UI change made in a worktree, follow "Preview from a worktree" in
  the `run-dashboard` skill.
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

## Documentation

- Operator help pages live in `docs/features`, `docs/how-to`, `docs/reference`,
  `docs/tutorials` and `docs/index.md`. Before writing or editing one, or a
  `CHANGELOG.md` entry, read `.claude/skills/help-pages/SKILL.md`.
- `docs/adr/` records why Helmcentral was built this way. Before writing or
  editing an ADR, read `.claude/skills/clarity-and-grace/SKILL.md`.
- A change affecting architecture or a feature contract creates or updates its
  ADR and updates the affected help page. An ADR alone leaves the help pages
  silently wrong.
- Feature specs and architecture notes stay out of the component READMEs
  (`backend/README.md`, `frontend/README.md`).

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
- Anything saved to disk, a store or a Mate transcript: **Saved Data**.
- Pointer, touch or drag handling on a map or chart: **Map And Gesture Input**.

## Modern Web Guidance

This project's Baseline target is Baseline 2024.

## Papercuts

- Maintain ~/papercuts.md, a global log shared by all sessions of anything that slowed down development. When you lose time to one mid-session, append date · symptom · fix · project. Check this file first when tooling fails mysteriously.