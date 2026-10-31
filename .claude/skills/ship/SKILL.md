---
name: ship
description: Carry finished work from your own worktree to an open PR, then watch it until it merges. Use when a change is built and its tests pass, or the operator says ship it, commit it, or open the PR.
---

Run every step in order. Each one ends on its **done** line; a step is not
finished until that line is true. The rules behind each check (what it
diffs, how to treat its findings) are in `AGENTS.md` under Checks Before
Commit And Release, Fallback Policy and Test-First Policy.

## 1. Check where you are

`git rev-parse --show-toplevel` must be your own worktree, not the main
checkout, and `git status` must hold only your changes. Run `git fetch
origin` and rebase onto `origin/main`; local `main` often lags.

**Done:** all three true. If the first two are not, stop and ask.

## 2. Release notes and docs

An operator-visible change gets an entry under `## [Unreleased]` in
`CHANGELOG.md`, written with the `help-pages` skill. Refactors, tests and
tooling get none. A change to architecture or a feature contract also
updates its ADR and the affected page under `docs/`.

**Done:** the entry is in, or the change is a refactor, test or tooling
change; and every ADR and `docs/` page the change contradicts is updated.

## 3. Simplify

For anything larger than a small edit, run `/simplify`.

**Done:** its fixes are applied, or the change is a small edit.

## 4. Test

Run every suite the change touches:

- Backend: `cd backend && go vet ./... && go test -short ./...`
- Frontend: `cd frontend && npm test && npm run lint && npm run knip:deps && npm run build`
- Help pages under `docs/`: `make docs-lint`

**Done:** every suite you ran is green. Keep the pass counts for the PR body.

## 5. Review

Run `/code-review medium` on the working tree. Then read the
`CODING_STANDARDS.md` sections the diff touches and check every changed line
against them. Check each finding against the code. Fix the real ones
test-first, then go back to step 4.

**Done:** every finding is fixed or dismissed with a reason you can state,
every touched standards section has been applied to the diff, and step 4 is
green on the fixed tree.

## 6. Commit and open the PR

Stage by path and commit. Push the branch. Open the PR with `gh pr
create`, or, when the branch already has one, the push updates it. The
body says what the operator will notice, then gives each design point agreed in this conversation and where it landed (file, screen,
or "dropped, because…"). End with the test pass counts. No "Generated with"
footer.

**Done:** the PR URL is in hand.

## 7. Security review

Run `/security-review`. It sees committed work only, so it runs after step
6. Fix each confirmed finding test-first, then go back to step 4.

**Done:** the report is clean, or every finding is fixed and pushed.

## 8. Watch until merged

Report the PR URL and a one-paragraph summary to the operator. Then start a
Monitor on this loop, which prints `state | mergeable | failed checks` only
when that line changes:

```bash
prev=; while :; do
  cur=$(gh pr view <N> --json state,mergeable,statusCheckRollup --jq '[.state, .mergeable,
    ([.statusCheckRollup[] | select((.conclusion // .state) | IN("FAILURE","ERROR","CANCELLED","TIMED_OUT","STARTUP_FAILURE","ACTION_REQUIRED"))
      | (.name // .context)] | join(", "))] | join(" | ")')
  [ "$cur" != "$prev" ] && echo "$cur" && prev=$cur
  case $cur in MERGED*|CLOSED*) exit 0;; esac
  sleep 60
done
```

- A failed check: read its log with `gh run view --log-failed`, fix it,
  go back to step 4.
- `CONFLICTING`: rebase onto `origin/main`, go back to step 4.
- `CLOSED`: stop and tell the operator.
- `MERGED`: tell the operator it merged and pick up whatever comes next in
  the plan you are working from.

The operator merges. The PR is theirs to merge, tag and deploy.

**Done:** the PR state is `MERGED` or `CLOSED`.
