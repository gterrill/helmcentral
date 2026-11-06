---
name: ship
description: Carry finished work from your own worktree to an open PR, then watch it until it merges. Use when a change is built and its tests pass, or the operator says ship it, commit it, or open the PR.
---

Run every step in order. Each one ends on its **done** line; a step is not
finished until that line is true. Fixes follow the Fallback and Test-First
policies in `AGENTS.md`, including findings applied with `--fix`.

## 1. Check where you are

`git rev-parse --show-toplevel` must be your own worktree, not the main
checkout, and `git status` must hold only your changes. Run `git fetch
origin` and rebase onto `origin/main`; local `main` often lags.

`/code-review` and `/security-review` review the session's own directory,
whatever a `cd` says. When a subagent built the change and this session
still sits in the main checkout, switch the session in first: `EnterWorktree`
with the worktree's path. If it refuses (a session already in another
worktree, or a subagent, whose switch never moves its parent), review
`git diff origin/main...HEAD` inside the worktree by hand for steps 5 and 7.

**Done:** all three true, and the session's directory is the worktree. If
the first two are not, stop and ask.

## 2. Release notes and docs

An operator-visible change gets an entry under `## [Unreleased]` in
`CHANGELOG.md`, written with the `help-pages` skill. Refactors, tests and
tooling get none. A change to architecture or a feature contract also
updates its ADR and the affected page under `docs/`.

**Done:** the entry is in, or the change is a refactor, test or tooling
change; and every ADR and `docs/` page the change contradicts is updated.

## 3. Simplify

For anything larger than a small edit, run `/simplify` once, before the first
review; its fixes then get reviewed with the rest. It applies its fixes,
so run it only in your own worktree or a tree holding nothing but your changes.
Its edits are refactors: no test-first, no CHANGELOG entry.

**Done:** its fixes are applied, or the change is a small edit.

## 4. Test

Run every suite the change touches:

- Backend: `make check-backend`
- Frontend: `make check-frontend`
- Help pages under `docs/`: `make docs-lint-changed`
- A change under `.github/workflows/`: `make workflow-lint`
- `CHANGELOG.md`: `sh scripts/check-changelog.sh`. A rebase merges this file
  without a conflict, so this is what catches an entry left in a released section.

A UI change is previewed now, before review, following "Preview from a
worktree" in the `run-dashboard` skill, at desktop and phone widths. Fixes the
preview prompts go through this step again.

Run the two check targets as they are, with no `tail` or `grep` on the end:
they run every step, print the pass counts, FAIL lines and log paths, and exit
non-zero on any failure. A failure under load needs confirming by rerunning
that test file alone before you call it a flake. Reproduce a failure this way:

- One frontend file: `npm --prefix <worktree>/frontend test --
  src/test/x.test.tsx`. `npx vitest` outside `frontend/` fetches an unrelated
  vitest.
- A backend failure that passes alone is not yet a flake. Reproduce it with
  `sh ../scripts/go-test-shard.sh <shard> 3` from `backend/`, the way CI deals
  the tests.
- A failure in a CI job: rerun the failed job too. Failing twice the same way
  means it is real.
- A failure in "Backend tests (shuffled)", which runs on main only: run `go
  test -short -count=1 -shuffle=<seed> .` from `backend/`, with the
  `-test.shuffle <seed>` printed in its log. A rerun draws a new seed, so a
  passing rerun proves nothing, and it is never called a flake.

**Done:** every suite you ran is green, and a UI change has been previewed at
desktop and phone widths. Keep the pass counts for the PR body.

## 5. Review

Run `/code-review medium` on the working tree. Then read the
`CODING_STANDARDS.md` sections the diff touches and check every changed line
against them. Check each finding against the code. Fix the real ones
test-first, then go back to step 4. Test-first means the new test has been run
and seen failing on the unfixed code; a builder subagent's brief asks for that
run and its hand-back shows it.

When the second `/code-review` pass still finds bugs in the function or
mechanism the first pass flagged, the design is the problem, not the next
finding. Stop fixing and ask the operator, naming the logic and the findings
it keeps producing, whether to simplify it, cut it or carry on.

A real finding gets fixed in this PR, even when it sits in code the change
only passes through. Dismiss a finding only when the code proves it wrong.

Any code change made after the review, other than a fix for its findings, goes
back to step 4 and gets reviewed again. A loop of fixes alone does not.

**Done:** every finding is fixed or shown wrong against the code, every
touched standards section has been applied to the diff, step 4 is green on the
fixed tree, and no code has changed since the review except fixes for its
findings.

## 6. Commit and open the PR

Stage by path and commit. Push the branch. Open the PR with `gh pr create
--body-file <path>`, or, when the branch already has one, the push updates it.
Write the body to a file in the session's scratchpad: the worktree guard
refuses inline bodies. The body says what the operator will notice, then gives
each design point agreed in this conversation and where it landed (file,
screen, or "dropped, because…"). End with the test pass counts. No "Generated
with" footer.

**Done:** the PR URL is in hand, and no preview server from this worktree is
still running.

## 7. Security review

Run `/security-review`. It takes no arguments and never edits code, and it
reports only HIGH and MEDIUM findings at 8/10 confidence or above. It diffs
`origin/HEAD...` in the session's directory, so it sees committed work only and
runs after step 6. Fix each confirmed finding test-first, then go back to step 4.

**Done:** the report covers this branch's commits, and it has no findings or
every finding is fixed and pushed. A report on an empty diff ran in the
wrong directory: go back to step 1.

## 8. Watch until merged

Report the PR URL and a one-paragraph summary to the operator. Then run `sh
scripts/watch-pr.sh <N> --once` as a background Bash command. The first run
reports the current line straight away. Each run after that exits only when
the line differs from the last one reported, so you are woken only on a
change. Act on the line, then run it again unless the PR is `MERGED` or
`CLOSED`. The line reads `state | mergeable | merge state | failed checks |
checks`, and the last field is `no checks yet`, `no checks after 15 min`,
`checks running` or `checks done`. A failure you have already told the operator
about does not wake you again.

`mergeable` means only that the PR has no conflicts. Call the PR ready to
merge only when the merge state is `CLEAN`. `no checks yet` means CI is queued,
so run the watcher again. `no checks after 15 min` means CI has not started:
tell the operator. `BLOCKED` with `checks done` and no failed checks means a
required check will never report: run `gh pr checks <N>` and tell the operator
which.

- A failed check: read its log with `gh run view --log-failed`, fix it,
  go back to step 4.
- `CONFLICTING`, or `BEHIND` (main has moved, and a PR must be tested against
  the latest main before it merges): rebase onto `origin/main`, go back to
  step 4. The push after a rebase is `git push --force-with-lease`.
- `CLOSED`: stop and tell the operator.
- `MERGED`: tell the operator it merged. Get the merge commit with `gh pr
  view <N> --json mergeCommit`, find its run with `gh run list --workflow CI
  --commit <sha>`, and wait on it with `gh run watch <id> --exit-status`.
  Report a red run to the operator as a real failure before picking up
  whatever comes next in the plan you are working from.

The operator merges. The PR is theirs to merge, tag and deploy.

**Done:** the PR state is `MERGED` or `CLOSED`.
