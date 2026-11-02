---
name: release
description: Cut a Helmcentral release by choosing the next SemVer, reviewing the commits since the last tag, finalising CHANGELOG.md and tagging it. Use when the operator asks to release, tag or ship a new version.
---

Run every step in order. Each one ends on its **done** line. The operator must
have asked for this release.

## 1. Choose the version

Run `git fetch origin --tags`, list the tags with `git tag --sort=-v:refname`
and take the next SemVer.

**Done:** you have the next version, and the `CHANGELOG.md` entries under
`## [Unreleased]` justify its size.

## 2. Review the release

Run `/code-review high` on the commits since the last tag. For a large
migration (a framework or major dependency upgrade), the operator runs
`/code-review ultra` instead. These levels report uncertain findings, so check
each one against the code. Carry each fix to its own PR with the `ship` skill.

**Done:** every finding is fixed and merged, shown wrong against the code, or
left open by the operator's decision.

## 3. Open the release PR

In your own worktree, rename `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`, add
a fresh empty `## [Unreleased]` above it, and update the compare links at the
foot of the file. Run `sh packaging/release-notes.sh vX.Y.Z`: the release
workflow publishes this output and fails when the section is missing. Then take
it through the `ship` skill, whose steps include `/security-review`.

**Done:** the PR state is `MERGED`.

## 4. Tag

Confirm CI passed on `main` at the release PR's merge commit, including the
full-target release builds, which run only on `main`. Ask the operator before you
tag. Fetch, then tag that merge commit and push only that tag. Watch the release
workflow to the end.

**Done:** the workflow passed and the GitHub release body shows the notes. The
operator decides when to deploy.
