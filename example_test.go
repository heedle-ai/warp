package warp_test

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/heedle-ai/warp"
)

func Example() {
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "warp-example")
	defer os.RemoveAll(dir)

	s, err := warp.Init(ctx, dir+"/store.git")
	if err != nil {
		fmt.Println(err)
		return
	}

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
	head, _, _ := s.Resolve(ctx, "refs/agents/demo")
	fmt.Println("blob:", blob)
	fmt.Println("head is commit:", head == commit)
	// Output:
	// create: <nil>
	// create again: true
	// blob: ce013625030ba8dba906f756967f9e9ca394464a
	// head is commit: true
}
