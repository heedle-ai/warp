Each guarantee has a test in `warp_test.go`.

| Guarantee | Test |
|---|---|
| Equal content gives equal IDs, different content gives different IDs, the ID is Git's own (`ce013625…` for `hello\n`), and `HashBlob` agrees with `PutBlob` | `TestBlob_ContentAddressed` |
| Tree IDs don't depend on entry order; invalid or duplicate names are rejected | `TestTree_RoundTripAndValidation` |
| The same commit spec always gives the same commit ID; trailers read back the same through warp and through `git log` | `TestCommit_DeterministicWithTrailers` |
| Create-only refs, compare-and-swap, stale writes rejected with `ErrConflict`, invalid ref names rejected | `TestAdvance_CompareAndSwap` |
| Of 24 concurrent writers expecting the same head, exactly one wins and 23 get `ErrConflict`, over three trials. The ref only moves to a descendant of its old head, and `git fsck --strict` passes afterwards | `TestAdvance_ExactlyOneConcurrentWinner` |
| Inherited `GIT_DIR` and similar variables (as inside a git hook) are ignored | `TestIgnoresInheritedGitEnvironment` |

Every `Advance` is a separate `git update-ref` process, so the concurrency test covers separate processes, not just goroutines.

## Single protocol for writers

warp writes only through Git's own commands, so it uses Git's own lock files. A person running `git` against the store at the same time as warp is coordinated with it, never silently overwritten. To keep history meaningful, still move warp's refs only through warp.

## What warp doesn't do

- **Merge.** Git's textual three-way merge isn't the merge heedle-ai wants for knowledge, so warp doesn't expose it. Merging will be defined on top of warp: union where data only accumulates, and an explicit conflict otherwise.
- **Undo real-world actions.** Moving a ref back doesn't unsend an email.
- **Privacy.** Anything you `git push` is readable by whoever can read the remote. Push agent knowledge only to private remotes.
- **Network filesystems.** Git's locking assumes a local disk.
