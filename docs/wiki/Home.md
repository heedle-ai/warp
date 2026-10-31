**warp** is heedle-ai's content-addressed object store. It's a bare Git repository driven through the `git` command line: warp adds a small typed Go API on top and reimplements nothing.

```go
blob, _ := s.PutBlob(ctx, []byte("hello\n"))   // "ce013625…", Git's own ID for this content
err := s.Advance(ctx, "refs/agents/demo", "", commit) // create the ref; fails if it exists
```

Because the store is a real Git repository, the tools you already know work on it: `git log` to read history, `git show` to inspect an object, `git fsck` to verify integrity, and `git push` to back it up or replicate it.

## Why

[shuttle](https://github.com/heedle-ai/shuttle/wiki) records *how* work ran, in Temporal's event history, which has retention limits and size caps. warp records *what the system knows*: permanently, checkable by hash, and portable. [loom](https://github.com/heedle-ai/loom/wiki) ends every run with a warp commit that links back to its Temporal workflow.

## Where warp fits

| Repo | Role |
|---|---|
| [weft](https://github.com/heedle-ai/weft/wiki) | Composition algebra |
| [draft](https://github.com/heedle-ai/draft/wiki) | Language: s-expressions that compile to shuttle plans |
| [shuttle](https://github.com/heedle-ai/shuttle/wiki) | Durable execution on Temporal |
| **warp** | Content-addressed store (this repo) |
| [drawdown](https://github.com/heedle-ai/drawdown/wiki) | Viewing commits, diffs and provenance |
| [loom](https://github.com/heedle-ai/loom/wiki) | Runs everything together |

## Pages

- [Getting Started](Getting-Started): install, write objects, move a ref
- [Object Model](Object-Model): objects, IDs, refs, and the Git command behind each call
- [Guarantees](Guarantees): what warp promises, and how each promise is tested
- [Contributing](Contributing)

## Requirements

- Go **1.24** or later
- `git` on `PATH`. Tested with Git 2.43; uses only long-standing plumbing commands
- Linux (amd64 or arm64), or macOS on Apple Silicon
