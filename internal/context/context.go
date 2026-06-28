package context

import (
	"fmt"

	srerr "github.com/amustafa/stackr/internal/errors"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/store"
)

// Context holds all the dependencies needed by engine operations.
type Context struct {
	Git   *git.Runner
	Store store.Backend

	Debug       bool
	Interactive bool
	Quiet       bool
}

// Discover finds the repo root, .git dir, and builds a Context.
func Discover(cwd string, debug, interactive bool) (*Context, error) {
	runner := &git.Runner{Dir: cwd, Debug: debug}

	root, err := runner.RepoRoot()
	if err != nil {
		return nil, srerr.ErrNotARepo
	}
	runner.Dir = root

	// Use the common git dir so the store is found from worktrees too.
	gitDir, err := runner.GitCommonDir()
	if err != nil {
		return nil, fmt.Errorf("could not find .git dir: %w", err)
	}

	return &Context{
		Git:         runner,
		Store:       store.NewRefStore(runner, gitDir),
		Debug:       debug,
		Interactive: interactive,
	}, nil
}

// RequireInit returns an error if stackr is not initialized.
func (c *Context) RequireInit() error {
	if !c.Store.Exists() {
		return srerr.ErrNotInitialized
	}
	return nil
}
