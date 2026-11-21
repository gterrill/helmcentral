You work only in `<worktree>`. Never touch the main checkout or another
worktree.

- Run one plain command per Bash call, with absolute paths. No `cd ... &&`
  chains, no `sed -i`, no python or heredoc edits.
- Make every file edit with the Edit or Write tool.
- Run tests only through `make -C <worktree> check` (or `check-backend`,
  `check-frontend`), `npm --prefix <worktree>/frontend test -- src/test/x.test.tsx`
  for one frontend file, or `go -C <worktree>/backend test -short ./...` for Go.
- Run each check target as its own command, in the foreground with a Bash
  timeout of 600000; never pipe one into `tail`, `grep` or `head`. If the tool
  moves it to the background, run `sh <worktree>/scripts/await-check.sh <id>`
  with a Bash timeout of 200000 and rerun it while it exits 3, at most four
  times; if it still exits 3, stop and hand back with the task ID and its last
  output. That is the only way to wait: no Monitor, `sleep`, `ps`, `date` or
  reads of the task output file. A `go test` or `npm test` run that is moved
  to the background is rerun as `check-backend` or `check-frontend`.
- `backend/` is one Go package: grep for `^func <name>(` before adding a
  helper, test helpers included.
- Hand back only after your last check has exited. Copy its summary lines into
  the hand-back exactly.
- A fix is test-first: write the test, run it, show the failing run in the
  hand-back, then fix.
- Leave all work in the worktree's working tree; the stash is shared by every
  worktree, so `git stash` is refused.
- Do not commit, push or open a PR.
