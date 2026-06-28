package store

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
)

// mergeFromRef performs a three-way merge of stackr metadata when local and
// remote refs have diverged. It reads the common ancestor (merge-base),
// merges each data file semantically, and creates a merge commit.
func (rs *RefStore) mergeFromRef(localSHA, remoteSHA string) error {
	baseSHA, err := rs.git.MergeBase(localSHA, remoteSHA)
	if err != nil {
		// No common ancestor — treat as independent histories.
		// Take remote (the safer default for shared state).
		return rs.git.UpdateRef(rs.ref, remoteSHA, localSHA)
	}

	// Read all three versions.
	baseBlobs, err := rs.ReadTreeBlobs(baseSHA)
	if err != nil {
		return fmt.Errorf("read base tree: %w", err)
	}
	localBlobs, err := rs.ReadTreeBlobs(localSHA)
	if err != nil {
		return fmt.Errorf("read local tree: %w", err)
	}
	remoteBlobs, err := rs.ReadTreeBlobs(remoteSHA)
	if err != nil {
		return fmt.Errorf("read remote tree: %w", err)
	}

	merged := make(map[string][]byte)

	// Merge branches.json (the graph).
	mergedGraph, err := mergeGraphs(baseBlobs["branches.json"], localBlobs["branches.json"], remoteBlobs["branches.json"])
	if err != nil {
		return fmt.Errorf("merge graph: %w", err)
	}
	data, err := json.MarshalIndent(mergedGraph, "", "  ")
	if err != nil {
		return err
	}
	merged["branches.json"] = append(data, '\n')

	// Merge config.json.
	mergedCfg, err := mergeConfigs(baseBlobs["config.json"], localBlobs["config.json"], remoteBlobs["config.json"])
	if err != nil {
		return fmt.Errorf("merge config: %w", err)
	}
	data, err = json.MarshalIndent(mergedCfg, "", "  ")
	if err != nil {
		return err
	}
	merged["config.json"] = append(data, '\n')

	// Merge pr_info.json.
	mergedPR, err := mergePRInfos(baseBlobs["pr_info.json"], localBlobs["pr_info.json"], remoteBlobs["pr_info.json"])
	if err != nil {
		return fmt.Errorf("merge pr info: %w", err)
	}
	data, err = json.MarshalIndent(mergedPR, "", "  ")
	if err != nil {
		return err
	}
	merged["pr_info.json"] = append(data, '\n')

	// Hash blobs and build tree.
	var entries []git.TreeEntry
	for name, content := range merged {
		sha, err := rs.git.HashObject(content)
		if err != nil {
			return err
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
		return err
	}

	// Create merge commit with two parents.
	commitSHA, err := rs.git.CommitTree(treeSHA, []string{localSHA, remoteSHA}, "stackr: merge metadata")
	if err != nil {
		return err
	}

	return rs.git.UpdateRef(rs.ref, commitSHA, localSHA)
}

// ---------- Graph merge ----------

func mergeGraphs(baseData, localData, remoteData []byte) (*graph.Graph, error) {
	base := parseGraphOrEmpty(baseData)
	local := parseGraphOrEmpty(localData)
	remote := parseGraphOrEmpty(remoteData)

	result := graph.New()
	result.Version = local.Version

	// Collect all branch names across all three versions.
	allNames := make(map[string]bool)
	for name := range base.Branches {
		allNames[name] = true
	}
	for name := range local.Branches {
		allNames[name] = true
	}
	for name := range remote.Branches {
		allNames[name] = true
	}

	for name := range allNames {
		baseBranch := base.Branches[name]
		localBranch := local.Branches[name]
		remoteBranch := remote.Branches[name]

		merged := mergeBranch(baseBranch, localBranch, remoteBranch)
		if merged != nil {
			result.Branches[name] = merged
		}
	}

	return result, nil
}

// mergeBranch performs a three-way merge on a single branch entry.
// Returns nil if the branch should be deleted from the result.
func mergeBranch(base, local, remote *graph.BranchState) *graph.BranchState {
	inBase := base != nil
	inLocal := local != nil
	inRemote := remote != nil

	switch {
	case !inBase && !inLocal && !inRemote:
		return nil

	case !inBase && inLocal && !inRemote:
		// Added locally only.
		return cloneBranchState(local)

	case !inBase && !inLocal && inRemote:
		// Added remotely only.
		return cloneBranchState(remote)

	case !inBase && inLocal && inRemote:
		// Added in both — merge fields.
		return mergeBranchFields(nil, local, remote)

	case inBase && !inLocal && !inRemote:
		// Deleted in both — gone.
		return nil

	case inBase && inLocal && !inRemote:
		// Deleted remotely. If local modified it, the deletion still wins
		// (the remote user explicitly removed it).
		return nil

	case inBase && !inLocal && inRemote:
		// Deleted locally. Same logic — deletion wins.
		return nil

	case inBase && inLocal && inRemote:
		// Present in all three — field-level merge.
		return mergeBranchFields(base, local, remote)

	default:
		return nil
	}
}

// mergeBranchFields merges individual fields of a BranchState.
// base may be nil (for branches added in both sides).
func mergeBranchFields(base, local, remote *graph.BranchState) *graph.BranchState {
	result := &graph.BranchState{}

	// ParentBranchName: if both changed to different values, take remote.
	result.ParentBranchName = mergeString(
		strOrEmpty(base, func(b *graph.BranchState) string { return b.ParentBranchName }),
		local.ParentBranchName,
		remote.ParentBranchName,
	)

	// ParentBranchRevision: take the version that matches the accepted parent.
	if result.ParentBranchName == local.ParentBranchName {
		result.ParentBranchRevision = local.ParentBranchRevision
	} else {
		result.ParentBranchRevision = remote.ParentBranchRevision
	}

	// BranchRevision: take whichever side changed it; if both changed, take remote.
	result.BranchRevision = mergeString(
		strOrEmpty(base, func(b *graph.BranchState) string { return b.BranchRevision }),
		local.BranchRevision,
		remote.BranchRevision,
	)

	// Children: union of children from both sides, minus any removed by either.
	result.Children = mergeChildren(base, local, remote)

	// IsTrunk: either side being trunk means trunk.
	result.IsTrunk = local.IsTrunk || remote.IsTrunk

	// Frozen: take local (operational state, local user's intent wins).
	result.Frozen = local.Frozen

	// Description: three-way string merge.
	result.Description = mergeString(
		strOrEmpty(base, func(b *graph.BranchState) string { return b.Description }),
		local.Description,
		remote.Description,
	)

	// Context: merge by key.
	result.Context = mergeContextEntries(base, local, remote)

	return result
}

// mergeChildren performs a three-way merge on children slices.
func mergeChildren(base, local, remote *graph.BranchState) []string {
	baseSet := make(map[string]bool)
	if base != nil {
		for _, c := range base.Children {
			baseSet[c] = true
		}
	}
	localSet := make(map[string]bool)
	for _, c := range local.Children {
		localSet[c] = true
	}
	remoteSet := make(map[string]bool)
	for _, c := range remote.Children {
		remoteSet[c] = true
	}

	// A child is in the result if:
	// - It was added by local (in local, not in base) OR
	// - It was added by remote (in remote, not in base) OR
	// - It was in base and NOT removed by either side.
	result := make(map[string]bool)
	for c := range localSet {
		if !baseSet[c] {
			result[c] = true // added by local
		} else if remoteSet[c] {
			result[c] = true // in base and not removed by remote
		}
	}
	for c := range remoteSet {
		if !baseSet[c] {
			result[c] = true // added by remote
		} else if localSet[c] {
			result[c] = true // in base and not removed by local
		}
	}

	// Convert to sorted slice for deterministic output.
	out := make([]string, 0, len(result))
	for c := range result {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// mergeContextEntries performs a three-way merge of BranchContext slices by key.
func mergeContextEntries(base, local, remote *graph.BranchState) []graph.BranchContext {
	baseMap := contextMap(base)
	localMap := contextMap(local)
	remoteMap := contextMap(remote)

	allKeys := make(map[string]bool)
	for k := range baseMap {
		allKeys[k] = true
	}
	for k := range localMap {
		allKeys[k] = true
	}
	for k := range remoteMap {
		allKeys[k] = true
	}

	var result []graph.BranchContext
	for key := range allKeys {
		_, inBase := baseMap[key]
		localCtx, inLocal := localMap[key]
		remoteCtx, inRemote := remoteMap[key]

		switch {
		case !inBase && inLocal && !inRemote:
			result = append(result, localCtx)
		case !inBase && !inLocal && inRemote:
			result = append(result, remoteCtx)
		case !inBase && inLocal && inRemote:
			// Both added — take remote.
			result = append(result, remoteCtx)
		case inBase && inLocal && !inRemote:
			// Deleted remotely — drop it.
		case inBase && !inLocal && inRemote:
			// Deleted locally — drop it.
		case inBase && inLocal && inRemote:
			// Modified — take whichever changed; if both changed, take remote.
			baseCtx := baseMap[key]
			localChanged := localCtx.Text != baseCtx.Text
			remoteChanged := remoteCtx.Text != baseCtx.Text
			if remoteChanged {
				result = append(result, remoteCtx)
			} else if localChanged {
				result = append(result, localCtx)
			} else {
				result = append(result, localCtx) // unchanged
			}
		}
	}

	// Sort by key for deterministic output.
	slices.SortFunc(result, func(a, b graph.BranchContext) int {
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		return 0
	})

	if len(result) == 0 {
		return nil
	}
	return result
}

// ---------- Config merge ----------

func mergeConfigs(baseData, localData, remoteData []byte) (*Config, error) {
	base := parseConfigOrEmpty(baseData)
	local := parseConfigOrEmpty(localData)
	remote := parseConfigOrEmpty(remoteData)

	return &Config{
		// Trunk is repository-global — take remote if changed.
		Trunk: mergeString(base.Trunk, local.Trunk, remote.Trunk),
		// Remote name is clone-local — take local if changed.
		Remote: mergeStringPreferLocal(base.Remote, local.Remote, remote.Remote),
	}, nil
}

// ---------- PR Info merge ----------

func mergePRInfos(baseData, localData, remoteData []byte) (*PRInfo, error) {
	base := parsePRInfoOrEmpty(baseData)
	local := parsePRInfoOrEmpty(localData)
	remote := parsePRInfoOrEmpty(remoteData)

	result := &PRInfo{Branches: make(map[string]*BranchPR)}

	allBranches := make(map[string]bool)
	for name := range base.Branches {
		allBranches[name] = true
	}
	for name := range local.Branches {
		allBranches[name] = true
	}
	for name := range remote.Branches {
		allBranches[name] = true
	}

	for name := range allBranches {
		basePR := base.Branches[name]
		localPR := local.Branches[name]
		remotePR := remote.Branches[name]

		merged := mergeBranchPR(basePR, localPR, remotePR)
		if merged != nil {
			result.Branches[name] = merged
		}
	}

	return result, nil
}

func mergeBranchPR(base, local, remote *BranchPR) *BranchPR {
	inBase := base != nil
	inLocal := local != nil
	inRemote := remote != nil

	switch {
	case !inLocal && !inRemote:
		return nil
	case inLocal && !inRemote:
		if inBase {
			return nil // deleted remotely
		}
		return cloneBranchPR(local)
	case !inLocal && inRemote:
		if inBase {
			return nil // deleted locally
		}
		return cloneBranchPR(remote)
	case inLocal && inRemote:
		// Both have it — take the one with the higher PR number,
		// or the one with a more advanced state.
		if !inBase {
			// Both added — take whichever has a PR number.
			if remote.Number > 0 {
				return cloneBranchPR(remote)
			}
			return cloneBranchPR(local)
		}
		// Modified in both — take the more advanced one.
		if prStateRank(remote.State) > prStateRank(local.State) {
			return cloneBranchPR(remote)
		}
		if remote.Number > local.Number {
			return cloneBranchPR(remote)
		}
		return cloneBranchPR(local)
	}
	return nil
}

// ---------- Helpers ----------

func mergeString(base, local, remote string) string {
	if local == remote {
		return local
	}
	if local == base {
		return remote // only remote changed
	}
	if remote == base {
		return local // only local changed
	}
	return remote // both changed — remote wins for shared state
}

func mergeStringPreferLocal(base, local, remote string) string {
	if local == remote {
		return local
	}
	if local == base {
		return remote
	}
	if remote == base {
		return local
	}
	return local // both changed — local wins
}

func strOrEmpty(b *graph.BranchState, fn func(*graph.BranchState) string) string {
	if b == nil {
		return ""
	}
	return fn(b)
}

func contextMap(b *graph.BranchState) map[string]graph.BranchContext {
	m := make(map[string]graph.BranchContext)
	if b == nil {
		return m
	}
	for _, c := range b.Context {
		m[c.Key] = c
	}
	return m
}

func cloneBranchState(b *graph.BranchState) *graph.BranchState {
	clone := *b
	clone.Children = slices.Clone(b.Children)
	clone.Context = slices.Clone(b.Context)
	return &clone
}

func cloneBranchPR(pr *BranchPR) *BranchPR {
	clone := *pr
	return &clone
}

func prStateRank(state string) int {
	switch state {
	case "merged":
		return 3
	case "closed":
		return 2
	case "open":
		return 1
	default:
		return 0
	}
}

func parseGraphOrEmpty(data []byte) *graph.Graph {
	if len(data) == 0 {
		return graph.New()
	}
	var g graph.Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return graph.New()
	}
	if g.Branches == nil {
		g.Branches = make(map[string]*graph.BranchState)
	}
	return &g
}

func parseConfigOrEmpty(data []byte) *Config {
	if len(data) == 0 {
		return &Config{}
	}
	var cfg Config
	_ = json.Unmarshal(data, &cfg)
	return &cfg
}

func parsePRInfoOrEmpty(data []byte) *PRInfo {
	if len(data) == 0 {
		return &PRInfo{Branches: make(map[string]*BranchPR)}
	}
	var info PRInfo
	_ = json.Unmarshal(data, &info)
	if info.Branches == nil {
		info.Branches = make(map[string]*BranchPR)
	}
	return &info
}
