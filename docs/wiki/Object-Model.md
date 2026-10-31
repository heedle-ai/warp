warp's objects are Git's objects. Each warp call is one Git plumbing command, run with a clean environment: inherited `GIT_*` variables, system and global config, prompts and locale are all ignored, so results don't depend on the machine.

| warp call | Git command | Notes |
|---|---|---|
| `PutBlob(data)` | `git hash-object -w --stdin` | Returns Git's ID for the content |
| `Blob(id)` | `git cat-file blob` | `ErrNotFound` if there's no such blob |
| `PutTree(entries)` | `git mktree -z` | Any input order; Git sorts. warp rejects empty, `.`, `..`, `/`-containing and duplicate names first, because `mktree` would accept duplicates and produce a tree `git fsck` flags |
| `Tree(id)` | `git ls-tree -z` | Entries in Git's order |
| `Commit(spec)` | `git interpret-trailers`, then `git commit-tree` | Author and committer are both `spec.Author`; GPG signing is off |
| `Trailers(id)` | `git interpret-trailers --parse` | |
| `Parents(id)` | `git log -1 --format='%T %P'` | Tree and parents |
| `Resolve(ref)` | `git rev-parse --verify` | `ok == false` if the ref doesn't exist |
| `Advance(ref, old, new)` | `git update-ref ref new old` | Compare-and-swap. `old == ""` means create-only |
| `IsAncestor(a, b)` | `git merge-base --is-ancestor` | |
| `FindByTrailer(ref, key, value)` | `git log --format=…%(trailers:…)` | Most recent match reachable from `ref` |

## Identity

An ID is Git's object name: a hash of the object's type, length and content. Equal content always gets an equal ID, and the same `CommitSpec` always gives the same commit, because warp never lets Git fill in "now": the author time comes from the caller.

warp uses Git's default object format, SHA-1. That's sound for identifying content heedle-ai writes itself, but it isn't a defence against someone deliberately crafting colliding content.

## Refs

Refs are the only mutable state. Names must start with `refs/` and pass `git check-ref-format`. They move only through `Advance`:

```text
Advance(ref, old, new): set ref := new if ref == old (old == "" means "ref must not exist")
otherwise ErrConflict, and nothing changes
```

Re-read the ref with `Resolve`, rebuild your commit on the new head, and try again.

## Trailers

Commit metadata, such as the Temporal workflow and run IDs, goes in Git trailers (`Key: Value` lines at the end of the message), the same format as `Signed-off-by:`. Keys can't contain `:`, spaces or newlines; values can't contain newlines.
