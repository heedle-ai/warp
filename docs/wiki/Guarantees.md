The complete list of guarantees, each with the test that guards it and the mistake it came from, is on [Invariants](Invariants). This page explains the ones that need more than a sentence.

[[/images/cas.svg|alt=Refs move forward by compare-and-swap]]

## Concurrency

Every `Advance` is a separate `git update-ref` process, so the concurrency test covers separate processes, not just goroutines.

## Single protocol for writers

warp writes only through Git's own commands, so it uses Git's own lock files. A person running `git` against the store at the same time as warp is coordinated with it, never silently overwritten. To keep history meaningful, still move warp's refs only through warp.

## Merging

`MergeTrees` is `git merge-tree`, so it behaves like Git's merge in every way:

- **Files merge line by line.** If ours changes the first line of a file and theirs the last, the result has both and no conflict: a file neither side wrote (`TestMergeTrees_MultiLineFilesMergeByLine`).
- **Subtrees merge path by path** inside them.
- **Renames are always followed.** If one side deletes a path and adds similar content under another name, the other side's change follows it there. `merge.renames=false` and `-X no-renames` don't turn this off in `git merge-tree` (`TestMergeTrees_RenamesAreFollowed`).
- **The store's config and attributes apply,** including merge drivers. warp creates stores without any; don't add them.

It's a whole-entry, per-path merge only for a flat tree of one-line blobs where no side deletes a path. [loom](https://github.com/heedle-ai/loom/wiki/Branching-and-Merging)'s beliefs index is exactly that, and loom refuses any index that isn't.

## What warp doesn't do

- **Merge contents for you.** `MergeTrees` exposes Git's three-way merge of trees, nothing more: it reports conflicts and leaves them for the caller to resolve. [loom](https://github.com/heedle-ai/loom/wiki/Branching-and-Merging) uses it on its beliefs index, where every path is a whole run, so no file is ever merged line by line into something no run produced.
- **Undo real-world actions.** Moving a ref back doesn't unsend an email.
- **Privacy.** Anything you `git push` is readable by whoever can read the remote. Push agent knowledge only to private remotes.
- **Network filesystems.** Git's locking assumes a local disk.
- **Stale lock files.** If a process dies while holding a ref's lock, `<ref>.lock` stays behind and every `Advance` on that ref returns `ErrConflict`. Git doesn't remove it automatically, and neither does warp: check no git process is running, then delete the `.lock` file under the store.
