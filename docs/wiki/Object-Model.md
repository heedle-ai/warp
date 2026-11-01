warp's objects are Git's objects. Each warp call runs the Git plumbing commands below (some first check the object's type with `git cat-file -t`), with a clean environment: inherited `GIT_*` variables, system and global config, prompts and locale are all ignored, so results don't depend on the machine.

| warp call | Git command | Notes |
|---|---|---|
| `PutBlob(data)` | `git hash-object -w --stdin` | Returns Git's ID for the content |
| `HashBlob(data)` | `git hash-object --stdin` | The ID `data` would have, without writing it or needing a store |
| `Blob(id)` | `git cat-file blob` | `ErrNotFound` if there's no such blob |
| `Blobs(ids)` | `git cat-file --batch` | Many blobs from one process, in order; `ErrNotFound` if any isn't a blob |
| `PutTree(entries)` | `git mktree -z` | Any input order; Git sorts. warp first rejects empty, `.`, `..`, `.git`, `/`-containing and duplicate names, because `mktree` would accept some of them and produce a tree `git fsck` flags |
| `Tree(id)` | `git ls-tree -z` | Entries in Git's order |
| `Commit(spec)` | `git interpret-trailers`, then `git commit-tree` | Author and committer are both `spec.Author`; GPG signing is off |
| `Trailers(id)` | `git log -1 --format=%(trailers:only,unfold)` | The same reader as `FindByTrailer` and `Log`, so they never disagree |
| `Parents(id)` | `git log -1 --format='%T %P'` | Tree and parents |
| `Resolve(ref)` | `git show-ref --verify` | Exact name only: Git's usual guessing (such as trying `refs/tags/` in front) isn't applied. `ok == false` if the ref doesn't exist |
| `Advance(ref, old, new)` | `git merge-base --is-ancestor`, then `git update-ref ref new old` | Compare-and-swap. `old == ""` means create-only. `new` must be a commit descending from `old` |
| `Log(from, limit)` | `git log --first-parent` | Commits newest first, with subject, time, parents and trailers |
| `Diff(a, b)` | `git diff --no-color --no-ext-diff` | Unified diff between two commits, for display |
| `IsAncestor(a, b)` | `git merge-base --is-ancestor` | |
| `FindByTrailer(from, key, value)` | `git log -F --grep`, then `%(trailers:only,unfold)` | Most recent commit reachable from commit `from` with exactly this trailer: key case-sensitive, value byte for byte |
| `MergeBases(a, b)` | `git merge-base --all` | Best common ancestors, sorted: none for unrelated histories, more than one after a criss-cross merge |
| `MergeTrees(base, ours, theirs)` | `git commit-tree` (three throwaway commits), then `git merge-tree --write-tree` | Git's own three-way merge of two trees. Returns the merged tree and the conflicting paths; each conflicting entry holds Git's conflict markers, so callers must replace it. Needs Git 2.38 or newer. It's Git's merge: files merge line by line, subtrees path by path, renames are followed, and the store's config applies; it's a whole-entry merge only for flat trees of one-line blobs with no deletions (see [Guarantees](Guarantees)) |
| `Refs(prefix)` | `git for-each-ref` | Refs under `prefix` (such as `refs/loom/`) and their commits, sorted by name |
| `ValidName(name)`, `CheckRef(ref)` | none, `git check-ref-format` | Validation, so callers can reject bad input early. `ValidName` does no I/O |

## IDs and errors

An `ID` is a full, lowercase hexadecimal object name: 40 digits (SHA-1) or 64 (SHA-256). Abbreviations and revision expressions such as `HEAD` or `refs/x:file` are rejected, so an ID always means exactly one object.

| Error | Meaning | Retry? |
|---|---|---|
| `ErrConflict` | The ref wasn't at `old`, or another writer held its lock. Nothing changed | Yes: re-read the ref and rebuild |
| `ErrInvalid` | Can never succeed: a malformed ID, ref, name or trailer, a move that isn't a fast-forward, or a ref name that clashes with an existing ref | No |
| `ErrNotFound` | The object doesn't exist, or isn't of the expected type | No |

`Advance` decides between conflict and invalid by re-reading the ref after a failed update, not by parsing Git's messages.

## Identity

An ID is Git's object name: a hash of the object's type, length and content. Equal content always gets an equal ID, and the same `CommitSpec` always gives the same commit, because warp never lets Git fill in "now": the author time comes from the caller.

warp uses Git's default object format, SHA-1. That's sound for identifying content heedle-ai writes itself, but it isn't a defence against someone deliberately crafting colliding content.

## Refs

Refs are the only mutable state. Names must start with `refs/` and pass `git check-ref-format`. They move only forward, and only through `Advance`:

```text
Advance(ref, old, new): set ref := new if ref == old and new descends from old
                        (old == "" means "ref must not exist")
otherwise an error, and nothing changes
```

Re-read the ref with `Resolve`, rebuild your commit on the new head, and try again.

## Trailers

Commit metadata, such as the Temporal workflow and run IDs, goes in Git trailers (`Key: Value` lines at the end of the message), the same format as `Signed-off-by:`. Trailers read back exactly as written, in order, duplicates included. To make that possible:

- keys are letters, digits and hyphens, starting with a letter or digit
- values are non-empty, valid UTF-8, without control characters or leading or trailing whitespace
- messages are valid UTF-8 whose only control characters are newline and tab; author names and emails have none. Git re-encodes invalid UTF-8 when reading it back and can't store NUL, and warp separates fields of `git log` output with control characters
- a message can't itself end in lines Git would read as trailers; put those in `Trailers`
- a message starts with a non-blank subject line
- a message can't contain a line starting with `---`, which Git treats as the end of the message
