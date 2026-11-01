**warp** is heedle-ai's content-addressed object store. It's a bare Git repository driven through the `git` command line: warp adds a small typed Go API on top and reimplements nothing.

```go
blob, _ := s.PutBlob(ctx, []byte("hello\n"))   // "ce013625…", Git's own ID for this content
err := s.Advance(ctx, "refs/agents/demo", "", commit) // create the ref; fails if it exists
```

Because the store is a real Git repository, the tools you already know work on it: `git log` to read history, `git show` to inspect an object, `git fsck` to verify integrity, and `git push` to back it up or replicate it.

[[/images/cas.svg|alt=Refs move forward by compare-and-swap: one writer wins, the other rebuilds on the new head]]

## Why

[shuttle](https://github.com/heedle-ai/shuttle/wiki) records *how* work ran, in Temporal's event history, which has retention limits and size caps. warp records *what the system knows*: permanently, checkable by hash, and portable. [loom](https://github.com/heedle-ai/loom/wiki) ends every run with a warp commit that links back to its Temporal workflow.

## Where warp fits

| Repo | Role |
|---|---|
| [weft](https://github.com/heedle-ai/weft/wiki) | Composition algebra |
| [draft](https://github.com/heedle-ai/draft/wiki) | Language: s-expressions that compile to shuttle plans |
| [shuttle](https://github.com/heedle-ai/shuttle/wiki) | Durable execution on Temporal |
| [bobbin](https://github.com/heedle-ai/bobbin/wiki) | A fresh Podman container per agent step |
| [reed](https://github.com/heedle-ai/reed/wiki) | A fresh browser per browsing step, reaching only the hosts its program names |
| **warp** | Content-addressed store (this repo) |
| [drawdown](https://github.com/heedle-ai/drawdown/wiki) | Web UI: write, run and browse programs and records |
| [loom](https://github.com/heedle-ai/loom/wiki) | Runs everything together |

## Pages

- [Getting Started](Getting-Started): install, write objects, move a ref
- [Object Model](Object-Model): objects, IDs, refs, and the Git command behind each call
- [Guarantees](Guarantees): concurrency, writers, and what warp doesn't do
- [Invariants](Invariants): every guarantee, the test that guards it, and the mistake it came from
- [Contributing](Contributing)

## Requirements

- Go **1.24** or later
- `git` on `PATH`, **2.38 or newer** for `MergeTrees` (`git merge-tree --write-tree`); everything else uses long-standing plumbing commands. Tested with Git 2.43. The Git from Xcode's Command Line Tools on current macOS is new enough; `brew install git` if `git --version` is older
- Linux (amd64 or arm64), or macOS on Apple Silicon
