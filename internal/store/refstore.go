package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	srerr "github.com/amustafa/stackr/internal/errors"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
)

const stackrRef = "refs/stackr/data"

// RefStore manages stackr metadata stored as git objects behind a custom ref.
// Shared data (graph, config, PR info) lives in the commit chain at refs/stackr/data.
// Local-only data (rebase state, undo) remains on the filesystem.
type RefStore struct {
	git    *git.Runner
	gitDir string // path to .git (common dir for worktree support)
	ref    string

	// Cache: populated on first read, invalidated on write.
	cachedCommit string
	cachedBlobs  map[string][]byte

	// Local filesystem store for ephemeral data (rebase state, undo).
	local *Store
}

// NewRefStore creates a RefStore backed by git custom refs.
func NewRefStore(gitRunner *git.Runner, gitDir string) *RefStore {
	return &RefStore{
		git:    gitRunner,
		gitDir: gitDir,
		ref:    stackrRef,
		local:  New(gitDir),
	}
}

// Ref returns the git ref used for shared metadata.
func (rs *RefStore) Ref() string {
	return rs.ref
}

// Init creates the local directory structure for ephemeral data.
// The ref itself is created lazily on first write.
func (rs *RefStore) Init() error {
	return rs.local.Init()
}

// Exists returns true if the ref exists or the local .stackr dir exists.
func (rs *RefStore) Exists() bool {
	sha, _ := rs.git.ReadRef(rs.ref)
	return sha != "" || rs.local.Exists()
}

// Root returns the local .stackr directory path (for undo/rebase state).
func (rs *RefStore) Root() string {
	return rs.local.Root()
}

// ---------- Shared data (stored in git refs) ----------

func (rs *RefStore) ReadGraph() (*graph.Graph, error) {
	data, err := rs.readBlob("branches.json")
	if err != nil {
		return nil, err
	}
	if data == nil {
		return graph.New(), nil
	}
	var g graph.Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, &srerr.StoreError{Op: "parse", Path: "branches.json", Err: err}
	}
	return &g, nil
}

func (rs *RefStore) WriteGraph(g *graph.Graph) error {
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return &srerr.StoreError{Op: "marshal", Path: "branches.json", Err: err}
	}
	data = append(data, '\n')
	return rs.writeFiles(map[string][]byte{"branches.json": data}, "update graph")
}

func (rs *RefStore) ReadConfig() (*Config, error) {
	data, err := rs.readBlob("config.json")
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, &srerr.StoreError{Op: "read", Path: "config.json", Err: fmt.Errorf("not found in ref")}
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, &srerr.StoreError{Op: "parse", Path: "config.json", Err: err}
	}
	return &cfg, nil
}

func (rs *RefStore) WriteConfig(cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return &srerr.StoreError{Op: "marshal", Path: "config.json", Err: err}
	}
	data = append(data, '\n')
	return rs.writeFiles(map[string][]byte{"config.json": data}, "update config")
}

func (rs *RefStore) ReadPRInfo() (*PRInfo, error) {
	data, err := rs.readBlob("pr_info.json")
	if err != nil {
		return nil, err
	}
	if data == nil {
		return &PRInfo{Branches: make(map[string]*BranchPR)}, nil
	}
	var info PRInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, &srerr.StoreError{Op: "parse", Path: "pr_info.json", Err: err}
	}
	if info.Branches == nil {
		info.Branches = make(map[string]*BranchPR)
	}
	return &info, nil
}

func (rs *RefStore) WritePRInfo(info *PRInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return &srerr.StoreError{Op: "marshal", Path: "pr_info.json", Err: err}
	}
	data = append(data, '\n')
	return rs.writeFiles(map[string][]byte{"pr_info.json": data}, "update pr info")
}

// ---------- Local-only data (stays on filesystem) ----------

func (rs *RefStore) ReadRebaseState() (*RebaseState, error) {
	return rs.local.ReadRebaseState()
}

