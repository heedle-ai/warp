package warp_test

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heedle-ai/warp"
)

// TestLaw_BlobsAgreesWithBlob: reading many blobs at once returns exactly
// what reading each one returns, in order, including empty, binary and
// repeated blobs and contents that look like cat-file's own headers.
func TestLaw_BlobsAgreesWithBlob(t *testing.T) {
	s := newStore(t)
	r := rand.New(rand.NewSource(1))
	contents := [][]byte{{}, []byte("\n"), []byte("x missing\n"), []byte("ce013625030ba8dba906f756967f9e9ca394464a blob 3\nabc\n")}
	for i := 0; i < 60; i++ {
		b := make([]byte, r.Intn(300))
		r.Read(b)
		contents = append(contents, b)
	}
	var ids []warp.ID
	for _, c := range contents {
		ids = append(ids, must(s.PutBlob(ctx, c)))
	}
	ids = append(ids, ids[3], ids[0]) // repeats
	got, err := s.Blobs(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(ids) {
		t.Fatalf("%d blobs, want %d", len(got), len(ids))
	}
	for i, id := range ids {
		if want := must(s.Blob(ctx, id)); !bytes.Equal(got[i], want) {
			t.Errorf("blob %d (%s): Blobs = %q, Blob = %q", i, id, got[i], want)
		}
	}
	if got, err := s.Blobs(ctx, nil); err != nil || len(got) != 0 {
		t.Errorf("no IDs: %v %v", got, err)
	}
}

// TestBlobs_MissingWrongKindAndInvalid: like Blob, a missing ID or a
// non-blob is ErrNotFound, and a malformed ID is refused before git runs.
func TestBlobs_MissingWrongKindAndInvalid(t *testing.T) {
	s := newStore(t)
	a := must(s.PutBlob(ctx, []byte("a\n")))
	tree := must(s.PutTree(ctx, []warp.Entry{{Name: "a", Kind: warp.KindBlob, ID: a}}))
	for _, ids := range [][]warp.ID{
		{a, "0123456789012345678901234567890123456789"},
		{tree},
		{a, tree, a},
	} {
		if _, err := s.Blobs(ctx, ids); !errors.Is(err, warp.ErrNotFound) {
			t.Errorf("Blobs(%v) err = %v, want ErrNotFound", ids, err)
		}
	}
	for _, bad := range []warp.ID{"HEAD", "ce01362", a + "\n" + a, ""} {
		if _, err := s.Blobs(ctx, []warp.ID{a, bad}); !errors.Is(err, warp.ErrInvalid) {
			t.Errorf("Blobs(%q) err = %v, want ErrInvalid", bad, err)
		}
	}
}

// TestInv_BlobsIsOneGitProcess: Blobs runs git once however many blobs it
// reads, counted by a git on PATH that logs each run before running the
// real one. (drawdown CI on macOS: one process pair per blob made every
// record slower as history grew.)
func TestInv_BlobsIsOneGitProcess(t *testing.T) {
	s := newStore(t)
	var ids []warp.ID
	for i := 0; i < 25; i++ {
		ids = append(ids, must(s.PutBlob(ctx, []byte(fmt.Sprintf("%d\n", i)))))
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "runs")
	script := fmt.Sprintf("#!/bin/sh\necho run >> %q\nexec %q \"$@\"\n", log, real)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := s.Blobs(ctx, ids); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if n := strings.Count(string(b), "run"); n != 1 {
		t.Errorf("Blobs of %d blobs ran git %d times, want 1", len(ids), n)
	}
}

// TestBlobs_OnlyFullIDsInSHA256Stores: in a sha256 store, Blobs reads full
// IDs and refuses a 40-character prefix of one, because git names the object
// it found and Blobs checks that name is the one asked for.
func TestBlobs_OnlyFullIDsInSHA256Stores(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "--object-format=sha256", dir).CombinedOutput(); err != nil {
		t.Skipf("git can't make sha256 stores: %v %s", err, out)
	}
	s := must(warp.Open(ctx, dir))
	a := must(s.PutBlob(ctx, []byte("a\n")))
	if len(a) != 64 {
		t.Fatalf("ID %s isn't sha256", a)
	}
	if got := must(s.Blobs(ctx, []warp.ID{a, a})); string(got[0]) != "a\n" || string(got[1]) != "a\n" {
		t.Errorf("Blobs = %q", got)
	}
	if _, err := s.Blobs(ctx, []warp.ID{a[:40]}); !errors.Is(err, warp.ErrNotFound) {
		t.Errorf("40-character prefix: err = %v, want ErrNotFound", err)
	}
}
