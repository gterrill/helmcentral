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
checkout, and `git status` must hold only your changes.

**Done:** both true. If either is false, stop and ask.

## 2. Release notes

An operator-visible change gets an entry under `## [Unreleased]` in
`CHANGELOG.md`, written with the `help-pages` skill. Refactors, tests and
tooling get none.

**Done:** the entry is in, or the change is one of those three.

## 3. Simplify

For anything larger than a small edit, run `/simplify`.

**Done:** its fixes are applied, or the change is a small edit.

## 4. Test

Run every suite the change touches:

- Backend: `cd backend && go vet ./... && go test -short ./...`
- Frontend: `cd frontend && npm test && npm run lint && npm run build`
- Help pages or `CHANGELOG.md`: `make docs-lint`

**Done:** every suite you ran is green. Keep the pass counts for the PR body.

## 5. Review

Run `/code-review medium` on the working tree. Check each finding against
the code. Fix the real ones test-first, then go back to step 4.

**Done:** every finding is fixed or dismissed with a reason you can state,
and step 4 is green on the fixed tree.

## 6. Commit and open the PR

Stage by path and commit. Push the branch and open the PR with `gh pr
create`. The body says what the operator will notice, then gives each
design point agreed in this conversation and where it landed (file, screen,
or "dropped, because…"). End with the test pass counts. No "Generated with"
footer.

**Done:** the PR URL is in hand.

## 7. Security review

Run `/security-review`. It sees committed work only, so it runs after step
6. Fix each confirmed finding test-first, commit, push.

**Done:** the report is clean, or every finding is fixed and pushed.

## 8. Watch until merged

Report the PR URL and a one-paragraph summary to the operator. Then start a
Monitor that polls every 60 s:

```bash
gh pr view <N> --json state,statusCheckRollup \
  --jq '.state, ([.statusCheckRollup[] | select(.conclusion=="FAILURE") | .name] | join(", "))'
```

- A failed check: read its log with `gh run view --log-failed`, fix it,
  go back to step 4.
- `CLOSED`: stop and tell the operator.
- `MERGED`: tell the operator it merged and pick up whatever comes next in
  the plan you are working from.

The operator merges. The PR is theirs to merge, tag and deploy.

**Done:** the PR state is `MERGED` or `CLOSED`.