func (rs *RefStore) WriteRebaseState(state *RebaseState) error {
	return rs.local.WriteRebaseState(state)
}

func (rs *RefStore) ClearRebaseState() error {
	return rs.local.ClearRebaseState()
}

func (rs *RefStore) HasRebaseState() bool {
	return rs.local.HasRebaseState()
}

func (rs *RefStore) SaveSnapshot(operation, branch string) error {
	// Save the current graph from refs as a local file snapshot.
	g, err := rs.ReadGraph()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// Write snapshot to the local filesystem.
	now := time.Now()
	id := fmt.Sprintf("%d", now.UnixNano())
	snapName := id + ".json"
	snapPath := filepath.Join(rs.local.Root(), "undo", "snapshots", snapName)
	if err := os.WriteFile(snapPath, data, 0o644); err != nil {
		return err
	}

	log := rs.local.readEventLog()
	log.Events = append(log.Events, UndoEvent{
		ID:        id,
		Timestamp: now,
		Operation: operation,
		Branch:    branch,
		Snapshot:  snapName,
	})
	return rs.local.writeEventLog(log)
}

func (rs *RefStore) PopSnapshot() (*UndoEvent, []byte, error) {
	return rs.local.PopSnapshot()
}

// ---------- Internal: read/write via git objects ----------

// readBlob reads a named file from the current ref's tree.
// Returns nil, nil if the ref doesn't exist or the file isn't in the tree.
func (rs *RefStore) readBlob(name string) ([]byte, error) {
	if err := rs.ensureCache(); err != nil {
		return nil, err
	}
	data, ok := rs.cachedBlobs[name]
	if !ok {
		return nil, nil
	}
	return data, nil
}

// ensureCache populates the cache by reading the current ref's tree.
func (rs *RefStore) ensureCache() error {
	if rs.cachedBlobs != nil {
		return nil
	}

	commitSHA, err := rs.git.ReadRef(rs.ref)
	if err != nil {
		return err
	}
	if commitSHA == "" {
		// Ref doesn't exist yet — return empty cache.
		rs.cachedBlobs = make(map[string][]byte)
		rs.cachedCommit = ""
		return nil
	}

	treeSHA, err := rs.git.GetCommitTree(commitSHA)
	if err != nil {
		return fmt.Errorf("read tree from ref: %w", err)
	}

	entries, err := rs.git.LsTree(treeSHA)
	if err != nil {
		return fmt.Errorf("ls-tree: %w", err)
	}

	blobs := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if e.Type != "blob" {
			continue
		}
		data, err := rs.git.CatBlob(e.SHA)
		if err != nil {
			return fmt.Errorf("read blob %s (%s): %w", e.Name, e.SHA, err)
		}
		blobs[e.Name] = data
	}

	rs.cachedCommit = commitSHA
	rs.cachedBlobs = blobs
	return nil
}

// invalidateCache clears the cached tree/blobs so the next read re-fetches.
func (rs *RefStore) invalidateCache() {
	rs.cachedCommit = ""
	rs.cachedBlobs = nil
}

// writeFiles atomically writes one or more named files to the ref.
// It reads the current tree, replaces/adds the given blobs, creates a new
// tree and commit, then CAS-updates the ref. Retries on concurrent modification.
func (rs *RefStore) writeFiles(files map[string][]byte, message string) error {
	const maxRetries = 3
	for attempt := range maxRetries {
		err := rs.tryWriteFiles(files, message)
		if err == nil {
			return nil
		}
		// If this wasn't the last attempt and looks like a CAS failure, retry.
		if attempt < maxRetries-1 {
			rs.invalidateCache()
			continue
		}
		return err
	}
	return fmt.Errorf("failed to update ref after retries")
}

