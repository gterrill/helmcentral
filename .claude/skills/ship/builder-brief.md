You work only in `<worktree>`. Never touch the main checkout or another
worktree.

- Run one plain command per Bash call, with absolute paths. No `cd ... &&`
  chains, no `sed -i`, no python or heredoc edits.
- Make every file edit with the Edit or Write tool.
- Run tests only through `make -C <worktree> check` (or `check-backend`,
  `check-frontend`), `npm --prefix <worktree>/frontend test -- src/test/x.test.tsx`
  for one frontend file, or `go -C <worktree>/backend test -short ./...` for Go.
- Run `make check` in the foreground with a Bash timeout of 600000. Never pipe
  a check target into `tail`, `grep` or `head`. No Monitor, sleep loops or
  background commands.
- Hand back only after your last check has exited. Copy its summary lines into
  the hand-back exactly.
- A fix is test-first: write the test, run it, show the failing run in the
  hand-back, then fix.
- Do not commit, push or open a PR.
