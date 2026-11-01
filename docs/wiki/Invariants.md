Every invariant below is guarded by a test or a CI step, and CI checks this page against the code on every push: each test named here must exist, and every `TestLaw_*`, `TestInv_*` and `Fuzz*` test must be listed here. A mistake isn't fixed until it has a row on this page.

[[/images/invariant-loop.svg|alt=From a mistake to a codified invariant: mistake, named invariant, guard, CI gate]]

[[/images/cas.svg|alt=Two writers race from the same head; one wins, the other gets ErrConflict, re-reads and rebuilds; history stays linear]]

| ID | Invariant | Guarded by | Came from |
|---|---|---|---|
| WARP-1 | Identity is content: equal content has equal IDs, and they're Git's own | `TestBlob_ContentAddressed` | Design: reuse Git's object model |
| WARP-2 | warp reimplements nothing: it depends only on Go's standard library and runs the `git` binary | `TestInv_StandardLibraryOnly` | The go-git detour (a reimplementation of Git) |
| WARP-3 | The same commit spec always gives the same commit ID | `TestCommit_DeterministicWithTrailers` | Records must be reproducible |
| WARP-4 | Refs move by compare-and-swap; create-only when there's no old value | `TestAdvance_CompareAndSwap` | Design |
| WARP-5 | Of writers racing from one head, exactly one wins; the rest get `ErrConflict` | `TestAdvance_ExactlyOneConcurrentWinner` | Design; checked across separate `git` processes |
| WARP-6 | Refs only move forward, and only to commits | `TestAdvance_OnlyFastForwardToCommits` | Review 1: a ref could move to an unrelated commit or a tree |
| WARP-7 | Conflicts are decided by re-reading the ref, never by parsing Git's messages; a name clash is `ErrInvalid` | `TestAdvance_NameClashIsNotAConflict` | Review 1: a permanent error was reported as a retryable conflict |
| WARP-8 | Ref names are exact: Git never guesses (`refs/tags/…`) | `TestResolve_ExactNameNoGuessing` | Review 1: guessing made a tag shadow a missing ref |
| WARP-9 | IDs are full hex names; revision expressions are refused | `TestIDs_RejectRevisionExpressions` | Review 1: `refs/r:file` was accepted as an ID |
| WARP-10 | Commit text reads back exactly, or is refused: trailers keep order and duplicates; messages need a subject and can't contain `---` lines, invalid UTF-8 or control characters | `FuzzTrailers`, `TestTrailers_ReadBackExactly`, `TestCommit_RejectsDashLinesThatHideTrailers`, `ci:fuzz` | Reviews 1 and 2, then four inputs found by `FuzzTrailers` (invalid UTF-8, NUL, a control character splitting `git log` output, a blank subject) |
| WARP-11 | Trailer lookup matches key and value exactly, and ignores the same text in a message body | `TestFindByTrailer_ExactMatchOnly`, `TestFindByTrailer_ValueInBodyIsNotATrailer`, `TestFindByTrailer` | Review 1: lookups matched case-insensitively and could be injected |
| WARP-12 | A ref pointing at a non-commit is `ErrInvalid`, never retried | `TestResolve_RefToNonCommitIsInvalid` | Review 2 |
| WARP-13 | Tree names are valid and unique; `.git` is refused | `TestTree_RoundTripAndValidation` | `git mktree` accepts duplicates; review 1 found `.git` |
| WARP-14 | Inherited `GIT_*` variables and user config never redirect warp | `TestIgnoresInheritedGitEnvironment` | Design: warp may run inside a git hook |
| WARP-15 | Only bare repositories open as stores | `TestOpen_RejectsNonBare` | Design |
| WARP-16 | `Log` and `Diff` read history exactly and refuse revision expressions | `TestLogAndDiff` | Needed by the UI |
| WARP-17 | The documented example's output is exact | `Example` | Docs must never claim untested behaviour |
