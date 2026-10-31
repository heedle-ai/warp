## 1. Install Go 1.24+ and Git

**macOS (Apple Silicon):** Git comes with the Xcode Command Line Tools (`xcode-select --install`), or use `brew install git`.

```bash
brew install go
git --version && go version
```

**Linux (amd64 or arm64)**

```bash
sudo apt-get install -y git        # or: sudo dnf install -y git
GO_VERSION=1.24.7
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
curl -fsSLO "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "go${GO_VERSION}.linux-${ARCH}.tar.gz"
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile && source ~/.profile
```

## 2. Run the tests

```bash
mkdir -p ~/src/heedle-ai && cd ~/src/heedle-ai
git clone https://github.com/heedle-ai/warp.git
cd warp
go test -race ./...
```

## 3. Use it

```go
s, err := warp.Init(ctx, dir+"/store.git")

blob, _ := s.PutBlob(ctx, []byte("hello\n"))
tree, _ := s.PutTree(ctx, []warp.Entry{{Name: "greeting", Kind: warp.KindBlob, ID: blob}})
commit, _ := s.Commit(ctx, warp.CommitSpec{
	Tree:     tree,
	Message:  "first observation",
	Trailers: []warp.Trailer{{Key: "Shuttle-Run", Value: "run-1"}},
	Author:   warp.Signature{Name: "warp", Email: "warp@heedle.ai", When: time.Unix(1700000000, 0)},
})

fmt.Println("create:", s.Advance(ctx, "refs/agents/demo", "", commit))
fmt.Println("create again:", s.Advance(ctx, "refs/agents/demo", "", commit) != nil)
```

```text
create: <nil>
create again: true
```

This is `Example` in `example_test.go`, so `go test` checks it.

## 4. Look at it with Git

```bash
git --git-dir store.git log --format='%h %s%n  %(trailers:only,unfold)' refs/agents/demo
git --git-dir store.git ls-tree refs/agents/demo
git --git-dir store.git cat-file -p ce013625030ba8dba906f756967f9e9ca394464a
git --git-dir store.git fsck --strict
```

Reading with `git` is always safe. For writes, see [Guarantees](Guarantees#single-protocol-for-writers).

Next: [Object Model](Object-Model).
