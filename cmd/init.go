package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	srctx "github.com/amustafa/stackr/internal/context"
	srerr "github.com/amustafa/stackr/internal/errors"
	"github.com/amustafa/stackr/internal/git"
	"github.com/amustafa/stackr/internal/graph"
	"github.com/amustafa/stackr/internal/store"
	"github.com/amustafa/stackr/internal/ui"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize stackr in a git repository",
	Long:  "Detects the trunk branch, initializes metadata storage, and seeds the branch graph. If no git repository exists, creates one first.",
	RunE:  runInit,
}

var errInitCancelled = errors.New("init cancelled")

var (
	initFlagTrunk string
	initFlagReset bool
)

func init() {
	initCmd.Flags().StringVar(&initFlagTrunk, "trunk", "", "trunk branch name (auto-detected if omitted)")
	initCmd.Flags().BoolVar(&initFlagReset, "reset", false, "re-initialize, overwriting existing data")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd := flagCwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	c, err := srctx.Discover(cwd, flagDebug, flagInteractive)

	switch {
	case errors.Is(err, srerr.ErrNotARepo):
		runner := &git.Runner{Dir: cwd, Debug: flagDebug}
		if err := runner.Init(); err != nil {
			return fmt.Errorf("git init failed: %w", err)
		}
		if err := bootstrapRepo(runner, cwd); err != nil {
			if errors.Is(err, errInitCancelled) {
				return nil
			}
			return err
		}
		if initFlagTrunk == "" {
			if branch, _ := runner.CurrentBranch(); branch != "" {
				initFlagTrunk = branch
			}
		}
		c, err = srctx.Discover(cwd, flagDebug, flagInteractive)
		if err != nil {
			return fmt.Errorf("failed to discover repo after git init: %w", err)
		}

	case err != nil:
		return err

	default:
		if c.Git.IsHeadUnborn() {
			if err := bootstrapRepo(c.Git, cwd); err != nil {
				if errors.Is(err, errInitCancelled) {
					return nil
				}
				return err
			}
			if initFlagTrunk == "" {
				if branch, _ := c.Git.CurrentBranch(); branch != "" {
					initFlagTrunk = branch
				}
			}
		}
	}

	if c.Store.Exists() && !initFlagReset {
		return fmt.Errorf("stackr already initialized (use --reset to re-initialize)")
	}

	trunk := initFlagTrunk
	if trunk == "" {
		trunk, err = c.Git.DefaultBranch()
		if err != nil {
			return fmt.Errorf("could not detect default branch: %w", err)
		}
		if trunk == "" {
			return fmt.Errorf("could not detect trunk branch — specify with --trunk")
		}
	}

	exists, err := c.Git.BranchExists(trunk)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("trunk branch %q does not exist", trunk)
	}

	rev, err := c.Git.RevParse(trunk)
	if err != nil {
		return fmt.Errorf("could not resolve %s: %w", trunk, err)
	}

	if err := c.Store.Init(); err != nil {
		return err
	}

	cfg := &store.Config{
		Trunk:  trunk,
		Remote: "origin",
	}
	if err := c.Store.WriteConfig(cfg); err != nil {
		return err
	}

	g := graph.New()
	g.AddTrunk(trunk, rev)

	current, err := c.Git.CurrentBranch()
	if err == nil && current != trunk {
		currentRev, err := c.Git.RevParse(current)
		if err == nil {
			_ = g.AddBranch(current, trunk, rev, currentRev)
		}
	}

	if err := c.Store.WriteGraph(g); err != nil {
		return err
	}

	fmt.Printf("Initialized stackr with trunk %q\n", trunk)
	return nil
}

func bootstrapRepo(runner *git.Runner, cwd string) error {
	if flagInteractive {
		return bootstrapInteractive(runner, cwd)
	}
	return bootstrapNonInteractive(runner)
}

