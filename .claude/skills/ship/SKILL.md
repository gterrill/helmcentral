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
origin` and rebase onto `origin/main`; local `main` often lags. Run `make
hooks` once: it turns on the commit-msg hook, is safe to repeat, and the
setting is shared by every checkout, so existing worktrees and the main
checkout get it too.

`/code-review` reviews the path you give it, so step 5 passes the worktree's
path.

**Done:** the first two are true. If not, stop and ask.

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

A docs-only diff (files under `docs/` outside `docs/examples/`, `README.md`
and `CHANGELOG.md`, the same rule CI uses) skips this step and step 7. Step 4
runs `make docs-lint-changed`, `npx vitest run src/test/help-links.test.ts
src/test/docs-vocabulary.test.ts` from `<worktree>/frontend`, `go test -short
-run TestLoadHelp_RealDocsTree .` from `<worktree>/backend`, and the
`CHANGELOG.md` check when that file changed.
Step 5 is one `/code-review medium` framed as checking the prose's factual
claims against the code.

**Done:** its fixes are applied, or the change is a small edit or docs-only.

## 4. Test

This step has two forms:

- **Targeted tests**, for the code touched (`builder-brief.md` lists the
  commands), after every build or fix. A builder's hand-back shows them.
- **`make check`**, once step 5 has nothing left to fix: `make -C <worktree>
  check` in the foreground with a Bash timeout of 600000. When the tool moves
  it to the background, wait with `sh <worktree>/scripts/await-check.sh <id>`
  (Bash timeout 200000), rerunning that while it exits 3. A run queued behind
  the machine-wide lock can take a while; after an hour, tell the operator and
  keep waiting. No Monitor, `sleep`, `ps` or reads of the task output file.

A fix after that run, for `make check`, CI (step 8) or the security review
(step 7), is a **finding fix**: test-first, targeted tests, no new review, then
`make check` again. So is a rebase, minus the test. A docs-only diff runs its
own checks (step 3) in place of `make check`. The summary's `tree:` line (after
`load:` and any `power:` or `slept:` line) is the hash of the tree it tested.
`make check` runs these, in order, and carries on past a failure:

- Backend: `make check-backend`
- Frontend: `make check-frontend`
- Help pages under `docs/`: `make docs-lint-changed`
- `CHANGELOG.md`: `sh scripts/check-changelog.sh`. A rebase merges this file
  without a conflict, so this is what catches an entry left in a released section.
- Commit messages: `sh scripts/check-commit-messages.sh`, which rejects Claude
  attribution.
- The tooling scripts' own tests (`scripts/*-test.sh`, `scripts/hooks/*-test.sh`):
  a failure there is a failure like any other.

A change under `.github/workflows/` also needs `make workflow-lint`.

Any new or changed Flux runs once through `scripts/flux.sh` against the box
(`INFLUX_URL=http://<reachable-host>:8086` if the settings address is
unreachable), and its output, or the error, goes in the PR body.

A UI change is previewed now, before review, following "Preview from a
worktree" in the `run-dashboard` skill, at desktop and phone widths. Fixes the
preview prompts go through this step again.

Run the check targets as they are, with no `tail` or `grep` on the end:
they run every step, print the pass counts, FAIL lines and log paths, and exit
non-zero on any failure. For frontend tests the check reruns each failed test
file alone once and labels it in the summary, but the step still fails. The
labels explain the listed files only. A file is a flake only when all three
hold: it is labelled "failed under load, passed alone"; the first run's
`Test Files  N failed` line in the log has N equal to the number of files so
labelled; and the log shows no other error (unhandled errors, setup or
teardown errors). Then carry on past it and say so in the PR. Anything else,
such as "failed alone too", is a real failure to fix. When the check prints
`no rerun:` or `not confirmed alone:`, confirm the failure by rerunning that
file alone by hand. Reproduce a failure this way:

- One frontend file: `npm --prefix <worktree>/frontend test --
  src/test/x.test.tsx`. `npx vitest` outside `frontend/` fetches an unrelated
  vitest.
- A backend failure that passes alone is not yet a flake. Reproduce it with
  `sh ../scripts/go-test-shard.sh <shard> 3` from `backend/`, the way CI deals
  the tests.
- A failure in a CI job: rerun the failed job too. Failing twice the same way
  means it is real.
- A failure in `make check-backend` or in main's "Backend tests (shuffled)",
  both of which run the tests in random order: run `go test -short -count=1
  -shuffle=<seed> .` from `backend/`, with the `-test.shuffle <seed>` printed
  in its log. A rerun draws a new seed, so a passing rerun proves nothing, and
  it is never called a flake. When the same seed fails on `origin/main` too,
  the leak predates this change: tell the operator and carry on.

**Done:** every suite you ran is green, and a UI change has been previewed at
desktop and phone widths; after the review, that includes `make check` (or a
docs-only diff's own checks). Keep the pass counts for the PR body.

## 5. Review

Keep a Decisions list in `<worktree>/.review-decisions.md` (gitignored): the
operator's choices from AskUserQuestion, and every finding you dismissed with
its reason. Run `/code-review medium <worktree path> — read
<worktree>/.review-decisions.md first; findings it already settles are not
findings`. Then read the
`CODING_STANDARDS.md` sections the diff touches and check every changed line
against them. Check each finding against the code. Fix the real ones
test-first, then go back to step 4. Test-first means the new test has been run
and seen failing on the unfixed code; a builder subagent's brief asks for that
run and its hand-back shows it.

Every builder prompt starts with `builder-brief.md` (in this skill's
directory) pasted verbatim, worktree path filled in. Start a fresh builder for
each review round; never `SendMessage` to the old one.

When the second `/code-review` pass still finds bugs in the function or
mechanism the first pass flagged, the design is the problem, not the next
finding. Stop fixing and ask the operator, naming the logic and the findings
it keeps producing, whether to simplify it, cut it or carry on.

Otherwise a third `/code-review` pass is the last: act only on its correctness
findings (wrong behaviour, data loss, security), and put the rest under
"Follow-ups" in the PR body or the ADR. If the change still needs another pass
after that, ask the operator.

A real finding gets fixed in this PR, even when it sits in code the change
only passes through. Dismiss a finding only when the code proves it wrong.

Any code change made after the review, other than a fix for its findings or a
finding fix (step 4), goes back to step 4 and gets reviewed again. A loop of
fixes alone does not.

**Done:** every finding is fixed or shown wrong against the code, every
touched standards section has been applied to the diff, step 4 is green on the
fixed tree (its one `make check`, or the docs-only checks), and no code has changed since the review except fixes for its
findings.

## 6. Commit and open the PR

First, `sh <worktree>/scripts/tree-hash.sh <worktree>` must print the `tree:`
hash of the last green `make check`; if it does not, run step 4's `make check`
again. A docs-only diff instead needs its own checks to have run on the tree
as it stands. Stage by path and commit. Run `sh scripts/check-commit-messages.sh`, since
step 4 ran before these commits existed; a hit means reword the commit before
pushing. Push the branch. Open the PR with `gh pr create
--body-file <path>`, or, when the branch already has one, the push updates it.
Write the body to `<scratchpad>/pr-body-<branch>.md`: the worktree guard
refuses inline bodies. The body says what the operator will notice, then gives
each design point agreed in this conversation and where it landed (file,
screen, or "dropped, because…"). End with the test pass counts. No "Generated
with" footer.

**Done:** the PR URL is in hand, and no preview server from this worktree is
still running.

## 7. Security review

Run `sh scripts/security-review.sh <worktree>` (from any directory). It starts
`claude -p "/security-review"` inside the worktree, because `/security-review`
diffs the directory its session started in. The review never edits code and
reports only HIGH and MEDIUM findings at 8/10 confidence or above. It sees
committed work only, so it runs after step 6. The script fails when the branch
has no commits ahead of `origin/HEAD`. Fix each confirmed finding test-first,
then go back to step 4.

A docs-only diff skips this step (see step 3).

**Done:** the script ran and its report covers this branch's commits, and it
has no findings or every finding is fixed and pushed; or the diff is docs-only.

## 8. Watch until merged

Report the PR URL and a one-paragraph summary to the operator. Then run `sh
scripts/watch-pr.sh <N> --once` as a background Bash command. The first run
reports the current line straight away. Each run after that exits only when
the line differs from the last one reported, so you are woken only on a
change. Act on the line, then run it again unless the PR is `MERGED` or
`CLOSED`. The line reads `state | mergeable | merge state | failed checks |
checks`, and the last field is `no checks yet`, `no checks after 15 min`,
`checks running` or `checks done`. A failure you have already told the operator
about does not wake you again. A repeated completion notice from a subagent
whose state has not changed gets no reply turn.

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
- `CLEAN` with `checks done`, on a **plan PR** (a PR named in a plan file the
  operator agreed), once step 7 covers every pushed commit or the diff is
  docs-only (a push after step 7, rebase included, means running it again
  first): merge it with `gh pr merge <N> --merge`, do not start the watcher
  again, and go on as for `MERGED`. On any other PR, tell the operator it is
  ready.
- `MERGED`: tell the operator it merged. Get the merge commit with `gh pr
  view <N> --json mergeCommit`, find its run with `gh run list --workflow CI
  --commit <sha>`, and wait on it with `gh run watch <id> --exit-status`.
  Report a red run to the operator as a real failure before picking up
  whatever comes next in the plan you are working from.

Every other merge, and every tag and deploy, is the operator's.

### The next PR's lane

Once the PR is reported, the wait for CI and the merge is build time for the next
PR, when the plan you are working from names one: start it now, without
waiting for "go ahead".

- Run its design pass and put its design questions, scope among them, to the
  operator now. Its builder starts once the operator has chosen the scope.
- When the next PR builds on this one's code, start its builder in `make
  worktree NAME=<next> STACK_ON=<this PR's branch>`. That records the branch's
  **base mark**, the commit it was stacked on. Otherwise use a plain `make
  worktree NAME=<next>`.
- Run its steps 1 to 5 as usual, with four changes while it is stacked. Step
  1's rebase onto `origin/main` waits. Its builder brief says the branch is
  stacked, so the builder commits on it (unpushed) before handing back.
  `/simplify` and `/code-review` cover only its own work: pass them "since
  <mark>", from `sh scripts/restack.sh <next worktree> --mark`. Its one `make
  check` waits until it is restacked on `main`, so its step 5 is done without
  it.
- Every move of a stacked branch goes through `sh scripts/restack.sh <next
  worktree> <new base>`: `<this PR's branch>` after a push here, `main` once
  this PR merges. It rebases only the branch's own commits and moves the mark.
  A plain `git rebase` replays this PR's old commits.
- Hold its step 6 until this PR merges and it is restacked on `main`; then
  run its `make check` and go on.
- This PR's watcher keeps running, and its notices come first.

Stay one PR ahead: one PR open and one being built. Checks from both queue
behind the machine-wide check lock; that wait is expected.

**Done:** the PR state is `MERGED` or `CLOSED`, and the next PR, when the plan
names one, has its design questions put to the operator or its builder
running.
