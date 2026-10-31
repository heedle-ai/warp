Each guarantee has a test in `warp_test.go`.

| Guarantee | Test |
|---|---|
| Equal content gives equal IDs, different content gives different IDs, the ID is Git's own (`ce013625…` for `hello\n`), and `HashBlob` agrees with `PutBlob` | `TestBlob_ContentAddressed` |
| Tree IDs don't depend on entry order; invalid or duplicate names are rejected | `TestTree_RoundTripAndValidation` |
| The same commit spec always gives the same commit ID; trailers read back the same through warp and through `git log` | `TestCommit_DeterministicWithTrailers` |
| Create-only refs, compare-and-swap, stale writes rejected with `ErrConflict`, invalid ref names rejected with `ErrInvalid` | `TestAdvance_CompareAndSwap` |
| Of 24 concurrent writers expecting the same head, exactly one wins and 23 get `ErrConflict`, over three trials. The ref only moves to a descendant of its old head, and `git fsck --strict` passes afterwards | `TestAdvance_ExactlyOneConcurrentWinner` |
| Inherited `GIT_DIR` and similar variables (as inside a git hook) are ignored | `TestIgnoresInheritedGitEnvironment` |
| Refs only move forward, and only to commits: an unrelated commit or a tree is rejected with `ErrInvalid` and the ref is unchanged | `TestAdvance_OnlyFastForwardToCommits` |
| A ref name that clashes with an existing ref is `ErrInvalid`, never a retryable `ErrConflict` | `TestAdvance_NameClashIsNotAConflict` |
| Ref names are exact: a tag that Git would guess from the name isn't used | `TestResolve_ExactNameNoGuessing` |
| IDs are full hex names; abbreviations and revision expressions are rejected | `TestIDs_RejectRevisionExpressions` |
| Trailers read back exactly, duplicates and order included; malformed keys, values and trailer-like messages are rejected | `TestTrailers_ReadBackExactly` |
| Trailer lookup matches key and value exactly, rejects injected keys, and ignores the same text in the message body | `TestFindByTrailer_ExactMatchOnly`, `TestFindByTrailer_ValueInBodyIsNotATrailer` |
| `Log` lists history newest first with trailers and times; `Diff` shows exactly the changed lines and rejects revision expressions | `TestLogAndDiff` |
| Messages with a `---` line (which hides trailers from `git log`) are rejected | `TestCommit_RejectsDashLinesThatHideTrailers` |
| A ref pointing at a non-commit is `ErrInvalid`, not retried | `TestResolve_RefToNonCommitIsInvalid` |

Every `Advance` is a separate `git update-ref` process, so the concurrency test covers separate processes, not just goroutines.

## Single protocol for writers

warp writes only through Git's own commands, so it uses Git's own lock files. A person running `git` against the store at the same time as warp is coordinated with it, never silently overwritten. To keep history meaningful, still move warp's refs only through warp.

## What warp doesn't do

- **Merge.** Git's textual three-way merge isn't the merge heedle-ai wants for knowledge, so warp doesn't expose it. Merging will be defined on top of warp: union where data only accumulates, and an explicit conflict otherwise.
- **Undo real-world actions.** Moving a ref back doesn't unsend an email.
- **Privacy.** Anything you `git push` is readable by whoever can read the remote. Push agent knowledge only to private remotes.
- **Network filesystems.** Git's locking assumes a local disk.
- **Stale lock files.** If a process dies while holding a ref's lock, `<ref>.lock` stays behind and every `Advance` on that ref returns `ErrConflict`. Git doesn't remove it automatically, and neither does warp: check no git process is running, then delete the `.lock` file under the store.
