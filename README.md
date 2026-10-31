# warp

heedle-ai's content-addressed object store: a bare Git repository, driven through the `git` command line.

```go
s, _ := warp.Init(ctx, "store.git")
blob, _ := s.PutBlob(ctx, []byte("hello\n"))        // git hash-object -w
tree, _ := s.PutTree(ctx, []warp.Entry{...})          // git mktree
commit, _ := s.Commit(ctx, warp.CommitSpec{...})      // git commit-tree
err := s.Advance(ctx, "refs/agents/demo", old, commit) // git update-ref (compare-and-swap)
```

warp doesn't reimplement Git. Every operation is a Git plumbing command, so `git log`, `git show`, `git fsck` and `git push` all work on the store.

Documentation: **[wiki](https://github.com/heedle-ai/warp/wiki)**

Requires Go 1.24+ and `git` on `PATH`.

```bash
go test -race ./...
```
