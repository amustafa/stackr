package git

import (
	"path/filepath"
	"testing"
)

func tempRunner(t *testing.T) *Runner {
	t.Helper()
	dir := t.TempDir()
	r := &Runner{Dir: dir}
	if _, err := r.RunGitCapture("init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	// Configure user for commits.
	r.RunGitCapture("config", "user.email", "test@test.com")
	r.RunGitCapture("config", "user.name", "Test")
	return r
}

func TestHashObjectCatBlobRoundTrip(t *testing.T) {
	r := tempRunner(t)
	content := []byte("hello world\n")

	sha, err := r.HashObject(content)
	if err != nil {
		t.Fatalf("HashObject: %v", err)
	}
	if sha == "" {
		t.Fatal("expected non-empty SHA")
	}

	got, err := r.CatBlob(sha)
	if err != nil {
		t.Fatalf("CatBlob: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, content)
	}
}

func TestMakeTreeLsTreeRoundTrip(t *testing.T) {
	r := tempRunner(t)

	sha1, _ := r.HashObject([]byte("file1 content"))
	sha2, _ := r.HashObject([]byte("file2 content"))

	entries := []TreeEntry{
		{Mode: "100644", Type: "blob", SHA: sha1, Name: "a.txt"},
		{Mode: "100644", Type: "blob", SHA: sha2, Name: "b.txt"},
	}
	treeSHA, err := r.MakeTree(entries)
	if err != nil {
		t.Fatalf("MakeTree: %v", err)
	}

	got, err := r.LsTree(treeSHA)
	if err != nil {
		t.Fatalf("LsTree: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].Name != "a.txt" || got[1].Name != "b.txt" {
		t.Fatalf("unexpected entry names: %v, %v", got[0].Name, got[1].Name)
	}
}

func TestCommitTreeAndGetCommitTree(t *testing.T) {
	r := tempRunner(t)

	sha, _ := r.HashObject([]byte("content"))
	treeSHA, _ := r.MakeTree([]TreeEntry{
		{Mode: "100644", Type: "blob", SHA: sha, Name: "file.txt"},
	})

	commitSHA, err := r.CommitTree(treeSHA, nil, "test commit")
	if err != nil {
		t.Fatalf("CommitTree: %v", err)
	}
	if commitSHA == "" {
		t.Fatal("expected non-empty commit SHA")
	}

	gotTree, err := r.GetCommitTree(commitSHA)
	if err != nil {
		t.Fatalf("GetCommitTree: %v", err)
	}
	if gotTree != treeSHA {
		t.Fatalf("tree mismatch: got %s, want %s", gotTree, treeSHA)
	}
}

func TestCommitTreeWithParent(t *testing.T) {
	r := tempRunner(t)

	sha, _ := r.HashObject([]byte("v1"))
	tree1, _ := r.MakeTree([]TreeEntry{
		{Mode: "100644", Type: "blob", SHA: sha, Name: "file.txt"},
	})
	commit1, _ := r.CommitTree(tree1, nil, "first")

	sha2, _ := r.HashObject([]byte("v2"))
	tree2, _ := r.MakeTree([]TreeEntry{
		{Mode: "100644", Type: "blob", SHA: sha2, Name: "file.txt"},
	})
	commit2, err := r.CommitTree(tree2, []string{commit1}, "second")
	if err != nil {
		t.Fatalf("CommitTree with parent: %v", err)
	}

	// Verify ancestry.
	isAnc, _ := r.IsAncestor(commit1, commit2)
	if !isAnc {
		t.Fatal("expected commit1 to be ancestor of commit2")
	}
}

func TestUpdateRefAndReadRef(t *testing.T) {
	r := tempRunner(t)

	sha, _ := r.HashObject([]byte("data"))
	tree, _ := r.MakeTree([]TreeEntry{
		{Mode: "100644", Type: "blob", SHA: sha, Name: "f.txt"},
	})
	commit, _ := r.CommitTree(tree, nil, "init")

	ref := "refs/test/myref"

	// ReadRef on non-existent ref.
	got, err := r.ReadRef(ref)
	if err != nil {
		t.Fatalf("ReadRef: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}

	// Create ref.
	if err := r.UpdateRef(ref, commit, ""); err != nil {
		t.Fatalf("UpdateRef create: %v", err)
	}

	got, err = r.ReadRef(ref)
	if err != nil {
		t.Fatalf("ReadRef after create: %v", err)
	}
	if got != commit {
		t.Fatalf("ref mismatch: got %s, want %s", got, commit)
	}

	// CAS with correct old value.
	sha2, _ := r.HashObject([]byte("data2"))
	tree2, _ := r.MakeTree([]TreeEntry{
		{Mode: "100644", Type: "blob", SHA: sha2, Name: "f.txt"},
	})
	commit2, _ := r.CommitTree(tree2, []string{commit}, "update")

	if err := r.UpdateRef(ref, commit2, commit); err != nil {
		t.Fatalf("UpdateRef CAS: %v", err)
	}

	// CAS with wrong old value should fail.
	sha3, _ := r.HashObject([]byte("data3"))
	tree3, _ := r.MakeTree([]TreeEntry{
		{Mode: "100644", Type: "blob", SHA: sha3, Name: "f.txt"},
	})
	commit3, _ := r.CommitTree(tree3, []string{commit2}, "bad cas")

	err = r.UpdateRef(ref, commit3, commit) // wrong old value
	if err == nil {
		t.Fatal("expected CAS failure with wrong old value")
	}
}

func TestDeleteRef(t *testing.T) {
	r := tempRunner(t)

	sha, _ := r.HashObject([]byte("data"))
	tree, _ := r.MakeTree([]TreeEntry{
		{Mode: "100644", Type: "blob", SHA: sha, Name: "f.txt"},
	})
	commit, _ := r.CommitTree(tree, nil, "init")

	ref := "refs/test/deleteme"
	r.UpdateRef(ref, commit, "")

	if err := r.DeleteRef(ref); err != nil {
		t.Fatalf("DeleteRef: %v", err)
	}
	got, _ := r.ReadRef(ref)
	if got != "" {
		t.Fatalf("ref should be deleted, got %q", got)
	}
}

func TestFetchRefBetweenRepos(t *testing.T) {
	// Create a "remote" bare repo.
	remoteDir := t.TempDir()
	remote := &Runner{Dir: remoteDir}
	remote.RunGitCapture("init", "--bare")

	// Create a local repo and add the remote.
	local := tempRunner(t)
	local.RunGitCapture("remote", "add", "origin", remoteDir)

	// Create a ref in local and push it.
	sha, _ := local.HashObject([]byte("shared data"))
	tree, _ := local.MakeTree([]TreeEntry{
		{Mode: "100644", Type: "blob", SHA: sha, Name: "data.json"},
	})
	commit, _ := local.CommitTree(tree, nil, "share")
	local.UpdateRef("refs/stackr/data", commit, "")

	if err := local.PushRef("origin", "refs/stackr/data:refs/stackr/data"); err != nil {
		t.Fatalf("PushRef: %v", err)
	}

	// Create a second local clone and fetch the ref.
	clone := &Runner{Dir: t.TempDir()}
	clone.RunGitCapture("clone", remoteDir, ".")

	// Fetch the custom ref.
	absRemote, _ := filepath.Abs(remoteDir)
	clone.RunGitCapture("remote", "set-url", "origin", absRemote)
	if err := clone.FetchRef("origin", "refs/stackr/data:refs/stackr/data"); err != nil {
		t.Fatalf("FetchRef: %v", err)
	}

	got, _ := clone.ReadRef("refs/stackr/data")
	if got != commit {
		t.Fatalf("fetched ref mismatch: got %s, want %s", got, commit)
	}
}
