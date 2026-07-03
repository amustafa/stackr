package store

// PRInfo holds PR metadata per branch.
type PRInfo struct {
	Branches map[string]*BranchPR `json:"branches"`
}

// BranchForPR returns the branch name for a given PR number, or empty string if not found.
func (p *PRInfo) BranchForPR(number int) string {
	if p == nil || p.Branches == nil {
		return ""
	}
	for name, pr := range p.Branches {
		if pr.Number == number {
			return name
		}
	}
	return ""
}

// BranchPR holds PR info for a single branch.
type BranchPR struct {
	Number     int    `json:"number,omitempty"`
	Title      string `json:"title,omitempty"`
	Body       string `json:"body,omitempty"`
	State      string `json:"state,omitempty"` // open, closed, merged
	URL        string `json:"url,omitempty"`
	Draft      bool   `json:"draft,omitempty"`
	BaseBranch string `json:"baseBranch,omitempty"`
}