func (rs *RefStore) tryWriteFiles(files map[string][]byte, message string) error {
	if err := rs.ensureCache(); err != nil {
		return err
	}

	// Build the updated set of tree entries.
	// Start from the existing blobs, then overlay the new files.
	merged := make(map[string][]byte, len(rs.cachedBlobs)+len(files))
	for k, v := range rs.cachedBlobs {
		merged[k] = v
	}
	for k, v := range files {
		merged[k] = v
	}

	// Hash each blob and build tree entries.
	var entries []git.TreeEntry
	for name, data := range merged {
		sha, err := rs.git.HashObject(data)
		if err != nil {
			return fmt.Errorf("hash %s: %w", name, err)
		}
		entries = append(entries, git.TreeEntry{
			Mode: "100644",
			Type: "blob",
			SHA:  sha,
			Name: name,
		})
	}

	treeSHA, err := rs.git.MakeTree(entries)
	if err != nil {
		return fmt.Errorf("make tree: %w", err)
	}

	// Create commit. Parent is the current ref head (if it exists).
	var parents []string
	if rs.cachedCommit != "" {
		parents = []string{rs.cachedCommit}
	}
	commitSHA, err := rs.git.CommitTree(treeSHA, parents, message)
	if err != nil {
		return fmt.Errorf("commit-tree: %w", err)
	}

	// CAS update the ref.
	if err := rs.git.UpdateRef(rs.ref, commitSHA, rs.cachedCommit); err != nil {
		return fmt.Errorf("update-ref CAS failed (concurrent modification?): %w", err)
	}

	// Update cache to reflect the write.
	rs.cachedCommit = commitSHA
	for k, v := range files {
		rs.cachedBlobs[k] = v
	}

	return nil
}

// ---------- Push / Pull ----------

// Push pushes the stackr metadata ref to the remote.
func (rs *RefStore) Push(remote string) error {
	return rs.git.PushRef(remote, rs.ref+":"+rs.ref)
}

// Pull fetches the stackr metadata ref from the remote and merges.
func (rs *RefStore) Pull(remote string) error {
	tempRef := "refs/stackr/remote-data"
	err := rs.git.FetchRef(remote, rs.ref+":"+tempRef)
	if err != nil {
		// Remote has no stackr data yet — nothing to pull.
		return nil
	}
	defer rs.git.DeleteRef(tempRef)

	remoteSHA, err := rs.git.ReadRef(tempRef)
	if err != nil || remoteSHA == "" {
		return nil
	}

	localSHA, _ := rs.git.ReadRef(rs.ref)

	if localSHA == "" {
		// No local data — take remote.
		rs.invalidateCache()
		return rs.git.UpdateRef(rs.ref, remoteSHA, "")
	}
	if remoteSHA == localSHA {
		return nil // Already in sync.
	}

	// Check fast-forward cases.
	remoteIsAncestor, _ := rs.git.IsAncestor(remoteSHA, localSHA)
	if remoteIsAncestor {
		return nil // Local is ahead, nothing to do.
	}
	localIsAncestor, _ := rs.git.IsAncestor(localSHA, remoteSHA)
	if localIsAncestor {
		// Remote is ahead — fast-forward.
		rs.invalidateCache()
		return rs.git.UpdateRef(rs.ref, remoteSHA, localSHA)
	}

	// Diverged — three-way merge required.
	rs.invalidateCache()
	return rs.mergeFromRef(localSHA, remoteSHA)
}

// ReadTreeBlobs reads all blobs from the tree of a given commit SHA.
func (rs *RefStore) ReadTreeBlobs(commitSHA string) (map[string][]byte, error) {
	treeSHA, err := rs.git.GetCommitTree(commitSHA)
	if err != nil {
		return nil, err
	}
	entries, err := rs.git.LsTree(treeSHA)
	if err != nil {
		return nil, err
	}
	blobs := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if e.Type != "blob" {
			continue
		}
		data, err := rs.git.CatBlob(e.SHA)
		if err != nil {
			return nil, err
		}
		blobs[e.Name] = data
	}
	return blobs, nil
}
