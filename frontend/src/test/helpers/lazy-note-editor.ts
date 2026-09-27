/**
 * The lazy note-editor chunk (note-editor.tsx -> note-editor-impl.tsx, ADR
 * 0117) pulls in Slate/Plate and friends - by far the largest dependency
 * graph behind a React.lazy() boundary in this project. Measured in this
 * vitest environment, the FIRST dynamic import() of
 * `@/components/note-editor-impl` in a given test file takes roughly 2
 * seconds to resolve; Testing Library's `findBy*` default timeout
 * (asyncUtilTimeout, src/test/setup.ts) is 1000ms, so whichever test in a
 * file mounts the editor first will lose that race under the default.
 *
 * React.lazy() memoises the resolved promise on the module-level lazy()
 * object itself (note-editor.tsx's `NoteEditorImpl` / `LazyNoteEditorBodyImpl`),
 * so only the first mount in a given test file actually pays the import
 * cost - every later mount of the SAME lazy object in that file renders
 * synchronously. That's why only the await immediately following the first
 * render of a lazy editor body/component in each test needs this: later
 * awaits in the same test, and awaits in later tests, resolve fast against
 * the already-settled promise.
 *
 * Do not raise the global `asyncUtilTimeout` in src/test/setup.ts to cover
 * this - that would hide slow awaits everywhere else in the suite. This
 * constant is deliberately scoped to the handful of call sites that
 * actually race the lazy editor chunk (note-capture-sheet.test.tsx,
 * note-editor.test.tsx).
 *
 * 4000ms, deliberately under vitest's own 5000ms default testTimeout (this
 * project sets neither testTimeout nor asyncUtilTimeout, so both defaults
 * apply). At 5000 the two would expire together and vitest would kill the
 * test first, reporting a bare "test timed out" instead of Testing
 * Library's error naming the element that never appeared. Twice the
 * measured import cost, and it still loses to nothing.
 */
export const LAZY_NOTE_EDITOR_TIMEOUT_MS = 4000
