## Ground rules

- **Reuse Git, never reimplement it.** Every operation is a Git command. If something seems to need hashing or object files written by hand, stop and ask first.
- **Deterministic commits.** Never let Git default a date; callers pass one.
- **A mistake isn't fixed until it has a row on [Invariants](Invariants)** and a guard test that fails on the old code. CI checks the page against the tests in both directions. Tests use the real `git` binary and a temporary store.
- **Fuzz inputs that ever failed stay in `testdata/fuzz/`** as permanent regression seeds.
- **Update this wiki in the same change.** The pages live in `docs/wiki/` in this repo, and every push to `main` publishes them here.
- Supported platforms are Linux (amd64, arm64) and macOS on Apple Silicon.

## Before opening a pull request

```bash
gofmt -l .        # prints nothing
go vet ./...
go test -race ./...
bash .github/invariants.sh
go test -run '^$' -fuzz FuzzTrailers -fuzztime 30s .
```
