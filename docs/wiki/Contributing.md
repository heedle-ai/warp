## Ground rules

- **Reuse Git, never reimplement it.** Every operation is a Git command. If something seems to need hashing or object files written by hand, stop and ask first.
- **Deterministic commits.** Never let Git default a date; callers pass one.
- **Every guarantee has a test,** listed on [Guarantees](Guarantees). Tests use the real `git` binary and a temporary store.
- **Update this wiki in the same change.** The pages live in `docs/wiki/` in this repo, and every push to `main` publishes them here.
- Supported platforms are Linux (amd64, arm64) and macOS on Apple Silicon.

## Before opening a pull request

```bash
gofmt -l .        # prints nothing
go vet ./...
go test -race ./...
```
