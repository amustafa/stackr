package errors

import (
	"errors"
	"fmt"
)

// Sentinel errors for common failure modes.
var (
	ErrNotARepo       = errors.New("not a git repository")
	ErrNotInitialized = errors.New("stackr not initialized — run `sr init`")
	ErrDirtyWorktree  = errors.New("working tree has uncommitted changes")
	ErrOnTrunk        = errors.New("cannot perform this operation on the trunk branch")
	ErrBranchNotFound = errors.New("branch not found in stack graph")
	ErrConflict       = errors.New("rebase conflict — resolve and run `sr continue`")
	ErrNoParent       = errors.New("branch has no parent in the stack graph")
	ErrNoChildren     = errors.New("branch has no children in the stack graph")
	ErrAlreadyExists  = errors.New("branch already exists")
	ErrFrozen         = errors.New("branch is frozen")
)

// GitError wraps a failed git command with its stderr.
type GitError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %v failed: %s", e.Args, e.Stderr)
}

func (e *GitError) Unwrap() error {
	return e.Err
}

// StoreError wraps file I/O errors from the store layer.
type StoreError struct {
	Op   string
	Path string
	Err  error
}

func (e *StoreError) Error() string {
	return fmt.Sprintf("store %s %s: %v", e.Op, e.Path, e.Err)
}

func (e *StoreError) Unwrap() error {
	return e.Err
}
