You work only in `<worktree>`. Never touch the main checkout or another
worktree.

- Run one plain command per Bash call, with absolute paths. No `cd ... &&`
  chains, no `sed -i`, no python or heredoc edits.
- Make every file edit with the Edit or Write tool.
- Run the **targeted tests**, the ones for the code you touched, and nothing
  wider. Go: `go -C <worktree>/backend vet ./...` and `go -C
  <worktree>/backend test -short -run '<TestA|TestB>' <package>`, where
  `<package>` is `.` or the subpackage you touched, such as `./cmd/inlandmask`. Frontend: `npm --prefix
  <worktree>/frontend test -- src/test/x.test.tsx`, `npm --prefix
  <worktree>/frontend exec -- tsc -p <worktree>/frontend` and `npm --prefix
  <worktree>/frontend run lint`. The lead runs `make check` once per PR, so
  never run it yourself.
- Run each test command in the foreground with a Bash timeout of 600000,
  never piped into `tail`, `grep` or `head`. If the tool moves one to the background, wait for its completion
  notice; no Monitor, `sleep`, `ps` or reads of the task output file. Next
  time, narrow its `-run` pattern or file list.
- `backend/` is one Go package: grep for `^func <name>(` before adding a
  helper, test helpers included.
- Hand back only after your last test run has exited. List the test commands
  you ran and their pass counts.
- A fix is test-first: write the test, run it, show the failing run in the
  hand-back, then fix.
- Leave all work in the worktree's working tree; the stash is shared by every
  worktree, so `git stash` is refused.
- Do not push or open a PR. Commit only when this brief says the branch is
  stacked: then stage by path and commit on the branch before handing back.
