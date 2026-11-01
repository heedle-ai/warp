package warp_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/heedle-ai/warp"
)

// TestInv_StandardLibraryOnly: warp depends only on Go's standard library, tests included;
// everything else it does is the git binary.
func TestInv_StandardLibraryOnly(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-test", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./...").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range strings.Fields(string(out)) {
		// The package itself and its test variants ("…/warp […]", "…/warp_test", "…/warp.test").
		if p != "github.com/heedle-ai/warp" && !strings.HasPrefix(p, "github.com/heedle-ai/warp_test") && !strings.HasPrefix(p, "github.com/heedle-ai/warp.test") && !strings.HasPrefix(p, "[") {
			t.Errorf("non-standard dependency: %s", p)
		}
	}
}

// FuzzTrailers: any message and trailer either is rejected with ErrInvalid,
// or is committed and reads back exactly (both copies, in order) through
// Trailers and FindByTrailer. Seeded with every trailer bug found so far.
func FuzzTrailers(f *testing.F) {
	for _, seed := range []struct{ msg, key, value string }{
		{"run", "Shuttle-Run", "run-1"},
		{"run", "a=b", "c"},                         // review 1: key with '='
		{"run", "#K", "v"},                          // review 1: key dropped
		{"run", "K", ""},                            // review 1: empty value dropped
		{"run", "K", " padded"},                     // review 1: value trimmed
		{"subj\n\nbody\n---\ntail", "Run", "r1"},    // review 2: '---' hid trailers from git log
		{"subject\n\nSneaky: trailer", "Run", "r1"}, // message ending in trailer-like lines
		{"run", "Url", "https://x/y?a=b:c"},         // colons in values
		{"run", "shuttle-run", "lower"},             // case must be kept
		{"run\x1fx", "K", "v"},                      // review 3: Log's field separator in a message
		{"run\x01x", "K", "v"},                      // review 3: Log's record separator in a message
		{"two\nline subject\n\nbody", "K", "v"},     // review 3: Log's subject is git's first paragraph
		{"subject  \t\nmore", "K", "v"},             // review 3: trailing space in the subject
	} {
		f.Add(seed.msg, seed.key, seed.value)
	}
	ctx := context.Background()
	s, err := warp.Init(ctx, f.TempDir()+"/store.git")
	if err != nil {
		f.Fatal(err)
	}
	tree, err := s.PutTree(ctx, nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, msg, key, value string) {
		want := []warp.Trailer{{Key: key, Value: value}, {Key: key, Value: value}}
		c, err := s.Commit(ctx, warp.CommitSpec{Tree: tree, Message: msg, Trailers: want,
			Author: warp.Signature{Name: "f", Email: "f@x", When: time.Unix(1700000000, 0)}})
		if errors.Is(err, warp.ErrInvalid) {
			return
		}
		if err != nil {
			t.Fatalf("Commit(%q, %q, %q): unexpected error %v", msg, key, value, err)
		}
		got, err := s.Trailers(ctx, c)
		if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("trailers %q read back as %q (err %v); message %q", want, got, err, msg)
		}
		if found, ok, err := s.FindByTrailer(ctx, c, key, value); err != nil || !ok || found != c {
			t.Fatalf("FindByTrailer(%q, %q) = %v %v %v; message %q", key, value, found, ok, err, msg)
		}
		// Log is the UI's reader: it must see the same commit, subject and trailers.
		log, err := s.Log(ctx, c, 1)
		if err != nil || len(log) != 1 || log[0].ID != c || fmt.Sprint(log[0].Trailers) != fmt.Sprint(want) || log[0].Subject != subject(msg) {
			t.Fatalf("Log of message %q = %+v (err %v); want subject %q, trailers %q", msg, log, err, subject(msg), want)
		}
	})
}

// subject is git's %s: the first paragraph of the message, each line
// without trailing whitespace, joined by single spaces.
func subject(msg string) string {
	var lines []string
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}