func bootstrapInteractive(runner *git.Runner, cwd string) error {
	name, _ := runner.GetConfig("user.name")
	email, _ := runner.GetConfig("user.email")

	defaultBranch := "main"
	if configured, _ := runner.GetConfig("init.defaultBranch"); configured != "" {
		defaultBranch = configured
	}
	if initFlagTrunk != "" {
		defaultBranch = initFlagTrunk
	}

	fields := []ui.FormField{
		{Key: "name", Label: "User name", Kind: ui.FieldText, Value: name},
		{Key: "email", Label: "User email", Kind: ui.FieldText, Value: email},
		{Key: "branch", Label: "Default branch", Kind: ui.FieldText, Value: defaultBranch, Required: true},
		{Key: "origin", Label: "Origin URL", Kind: ui.FieldText},
		{Key: "upstream", Label: "Upstream URL", Kind: ui.FieldText},
		{Key: "gitignore", Label: "Create .gitignore", Kind: ui.FieldToggle, Toggle: true},
		{Key: "readme", Label: "Create README.md", Kind: ui.FieldToggle, Toggle: true},
	}

	result, err := ui.Form("Initialize Git Repository", fields)
	if err != nil {
		if errors.Is(err, ui.ErrCancelled) {
			fmt.Fprintln(os.Stderr, "Init cancelled. Git repository created but stackr not initialized.")
			return errInitCancelled
		}
		return err
	}

	return applyFormResult(runner, cwd, result)
}

func applyFormResult(runner *git.Runner, cwd string, result *ui.FormResult) error {
	if v := result.Values["name"]; v != "" {
		if err := runner.SetConfig("user.name", v); err != nil {
			return err
		}
	}
	if v := result.Values["email"]; v != "" {
		if err := runner.SetConfig("user.email", v); err != nil {
			return err
		}
	}

	branch := result.Values["branch"]
	currentBranch, _ := runner.GetConfig("init.defaultBranch")
	if currentBranch == "" {
		currentBranch = "main"
	}
	if branch != "" && branch != currentBranch {
		if runner.IsHeadUnborn() {
			_ = runner.RunGit("symbolic-ref", "HEAD", "refs/heads/"+branch)
		} else {
			if err := runner.RenameBranch(currentBranch, branch); err != nil {
				return fmt.Errorf("could not rename branch to %q: %w", branch, err)
			}
		}
	}

	if v := result.Values["origin"]; v != "" {
		if err := runner.AddRemote("origin", v); err != nil {
			return fmt.Errorf("could not add origin remote: %w", err)
		}
	}
	if v := result.Values["upstream"]; v != "" {
		if err := runner.AddRemote("upstream", v); err != nil {
			return fmt.Errorf("could not add upstream remote: %w", err)
		}
	}

	var createdFiles []string
	if result.Toggles["gitignore"] {
		if created, err := writeGitignore(cwd); err != nil {
			return err
		} else if created {
			createdFiles = append(createdFiles, ".gitignore")
		}
	}
	if result.Toggles["readme"] {
		if created, err := writeReadme(cwd); err != nil {
			return err
		} else if created {
			createdFiles = append(createdFiles, "README.md")
		}
	}

	if len(createdFiles) > 0 {
		if err := runner.Add(createdFiles...); err != nil {
			return err
		}
		return runner.Commit("Initial commit", git.CommitOpts{})
	}
	return runner.Commit("Initial commit", git.CommitOpts{AllowEmpty: true})
}

func bootstrapNonInteractive(runner *git.Runner) error {
	if initFlagTrunk != "" {
		if runner.IsHeadUnborn() {
			_ = runner.RunGit("symbolic-ref", "HEAD", "refs/heads/"+initFlagTrunk)
		} else {
			currentBranch, _ := runner.CurrentBranch()
			if currentBranch != initFlagTrunk {
				if err := runner.RenameBranch(currentBranch, initFlagTrunk); err != nil {
					return fmt.Errorf("could not rename branch to %q: %w", initFlagTrunk, err)
				}
			}
		}
	}
	return runner.Commit("Initial commit", git.CommitOpts{AllowEmpty: true})
}

func writeGitignore(dir string) (bool, error) {
	path := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(defaultGitignore), 0644)
}

func writeReadme(dir string) (bool, error) {
	path := filepath.Join(dir, "README.md")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	name := filepath.Base(dir)
	content := fmt.Sprintf("# %s\n", name)
	return true, os.WriteFile(path, []byte(content), 0644)
}

const defaultGitignore = `# OS files
.DS_Store
Thumbs.db
Desktop.ini

# Editor/IDE files
.idea/
.vscode/
*.swp
*.swo
*~
.project
.settings/

# Common build artifacts
dist/
build/
*.o
*.a
*.so
*.dylib

# Dependencies
node_modules/
vendor/
__pycache__/
*.pyc

# Environment/secrets
.env
.env.local
.env.*.local

# Stackr
.worktrees
`
