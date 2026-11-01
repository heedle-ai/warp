The complete list of guarantees, each with the test that guards it and the mistake it came from, is on [Invariants](Invariants). This page explains the ones that need more than a sentence.

[[/images/cas.svg|alt=Refs move forward by compare-and-swap]]

## Concurrency

Every `Advance` is a separate `git update-ref` process, so the concurrency test covers separate processes, not just goroutines.

## Single protocol for writers

warp writes only through Git's own commands, so it uses Git's own lock files. A person running `git` against the store at the same time as warp is coordinated with it, never silently overwritten. To keep history meaningful, still move warp's refs only through warp.

## What warp doesn't do

- **Merge.** Git's textual three-way merge isn't the merge heedle-ai wants for knowledge, so warp doesn't expose it. Merging will be defined on top of warp: union where data only accumulates, and an explicit conflict otherwise.
- **Undo real-world actions.** Moving a ref back doesn't unsend an email.
- **Privacy.** Anything you `git push` is readable by whoever can read the remote. Push agent knowledge only to private remotes.
- **Network filesystems.** Git's locking assumes a local disk.
- **Stale lock files.** If a process dies while holding a ref's lock, `<ref>.lock` stays behind and every `Advance` on that ref returns `ErrConflict`. Git doesn't remove it automatically, and neither does warp: check no git process is running, then delete the `.lock` file under the store.
