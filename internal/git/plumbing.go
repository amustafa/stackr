package git

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// TreeEntry represents a single entry in a git tree object.
type TreeEntry struct {
	Mode string // "100644" for regular files
	Type string // "blob" or "tree"
	SHA  string
	Name string
}

// HashObject writes data to the git object store and returns the blob SHA.
func (r *Runner) HashObject(data []byte) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := r.command("hash-object", "-w", "--stdin")
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if r.Debug {
		fmt.Fprintf(os.Stderr, "[debug] git hash-object -w --stdin\n")
	}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("hash-object: %s: %w", stderr.String(), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// CatBlob reads a blob object by SHA and returns its contents.
func (r *Runner) CatBlob(sha string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := r.command("cat-file", "blob", sha)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("cat-file blob %s: %s: %w", sha, stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

// MakeTree creates a tree object from entries and returns the tree SHA.
func (r *Runner) MakeTree(entries []TreeEntry) (string, error) {
	var input bytes.Buffer
	for _, e := range entries {
		fmt.Fprintf(&input, "%s %s %s\t%s\n", e.Mode, e.Type, e.SHA, e.Name)
	}
	var stdout, stderr bytes.Buffer
	cmd := r.command("mktree")
	cmd.Stdin = &input
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mktree: %s: %w", stderr.String(), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// LsTree lists entries in a tree object.
func (r *Runner) LsTree(treeSHA string) ([]TreeEntry, error) {
	out, err := r.RunGitCapture("ls-tree", treeSHA)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var entries []TreeEntry
	for _, line := range strings.Split(out, "\n") {
		// Format: "<mode> <type> <sha>\t<name>"
		tabIdx := strings.IndexByte(line, '\t')
		if tabIdx < 0 {
			continue
		}
		meta := line[:tabIdx]
		name := line[tabIdx+1:]
		parts := strings.Fields(meta)
		if len(parts) != 3 {
			continue
		}
		entries = append(entries, TreeEntry{
			Mode: parts[0],
			Type: parts[1],
			SHA:  parts[2],
			Name: name,
		})
	}
	return entries, nil
}

// CommitTree creates a commit object with the given tree, optional parents, and message.
// Returns the commit SHA.
func (r *Runner) CommitTree(treeSHA string, parents []string, message string) (string, error) {
	args := []string{"commit-tree", treeSHA}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	args = append(args, "-m", message)

	var stdout, stderr bytes.Buffer
	cmd := r.command(args...)
	// Set environment on the command so these commits are visually distinct
	// from user commits and don't require user git config.
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=stackr",
		"GIT_AUTHOR_EMAIL=stackr@localhost",
		"GIT_COMMITTER_NAME=stackr",
		"GIT_COMMITTER_EMAIL=stackr@localhost",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("commit-tree: %s: %w", stderr.String(), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// GetCommitTree returns the tree SHA for a given commit.
func (r *Runner) GetCommitTree(commitSHA string) (string, error) {
	return r.RunGitCapture("rev-parse", commitSHA+"^{tree}")
}

// ReadRef returns the SHA a ref points to, or empty string if the ref does not exist.
func (r *Runner) ReadRef(ref string) (string, error) {
	sha, err := r.RunGitCapture("rev-parse", "--verify", ref)
	if err != nil {
		return "", nil // ref does not exist
	}
	return sha, nil
}

// UpdateRef atomically updates a ref. If oldSHA is non-empty, performs a CAS
// (compare-and-swap) — fails if the ref doesn't currently point to oldSHA.
func (r *Runner) UpdateRef(ref, newSHA, oldSHA string) error {
	args := []string{"update-ref", ref, newSHA}
	if oldSHA != "" {
		args = append(args, oldSHA)
	}
	_, err := r.RunGitCapture(args...)
	return err
}

// DeleteRef deletes a ref.
func (r *Runner) DeleteRef(ref string) error {
	_, err := r.RunGitCapture("update-ref", "-d", ref)
	return err
}

// FetchRef fetches a specific refspec from a remote.
func (r *Runner) FetchRef(remote, refspec string) error {
	_, _, err := r.RunGitCaptureAll("fetch", remote, refspec)
	return err
}

// PushRef pushes a specific refspec to a remote.
func (r *Runner) PushRef(remote, refspec string) error {
	return r.RunGit("push", remote, refspec)
}
