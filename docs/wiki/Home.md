**warp** will be heedle-ai's content-addressed object store: Git's object model (blobs, trees, commits, refs) applied to an agent's knowledge instead of to source files.

> **Status: Planned.** The repository contains only an empty package. Everything on these pages is design, not working code. Pages are updated as each part lands.

## Why

[shuttle](https://github.com/heedle-ai/shuttle/wiki) records *how* work ran, in Temporal's event history. That history has retention limits, per-workflow size caps, and belongs to one Temporal namespace. warp records *what the system knows*: permanently, checkable by hash, and portable between systems. Each plan run will end in a warp commit that points back to its Temporal workflow and run ID.

## Where warp fits in heedle-ai

| Repo | Role |
|---|---|
| [weft](https://github.com/heedle-ai/weft/wiki) | Composition algebra |
| [draft](https://github.com/heedle-ai/draft/wiki) | Language: s-expressions that compile to shuttle plans |
| **warp** | Content-addressed object store (this repo) |
| [shuttle](https://github.com/heedle-ai/shuttle/wiki) | Durable execution on Temporal |
| [drawdown](https://github.com/heedle-ai/drawdown/wiki) | UI for commits, diffs and provenance |

## Pages

- [Object Model](Object-Model): objects, identity, refs
- [Roadmap](Roadmap): what gets built, in order
